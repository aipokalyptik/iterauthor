package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/aipokalyptik/iterauthor/internal/application"
	"github.com/aipokalyptik/iterauthor/internal/project"
	"github.com/rivo/tview"
)

func (u *UI) generationDialog(force bool) {
	if u.state.Busy || u.editor != nil {
		u.notice("Save the open buffer or stop active work before generation.")
		return
	}
	if len(u.state.State.Changes) > 0 {
		u.changes()
		return
	}
	id := u.selected
	title := "Generate"
	if force {
		title = "Regenerate"
	}
	u.choice(title+" prose", u.state.Config.TitleOf(id)+fmt.Sprintf("\n%d calls maximum across this queue; %d draft attempts per passage.\nAuthored prose remains active until you choose a replacement.", u.state.Config.Limits.Calls, u.state.Config.Limits.Drafts), []string{"This branch", "Selected passages", "Entire story", "Back"}, func(i int) {
		if i == 3 {
			return
		}
		start := func(ids []string) { u.generate(application.Selection{Scope: "selected", IDs: ids}, force) }
		if i == 1 {
			u.selectPassages(title+" selected passages", start)
			return
		}
		sel := application.Selection{Scope: "branch", Target: id}
		if i == 2 {
			sel.Scope = "story"
		}
		u.generate(sel, force)
	})
}
func (u *UI) generate(selection application.Selection, force bool) {
	if u.editor != nil {
		u.notice("Save the open buffer before generation.")
		return
	}
	if err := u.core.Generate(selection, force); err != nil {
		u.error(err)
		return
	}
	if u.core.View().Busy {
		u.section = "Activity"
	}
	u.refresh()
}
func (u *UI) startJob(kind, target, manualModel, prompt string) {
	if u.editor != nil {
		u.notice("Save the open buffer first.")
		return
	}
	if err := u.core.Start(application.Job{Kind: kind, Target: target, Model: manualModel, Prompt: prompt}); err != nil {
		u.error(err)
		return
	}
	u.refresh()
}
func (u *UI) expand() {
	if !u.writable() {
		return
	}
	id := u.selected
	direction := tview.NewTextArea().SetLabel("Direction: ").SetText("Add useful detail inside this drafting brief. Preserve my existing plot decisions.", false).SetSize(8, 0)
	f := tview.NewForm().AddFormItem(direction)
	f.AddButton("Generate proposal", func() { p := direction.GetText(); u.closeDialog(); u.startJob("outline", id, "", p) }).AddButton("Back", u.closeDialog)
	f.SetBorder(true).SetTitle(" Generate outline detail · " + u.state.Config.TitleOf(id) + " ")
	u.showDialog(f, 90, 18, f)
}

