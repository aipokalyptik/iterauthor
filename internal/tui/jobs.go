package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/aipokalyptik/iterauthor/internal/engine"
	"github.com/aipokalyptik/iterauthor/internal/project"
	"github.com/rivo/tview"
)

func (u *UI) eligible(ids []string, force bool) []string {
	var out []string
	for _, id := range ids {
		status := u.store.Status(id)
		if force || status == "Missing" || status == "Invalidated" {
			out = append(out, id)
		}
	}
	return out
}
func (u *UI) generationDialog(force bool) {
	if u.busy || u.editor != nil {
		u.notice("Save the open buffer or stop active work before generation.")
		return
	}
	if len(u.store.State.Changes) > 0 {
		u.changes()
		return
	}
	id := u.selected
	title := "Generate"
	if force {
		title = "Regenerate"
	}
	u.choice(title+" prose", u.store.Config.TitleOf(id)+fmt.Sprintf("\n%d calls maximum across this queue; %d draft attempts per passage.\nAuthored prose remains active until you choose a replacement.", u.store.Config.Limits.Calls, u.store.Config.Limits.Drafts), []string{"This branch", "Selected passages", "Entire story", "Back"}, func(i int) {
		if i == 3 {
			return
		}
		start := func(ids []string) { u.editing = false; u.beginQueue(u.eligible(ids, force)) }
		if i == 1 {
			u.selectPassages(title+" selected passages", start)
			return
		}
		ids := u.store.Config.Leaves(id)
		if i == 2 {
			ids = u.store.Config.Leaves(u.store.Config.Root)
		}
		start(ids)
	})
}
func (u *UI) beginQueue(ids []string) {
	if len(ids) == 0 {
		u.notice("No eligible passages. Regenerate requests replacement candidates.")
		return
	}
	u.queue = append([]string(nil), ids...)
	u.queueCalls = u.store.Config.Limits.Calls
	u.queueDeadline = time.Now().Add(time.Duration(u.store.Config.Limits.Minutes) * time.Minute)
	u.queueContext, u.queueCancel = context.WithDeadline(context.Background(), u.queueDeadline)
	u.nextQueued()
}
func (u *UI) nextQueued() {
	if len(u.queue) == 0 {
		if u.queueCancel != nil {
			u.queueCancel()
			u.queueCancel = nil
		}
		return
	}
	if u.queueCalls <= 0 || time.Now().After(u.queueDeadline) {
		u.queue = nil
		if u.queueCancel != nil {
			u.queueCancel()
		}
		u.notice("Queue budget exhausted. Completed candidates are retained in Activity.")
		return
	}
	id := u.queue[0]
	u.queue = u.queue[1:]
	u.startJob("prose", id, "", "", nil)
}
func (u *UI) startJob(kind, target, manualModel, prompt string, history []project.Turn) {
	if u.busy || u.editor != nil {
		u.notice("Finish the active operation or save the editor first.")
		return
	}
	snapshot, err := u.store.Snapshot()
	if err != nil {
		u.queue = nil
		u.error(err)
		return
	}
	ctx := context.Background()
	if kind == "prose" && u.queueContext != nil {
		ctx = u.queueContext
		snapshot.Config.Limits.Calls = u.queueCalls
	}
	ctx, u.cancel = context.WithCancel(ctx)
	u.busy = true
	store := u.store
	if kind == "prose" {
		u.editing = false
		u.section = "Activity"
	} else {
		u.editing = true
	}
	u.notice("Starting " + kind + " for " + store.Config.TitleOf(target))
	u.refresh()
	e := engine.Engine{Client: u.client, Demo: u.demo, Save: store.SaveRun, Progress: func(stage string) { u.post(func() { u.notice(stage) }) }}
	go func() {
		run := e.Run(ctx, snapshot, target, kind, manualModel, prompt, history)
		u.post(func() {
			u.busy = false
			u.cancel = nil
			if kind == "prose" {
				u.queueCalls -= run.Calls
				if err := store.OfferRun(run, run.Status == "Available"); err != nil {
					u.notice(err.Error())
					u.queue = nil
				}
			}
			if kind == "advice" || kind == "edit" {
				if u.conversation != nil && u.conversation.Target == target {
					reply := run.Text
					if run.Error != "" {
						reply += "\n" + run.Error
					}
					if len(run.Edits) > 0 {
						reply += fmt.Sprintf("\n%d edit proposal(s) retained. Open this run in Activity to apply them.", len(run.Edits))
					}
					u.conversation.Turns = append(u.conversation.Turns, project.Turn{Role: "assistant", Text: reply, Run: run.ID})
					if err := store.SaveConversation(*u.conversation); err != nil {
						u.notice(err.Error())
					}
				}
			}
			if run.Status == "Canceled" || run.Status == "Failed" {
				u.queue = nil
			}
			u.refresh()
			u.notice(run.Kind + ": " + run.Status + " · " + run.Error)
			if u.quitPending {
				u.app.Stop()
				return
			}
			if kind == "prose" && len(u.queue) > 0 {
				u.nextQueued()
				return
			}
			if kind == "outline" || kind == "test" {
				u.inspectRun(run)
			}
		})
	}()
}
func (u *UI) expand() {
	if !u.writable() {
		return
	}
	id := u.selected
	direction := tview.NewTextArea().SetLabel("Direction: ").SetText("Add useful detail inside this drafting brief. Preserve my existing plot decisions.", false).SetSize(8, 0)
	f := tview.NewForm().AddFormItem(direction)
	f.AddButton("Generate proposal", func() { p := direction.GetText(); u.closeDialog(); u.startJob("outline", id, "", p, nil) }).AddButton("Back", u.closeDialog)
	f.SetBorder(true).SetTitle(" Generate outline detail · " + u.store.Config.TitleOf(id) + " ")
	u.showDialog(f, 90, 18, f)
}

func (u *UI) newConversation() {
	if !u.writable() {
		return
	}
	target := u.currentID()
	if u.store.Config.Nodes[target] == nil && u.store.Config.Knowledge[target] == nil {
		target = u.store.Config.Root
	}
	ids := u.store.Config.ModelIDs()
	selected := u.store.State.LastManualModel
	if _, ok := u.store.Config.Models[selected]; !ok {
		selected = u.store.Config.BaseModel
	}
	index := 0
	var labels []string
	for i, id := range ids {
		labels = append(labels, u.store.Config.Models[id].Name)
		if id == selected {
			index = i
		}
	}
	mode := "advice"
	f := tview.NewForm().AddTextView("Scope", u.store.Config.TitleOf(target)+" ["+target+"]\nWrite scope: selected item and outline descendants. Private notes excluded.\nBrowsing does not retarget the conversation.", 60, 4, false, false).AddDropDown("Activity", []string{"Advice / research", "Propose source edits"}, 0, func(_ string, i int) {
		mode = "advice"
		if i == 1 {
			mode = "edit"
		}
	}).AddDropDown("Model", labels, index, func(_ string, i int) { selected = ids[i] })
	f.AddButton("Start conversation", func() {
		if u.conversation != nil {
			if err := u.store.SaveConversation(*u.conversation); err != nil {
				u.notice(err.Error())
				return
			}
		}
		u.conversation = &project.Conversation{ID: project.NewID(), Target: target, Mode: mode, Model: selected}
		u.store.State.LastManualModel = selected
		if err := u.store.SaveState(); err != nil {
			u.notice(err.Error())
			return
		}
		if err := u.store.SaveConversation(*u.conversation); err != nil {
			u.notice(err.Error())
			return
		}
		u.prompt.SetText("", false)
		u.assistantVisible = true
		u.compactAssistant = true
		u.closeDialog()
		u.renderAssistant()
		u.layout()
		u.app.SetFocus(u.prompt)
	}).AddButton("Back", u.closeDialog)
	f.SetBorder(true).SetTitle(" Start a focused conversation ")
	u.showDialog(f, 92, 22, f)
}
func (u *UI) sessionMenu() {
	if u.busy {
		u.notice("Finish or cancel the active conversation turn before switching sessions.")
		return
	}
	list, err := u.store.Conversations()
	if err != nil {
		u.error(err)
		return
	}
	menu := tview.NewList().ShowSecondaryText(true)
	for _, c := range list {
		conversation := c
		menu.AddItem(u.store.Config.TitleOf(c.Target)+" · "+c.Mode, c.ID+" · "+u.store.Config.Models[c.Model].Name, 0, func() {
			if u.conversation != nil {
				if err := u.store.SaveConversation(*u.conversation); err != nil {
					u.error(err)
					return
				}
			}
			u.conversation = &conversation
			u.prompt.SetText(conversation.Draft, true)
			u.assistantVisible = true
			u.compactAssistant = true
			u.closeDialog()
			u.renderAssistant()
			u.layout()
			u.app.SetFocus(u.prompt)
		})
	}
	menu.AddItem("New conversation", "Uses the viewed item as its initial scope", 0, func() { u.closeDialog(); u.newConversation() })
	menu.AddItem("Back", "", 0, u.closeDialog)
	menu.SetBorder(true).SetTitle(" Saved conversations ")
	u.showDialog(menu, 92, 26, menu)
}
func (u *UI) manualModelDialog() {
	if u.busy {
		u.notice("Finish or cancel the current turn first.")
		return
	}
	if u.conversation == nil {
		u.newConversation()
		return
	}
	menu := tview.NewList()
	for _, id := range u.store.Config.ModelIDs() {
		ref := id
		m := u.store.Config.Models[id]
		menu.AddItem(m.Name, m.Model, 0, func() {
			u.conversation.Model = ref
			u.store.State.LastManualModel = ref
			if err := u.store.SaveConversation(*u.conversation); err != nil {
				u.error(err)
				return
			}
			if err := u.store.SaveState(); err != nil {
				u.error(err)
				return
			}
			u.closeDialog()
			u.renderAssistant()
		})
	}
	menu.AddItem("Back", "", 0, u.closeDialog)
	menu.SetBorder(true).SetTitle(" Model for this conversation ")
	u.showDialog(menu, 82, 20, menu)
}
func (u *UI) send() {
	if u.conversation == nil {
		u.newConversation()
		return
	}
	if u.busy || u.editor != nil {
		u.notice("Save the editor or stop the active operation first.")
		return
	}
	p := strings.TrimSpace(u.prompt.GetText())
	if p == "" {
		return
	}
	c := u.conversation
	history := append([]project.Turn(nil), c.Turns...)
	c.Turns = append(c.Turns, project.Turn{Role: "author", Text: p})
	c.Draft = ""
	u.prompt.SetText("", false)
	if err := u.store.SaveConversation(*c); err != nil {
		u.error(err)
		return
	}
	u.startJob(c.Mode, c.Target, c.Model, p, history)
}
func (u *UI) reviewLatest(id string) {
	ref := u.store.State.Passages[id].Candidate
	if ref == "" {
		u.message("No candidate", "Generate or regenerate a passage first.")
		return
	}
	r, err := u.store.LoadRun(ref)
	if err != nil {
		u.error(err)
		return
	}
	u.inspectRun(r)
}
func (u *UI) inspectRun(run project.Run) {
	text := fmt.Sprintf("%s · %s\nTarget: %s\nCalls: %d; reported tokens: %d\n%s\n\n", run.Kind, run.Status, u.store.Config.TitleOf(run.Target), run.Calls, run.Tokens, run.Error)
	if run.Demo {
		text += "DEMO MODEL — outputs and reviews are synthetic.\n\n"
	}
	if u.store.State.InvalidRuns[run.ID] {
		text += "INVALIDATED — retained for inspection only.\n\n"
	}
	text += run.Text
	for i, c := range run.Candidates {
		text += fmt.Sprintf("\n\nCANDIDATE %d\n\n%s\n\nConsistency:\n%s\n\nStyle:\n%s", i+1, c.Text, pretty(c.Consistency), pretty(c.Style))
	}
	for _, edit := range run.Edits {
		old, _ := u.store.Read(edit.ID, edit.Field)
		text += "\n\nEDIT PROPOSAL: " + u.store.Config.TitleOf(edit.ID) + " / " + edit.Field + "\n\nCURRENT:\n" + old + "\n\nPROPOSED:\n" + edit.Text
	}
	view := tview.NewTextView().SetText(clean(text)).SetWrap(true).SetWordWrap(true)
	view.SetBorder(true).SetTitle(" Operation " + run.ID + " ")
	bar := tview.NewFlex().AddItem(u.button("Back", u.closeDialog), 0, 1, false).AddItem(u.button("Exact inputs/tools", func() { u.inspectText("Recorded calls and context", run.Context+"\n\n"+pretty(run.Trace)) }), 0, 1, false)
	if run.Kind == "prose" && len(run.Candidates) > 0 {
		bar.AddItem(u.button("Use candidate", func() {
			if u.busy {
				u.notice("Stop active work before applying a candidate.")
				return
			}
			u.chooseCandidate(run)
		}), 0, 1, false)
	}
	if run.Kind == "outline" && run.Text != "" {
		bar.AddItem(u.button("Import into brief", func() {
			if !u.writable() {
				return
			}
			if u.store.State.InvalidRuns[run.ID] {
				u.notice("This proposal is invalidated.")
				return
			}
			current, err := u.store.Read(run.Target, "outline")
			if err != nil {
				u.error(err)
				return
			}
			if err = u.store.SaveText(run.Target, "outline", current+"\n\n"+run.Text, current); err != nil {
				u.error(err)
				return
			}
			u.closeDialog()
			u.selected = run.Target
			u.section = "Outline"
			u.tab = "Outline"
			u.refresh()
			u.notice("Imported as authored outline detail. Review saved changes before generation.")
		}), 0, 1, false)
	}
	if len(run.Edits) > 0 {
		bar.AddItem(u.button("Apply proposals", func() { u.applyEdits(run) }), 0, 1, false)
	}
	body := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(view, 0, 1, true).AddItem(bar, 1, 0, false)
	u.showDialog(body, 110, 38, view)
}
func (u *UI) chooseCandidate(run project.Run) {
	list := tview.NewList().ShowSecondaryText(true)
	for i, c := range run.Candidates {
		index := i
		status := "Unreviewed / findings"
		if c.Consistency != nil && c.Consistency.Pass && c.Style != nil && c.Style.Pass {
			status = "Both checks passed"
		}
		list.AddItem(fmt.Sprintf("Candidate %d", i+1), status, 0, func() {
			if err := u.store.UseCandidate(run, index, true); err != nil {
				u.error(err)
				return
			}
			u.editing = true
			u.closeDialog()
			u.selected = run.Target
			u.section = "Outline"
			u.tab = "Prose"
			u.refresh()
			u.notice("Candidate selected. Earlier versions remain in history.")
		})
	}
	list.AddItem("Back", "", 0, u.closeDialog)
	list.SetBorder(true).SetTitle(" Explicitly select active prose ")
	u.showDialog(list, 82, 20, list)
}
func (u *UI) applyEdits(run project.Run) {
	if !u.writable() {
		return
	}
	if u.store.State.InvalidRuns[run.ID] {
		u.notice("This run is invalidated.")
		return
	}
	hash, err := u.store.Fingerprint()
	if err != nil {
		u.error(err)
		return
	}
	if hash != run.Fingerprint {
		u.error(fmt.Errorf("sources changed since this proposal; start a new turn using current files"))
		return
	}
	for _, edit := range run.Edits {
		if !u.store.Config.Contains(run.Target, edit.ID) {
			u.error(fmt.Errorf("proposal is outside its write scope"))
			return
		}
		old, err := u.store.Read(edit.ID, edit.Field)
		if err != nil {
			u.error(err)
			return
		}
		if err = u.store.SaveText(edit.ID, edit.Field, edit.Text, old); err != nil {
			u.error(err)
			return
		}
	}
	u.closeDialog()
	u.refresh()
	u.notice("Scoped edits applied and views refreshed. Generation remains paused.")
}
func (u *UI) sourceHistory(id string) {
	base := filepath.Join(u.store.Dir, ".twriter", "history")
	dirs, err := os.ReadDir(base)
	if os.IsNotExist(err) {
		u.message("Source history", "No source edits have been saved yet.")
		return
	}
	if err != nil {
		u.error(err)
		return
	}
	type version struct {
		path, label string
		at          time.Time
	}
	var versions []version
	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		files, _ := os.ReadDir(filepath.Join(base, dir.Name()))
		for _, f := range files {
			if strings.HasPrefix(f.Name(), id+"-") {
				info, e := f.Info()
				if e != nil {
					continue
				}
				versions = append(versions, version{path: filepath.Join(base, dir.Name(), f.Name()), label: f.Name(), at: info.ModTime()})
			}
		}
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i].at.After(versions[j].at) })
	list := tview.NewList().ShowSecondaryText(true)
	for _, v := range versions {
		v := v
		list.AddItem(v.label, v.at.Format(time.RFC3339), 0, func() {
			b, e := os.ReadFile(v.path)
			if e != nil {
				u.error(e)
				return
			}
			u.inspectText("Source backup · "+v.label, string(b))
		})
	}
	list.AddItem("Back", "", 0, u.closeDialog)
	list.SetBorder(true).SetTitle(" Source history · " + u.store.Config.TitleOf(id) + " ")
	u.showDialog(list, 90, 28, list)
}