func (u *UI) newConversation() {
	if !u.writable() {
		return
	}
	target := u.currentID()
	if u.state.Config.Nodes[target] == nil && u.state.Config.Knowledge[target] == nil {
		target = u.state.Config.Root
	}
	ids := u.state.Config.ModelIDs()
	selected := u.state.State.LastManualModel
	if _, ok := u.state.Config.Models[selected]; !ok {
		selected = u.state.Config.BaseModel
	}
	index := 0
	var labels []string
	for i, id := range ids {
		labels = append(labels, u.state.Config.Models[id].Name)
		if id == selected {
			index = i
		}
	}
	mode := "advice"
	f := tview.NewForm().AddTextView("Scope", u.state.Config.TitleOf(target)+" ["+target+"]\nWrite scope: selected item and outline descendants. Private notes excluded.\nBrowsing does not retarget the conversation.", 60, 4, false, false).AddDropDown("Activity", []string{"Advice / research", "Propose source edits"}, 0, func(_ string, i int) {
		mode = "advice"
		if i == 1 {
			mode = "edit"
		}
	}).AddDropDown("Model", labels, index, func(_ string, i int) { selected = ids[i] })
	f.AddButton("Start conversation", func() {
		if u.conversation != nil {
			if err := u.core.SaveDraft(u.conversation.ID, u.prompt.GetText()); err != nil {
				u.notice(err.Error())
				return
			}
		}
		conversation, err := u.core.NewConversation(target, mode, selected)
		if err != nil {
			u.error(err)
			return
		}
		u.conversation = &conversation
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
	if u.state.Busy {
		u.notice("Finish or cancel the active conversation turn before switching sessions.")
		return
	}
	list, err := u.core.Conversations()
	if err != nil {
		u.error(err)
		return
	}
	menu := tview.NewList().ShowSecondaryText(true)
	for _, c := range list {
		conversation := c
		menu.AddItem(u.state.Config.TitleOf(c.Target)+" · "+c.Mode, c.ID+" · "+u.state.Config.Models[c.Model].Name, 0, func() {
			if u.conversation != nil {
				if err := u.core.SaveDraft(u.conversation.ID, u.prompt.GetText()); err != nil {
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
	if u.state.Busy {
		u.notice("Finish or cancel the current turn first.")
		return
	}
	if u.conversation == nil {
		u.newConversation()
		return
	}
	menu := tview.NewList()
	for _, id := range u.state.Config.ModelIDs() {
		ref := id
		m := u.state.Config.Models[id]
		menu.AddItem(m.Name, m.Model, 0, func() {
			if err := u.core.SetConversationModel(u.conversation.ID, ref); err != nil {
				u.error(err)
				return
			}
			u.conversation.Model = ref
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
	if u.state.Busy || u.editor != nil {
		u.notice("Save the editor or stop the active operation first.")
		return
	}
	p := strings.TrimSpace(u.prompt.GetText())
	if p == "" {
		return
	}
	if err := u.core.Send(u.conversation.ID, p); err != nil {
		u.error(err)
		return
	}
	c, err := u.core.LoadConversation(u.conversation.ID)
	if err != nil {
		u.error(err)
		return
	}
	u.conversation = &c
	u.prompt.SetText("", false)
	u.refresh()
}
func (u *UI) reviewLatest(id string) {
	ref := u.state.State.Passages[id].Candidate
	if ref == "" {
		u.message("No candidate", "Generate or regenerate a passage first.")
		return
	}
	r, err := u.core.LoadRun(ref)
	if err != nil {
		u.error(err)
		return
	}
	u.inspectRun(r)
}
func (u *UI) inspectRun(run project.Run) {
	text := fmt.Sprintf("%s · %s\nTarget: %s\nCalls: %d; reported tokens: %d\n%s\n\n", run.Kind, run.Status, u.state.Config.TitleOf(run.Target), run.Calls, run.Tokens, run.Error)
	if run.Demo {
		text += "DEMO MODEL — outputs and reviews are synthetic.\n\n"
	}
	if u.state.State.InvalidRuns[run.ID] {
		text += "INVALIDATED — retained for inspection only.\n\n"
	}
	text += run.Text
	for i, c := range run.Candidates {
		text += fmt.Sprintf("\n\nCANDIDATE %d\n\n%s\n\nConsistency:\n%s\n\nStyle:\n%s", i+1, c.Text, pretty(c.Consistency), pretty(c.Style))
	}
	for _, edit := range run.Edits {
		old, _ := u.core.Read(edit.ID, edit.Field)
		text += "\n\nEDIT PROPOSAL: " + u.state.Config.TitleOf(edit.ID) + " / " + edit.Field + "\n\nCURRENT:\n" + old + "\n\nPROPOSED:\n" + edit.Text
	}
	view := tview.NewTextView().SetText(clean(text)).SetWrap(true).SetWordWrap(true)
	view.SetBorder(true).SetTitle(" Operation " + run.ID + " ")
	bar := tview.NewFlex().AddItem(u.button("Back", u.closeDialog), 0, 1, false).AddItem(u.button("Exact inputs/tools", func() { u.inspectText("Recorded calls and context", run.Context+"\n\n"+pretty(run.Trace)) }), 0, 1, false)
	if run.Kind == "prose" && len(run.Candidates) > 0 {
		bar.AddItem(u.button("Use candidate", func() {
			if u.state.Busy {
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
			if err := u.core.ImportOutline(run.ID); err != nil {
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
			if err := u.core.UseCandidate(run.ID, index); err != nil {
				u.error(err)
				return
			}
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
	if err := u.core.ApplyEdits(run.ID); err != nil {
		u.error(err)
		return
	}
	u.closeDialog()
	u.refresh()
	u.notice("Scoped edits applied and views refreshed. Generation remains paused.")
}
func (u *UI) sourceHistory(id string) {
	versions, err := u.core.SourceHistory(id)
	if err != nil {
		u.error(err)
		return
	}
	if len(versions) == 0 {
		u.message("Source history", "No source edits have been saved yet.")
		return
	}
	list := tview.NewList().ShowSecondaryText(true)
	for _, v := range versions {
		v := v
		list.AddItem(v.Label, v.At.Format(time.RFC3339), 0, func() {
			text, e := u.core.ReadSourceVersion(v.ID)
			if e != nil {
				u.error(e)
				return
			}
			u.inspectText("Source backup · "+v.Label, text)
		})
	}
	list.AddItem("Back", "", 0, u.closeDialog)
	list.SetBorder(true).SetTitle(" Source history · " + u.state.Config.TitleOf(id) + " ")
	u.showDialog(list, 90, 28, list)
}
