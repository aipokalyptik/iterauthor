package tui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/aipokalyptik/iterauthor/internal/project"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func (u *UI) rebuildTree() {
	root := tview.NewTreeNode(u.section).SetExpanded(true)
	var selected *tview.TreeNode
	if u.section == "Outline" || u.section == "Manuscript" {
		var add func(string) *tview.TreeNode
		add = func(id string) *tview.TreeNode {
			n := u.state.Config.Nodes[id]
			title := n.Title
			if len(n.Children) == 0 {
				title += " · " + u.core.Status(id)
			}
			row := tview.NewTreeNode(title).SetReference(id).SetExpanded(true)
			if id == u.selected {
				selected = row
			}
			for _, child := range n.Children {
				row.AddChild(add(child))
			}
			return row
		}
		root = add(u.state.Config.Root)
	} else if u.section == "Knowledge" {
		var ids []string
		for id := range u.state.Config.Knowledge {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return u.state.Config.TitleOf(ids[i]) < u.state.Config.TitleOf(ids[j]) })
		for _, id := range ids {
			e := u.state.Config.Knowledge[id]
			row := tview.NewTreeNode(e.Title + " · " + e.Kind).SetReference(id)
			root.AddChild(row)
			if id == u.knowledge {
				selected = row
			}
		}
	} else {
		root.AddChild(tview.NewTreeNode("Use the buttons in the document pane.").SetSelectable(false))
	}
	u.tree.SetTitle(" " + u.section + " · Ctrl+O Browse ").SetBorder(true)
	u.tree.SetChangedFunc(nil)
	u.tree.SetRoot(root)
	if selected != nil {
		u.tree.SetCurrentNode(selected)
	} else {
		u.tree.SetCurrentNode(root)
	}
	u.tree.SetChangedFunc(func(n *tview.TreeNode) {
		id, ok := n.GetReference().(string)
		if !ok {
			return
		}
		if u.editor != nil {
			u.notice("Save or cancel the open buffer before choosing another item.")
			return
		}
		if u.section == "Knowledge" {
			u.knowledge = id
		} else {
			u.selected = id
		}
		u.renderDocument()
		u.layout()
		u.chrome()
	})
}
func (u *UI) renderDocument() {
	u.documentMain = u.view
	u.document.Clear()
	u.tabbar.Clear()
	u.document.SetTitle(" " + u.section + " / " + clean(u.state.Config.TitleOf(u.currentID())) + " ")
	var tabs []string
	if u.section == "Outline" {
		tabs = []string{"Outline", "Prose", "Guidance", "Context", "History", "Notes"}
	} else if u.section == "Knowledge" {
		tabs = []string{"Entry", "Links", "History", "Notes"}
	}
	for _, name := range tabs {
		tab := name
		label := name
		if u.tab == name {
			label = "[" + name + "]"
		}
		u.tabbar.AddItem(u.button(label, func() { u.guardBuffer(func() { u.tab = tab; u.renderDocument(); u.layout() }) }), len(label)+2, 0, false)
	}
	if len(tabs) > 0 {
		u.document.AddItem(u.tabbar, 1, 0, false)
	}
	if u.editor != nil {
		u.document.AddItem(u.editor, 0, 1, true)
		actions := tview.NewFlex().AddItem(u.button("Save", func() { u.saveEdit() }), 12, 0, false).AddItem(u.button("Cancel edit", func() { u.editor = nil; u.refresh(); u.app.SetFocus(u.documentFocus()) }), 16, 0, false)
		u.document.AddItem(actions, 1, 0, false)
		return
	}
	if u.section == "Activity" {
		u.activity()
		return
	}
	if u.section == "Settings" {
		u.settingsView()
		return
	}
	if u.section == "Manuscript" {
		u.view.SetText(clean(u.core.Manuscript()))
		u.document.AddItem(u.view, 0, 1, true)
		u.actions([]string{"Open selected source", "Export manuscript"}, []func(){func() { u.navigate("Outline", u.selected); u.tab = "Prose"; u.renderDocument() }, u.export})
		return
	}
	id := u.currentID()
	if id == "" {
		u.view.SetText("The knowledge store is empty. Add a character, place, world fact, or another entry.")
		u.document.AddItem(u.view, 0, 1, true)
		u.actions([]string{"New entry"}, []func(){u.addEntry})
		return
	}
	text := ""
	var labels []string
	var funcs []func()
	switch u.tab {
	case "Outline", "Entry", "Notes":
		field := u.field()
		var err error
		text, err = u.core.Read(id, field)
		if err != nil {
			text = err.Error()
		}
		if u.tab == "Notes" {
			text = "PRIVATE NOTES — never sent to or searchable by models.\n\n" + text
		}
		if text == "" {
			text = "Empty. Choose Edit to add text."
		}
		labels = []string{"Edit", "Discuss", "Actions"}
		funcs = []func(){u.edit, u.newConversation, u.commandMenu}
	case "Prose":
		n := u.state.Config.Nodes[id]
		if n == nil {
			break
		}
		if len(n.Children) > 0 {
			for _, leaf := range u.state.Config.Leaves(id) {
				prose, _ := u.core.Read(leaf, "prose")
				text += "## " + u.state.Config.TitleOf(leaf) + " · " + u.core.Status(leaf) + "\n\n" + prose + "\n\n"
			}
			labels = []string{"Generate branch", "Regenerate branch"}
			funcs = []func(){func() { u.generationDialog(false) }, func() { u.generationDialog(true) }}
		} else {
			prose, _ := u.core.Read(id, "prose")
			text = u.core.Status(id) + "\n\n" + prose
			if prose == "" {
				text = "Missing passage. Generate from this leaf's complete drafting brief."
			}
			labels = []string{"Edit", "Generate", "Regenerate", "Review candidate"}
			funcs = []func(){u.edit, func() { u.generationDialog(false) }, func() { u.generationDialog(true) }, func() { u.reviewLatest(id) }}
		}
	case "Guidance":
		snap, err := u.core.Snapshot()
		if err != nil {
			text = err.Error()
		} else {
			text = "EFFECTIVE PROSE STYLE\n\n" + snap.Style(id) + "\n\nMODEL ASSIGNMENTS\n"
			for _, role := range project.Roles {
				ref, origin := snap.Config.ResolveModel(id, role)
				text += fmt.Sprintf("%s: %s (from %s)\n", project.RoleNames[role], snap.Config.Models[ref].Name, origin)
			}
			text += "\nOPERATION INSTRUCTIONS\n"
			for _, role := range project.Roles {
				if p := snap.Config.Prompt(id, role); p != "" {
					text += "\n" + project.RoleNames[role] + ":\n" + p + "\n"
				}
			}
		}
		labels = []string{"Edit local style", "Models/style mode", "Operation prompt"}
		funcs = []func(){u.edit, u.guidanceDialog, u.promptDialog}
	case "Context":
		c := u.state.Config
		text = "AUTOMATIC CONTEXT\n"
		for _, kind := range []string{"knowledge", "outline"} {
			enabled, origin := c.Automatic(id, kind)
			text += fmt.Sprintf("%s: %t · from %s\n", kind, enabled, origin)
		}
		text += "\nREQUIRED ATTACHMENTS (included even with automatic selection off)\n"
		for _, ancestor := range c.Ancestors(id) {
			for _, a := range c.Nodes[ancestor].Attachments {
				suffix := "entry only"
				if a.Descendants {
					suffix = "with descendants"
				}
				text += fmt.Sprintf("%s [%s] · %s · attached at %s\n", c.TitleOf(a.ID), a.ID, suffix, c.TitleOf(ancestor))
			}
		}
		text += "\nNotes are excluded. History/Activity records the actual prompts and tool reads for each run."
		labels = []string{"Automatic selection", "Attach reference", "Remove local ref"}
		funcs = []func(){u.contextDialog, u.attachDialog, u.removeAttachment}
	case "Links":
		text = "Explicit outline consumers:\n\n"
		for nodeID, n := range u.state.Config.Nodes {
			for _, a := range n.Attachments {
				if a.ID == id {
					text += n.Title + " [" + nodeID + "]\n"
				}
			}
		}
		text += "\nAutomatic tool reads are recorded per run in Activity."
		labels = []string{"Discuss"}
		funcs = []func(){u.newConversation}
	case "History":
		u.history(id)
		return
	}
	u.view.SetText(clean(text))
	u.document.AddItem(u.view, 0, 1, true)
	u.actions(labels, funcs)
}
func (u *UI) actions(labels []string, functions []func()) {
	bar := tview.NewFlex()
	for i, label := range labels {
		bar.AddItem(u.button(label, functions[i]), 0, 1, false)
	}
	u.document.AddItem(bar, 1, 0, false)
}
func (u *UI) history(id string) {
	runs, err := u.core.Runs()
	if err != nil {
		u.error(err)
		return
	}
	list := tview.NewList().ShowSecondaryText(true)
	list.SetBorderPadding(0, 0, 1, 1)
	for _, r := range runs {
		if r.Target != id {
			continue
		}
		run := r
		status := r.Status
		if r.Finished == "" {
			status = "Interrupted"
		}
		if u.state.State.InvalidRuns[r.ID] {
			status = "Invalidated"
		}
		list.AddItem(r.Kind+" · "+status, r.Started, 0, func() { u.inspectRun(run) })
	}
	list.AddItem("Source backups", "Open a saved version of authored text", 0, func() { u.sourceHistory(id) })
	u.documentMain = list
	u.document.AddItem(list, 0, 1, true)
	u.actions([]string{"Edit source", "Invalidate generated cache"}, []func(){u.edit, u.invalidateCache})
}
func (u *UI) activity() {
	runs, err := u.core.Runs()
	if err != nil {
		u.view.SetText(err.Error())
		u.document.AddItem(u.view, 0, 1, true)
		return
	}
	u.runs = runs
	list := tview.NewList().ShowSecondaryText(true)
	if u.state.Busy {
		list.AddItem("Running: "+u.progress, "Cancel stops the active call and clears the queue.", 0, func() { u.stopWork() })
	}
	for _, id := range u.state.Queue {
		list.AddItem("Queued: "+u.state.Config.TitleOf(id), "Use Cancel work to stop the remaining queue.", 0, nil)
	}
	for _, r := range runs {
		run := r
		status := r.Status
		if r.Finished == "" && !u.state.Busy {
			status = "Interrupted"
		}
		list.AddItem(u.state.Config.TitleOf(r.Target)+" · "+r.Kind+" · "+status, fmt.Sprintf("%d calls · %d reported tokens · %s", r.Calls, r.Tokens, r.Started), 0, func() { u.inspectRun(run) })
	}
	if len(runs) == 0 && !u.state.Busy {
		list.AddItem("No operations yet", "Generate a leaf or start a focused conversation.", 0, nil)
	}
	u.documentMain = list
	u.document.AddItem(list, 0, 1, true)
	u.actions([]string{"Refresh", "Cancel work", "Review saved changes"}, []func(){func() { u.renderDocument() }, u.stopWork, u.changes})
}
func (u *UI) settingsView() {
	c := u.state.Config
	var b strings.Builder
	fmt.Fprintf(&b, "PROJECT\n%s\n%s\n\nMODEL CONNECTIONS\n", c.Title, u.state.Dir)
	for _, id := range c.ModelIDs() {
		m := c.Models[id]
		mark := ""
		if id == c.BaseModel {
			mark = " · base"
		}
		fmt.Fprintf(&b, "%s: %s%s\n  %s\n  model=%s · tools=%t\n", id, m.Name, mark, m.URL, m.Model, m.Tools)
	}
	fmt.Fprintf(&b, "\nGENERATION LIMITS\n%d draft attempts per passage\n%d total calls per requested operation/queue\n%d output tokens per call\n%d input characters per call\n%d minutes per operation/queue\nGeneration starts only on your command.\n", c.Limits.Drafts, c.Limits.Calls, c.Limits.OutputTokens, c.Limits.ContextChars, c.Limits.Minutes)
	b.WriteString("\nEndpoints are reached from this machine. API credentials are environment variables, not story files.\n\nOne operation runs at a time. Queues are explicit and canceled before editing. Pricing is not available; call/output/time caps bound usage.")
	u.view.SetText(b.String())
	u.document.AddItem(u.view, 0, 1, true)
	u.actions([]string{"Models", "Feature defaults", "Limits", "Test base tools"}, []func(){u.modelMenu, u.defaultsDialog, u.limitsDialog, func() { u.startJob("test", u.state.Config.Root, u.state.Config.BaseModel, "") }})
}
func (u *UI) renderAssistant() {
	u.assistant.Clear()
	u.assistant.SetBorder(true)
	if u.conversation == nil {
		return
	}
	c := u.conversation
	verb := "Reads"
	if c.Mode == "edit" {
		verb = "Edits"
	}
	u.assistant.SetTitle(" " + verb + ": " + clean(u.state.Config.TitleOf(c.Target)) + " · " + c.Mode + " · " + clean(u.state.Config.Models[c.Model].Name) + " ")
	var b strings.Builder
	for _, turn := range c.Turns {
		fmt.Fprintf(&b, "%s: %s\n\n", turn.Role, turn.Text)
	}
	u.log.SetText(clean(b.String()))
	u.assistant.AddItem(u.log, 0, 1, false)
	compose := tview.NewFlex().AddItem(u.prompt, 0, 1, true).AddItem(u.button("Send ^R", u.send), 10, 0, false)
	u.assistant.AddItem(compose, 4, 0, true)
	bar := tview.NewFlex().AddItem(u.button("Sessions", u.sessionMenu), 0, 1, false).AddItem(u.button("Model", u.manualModelDialog), 0, 1, false).AddItem(u.button("Back to document", func() {
		u.compactAssistant = false
		u.assistantVisible = false
		u.layout()
		u.app.SetFocus(u.documentFocus())
	}), 0, 1, false)
	u.assistant.AddItem(bar, 1, 0, false)
}

type command struct {
	label, description string
	fn                 func()
	enabled            bool
}

func (u *UI) commands() []command {
	local := u.section == "Outline" || u.section == "Knowledge"
	outline := u.section == "Outline"
	can := !u.state.Busy
	return []command{
		{"Edit current field", "This source; story generation stays paused", u.edit, local && can},
		{"Discuss current item", "Start a conversation with an explicit scope", u.newConversation, local && can},
		{"Open in external editor", "VISUAL / EDITOR; reload on return", u.externalEditor, local && can},
		{"Add child outline", "Selected outline becomes the parent", u.addChild, outline && can},
		{"New knowledge entry", "Add a world fact, character, place, or another entry", u.addEntry, can},
		{"Rename current item", "Stable identity and references are preserved", u.rename, local && can},
		{"Generate outline detail", "Cached proposal inside this drafting brief", u.expand, outline && can},
		{"Generate prose", "Fill missing/invalidated passages in chosen scope", func() { u.generationDialog(false) }, outline && can},
		{"Regenerate prose", "Create replacement candidates in chosen scope", func() { u.generationDialog(true) }, outline && can},
		{"Invalidate prose", "Keep previous text and mark it for regeneration", u.invalidateDialog, outline && can},
		{"Invalidate generated cache", "Select outline/context/prose run records", u.invalidateCache, outline && can},
		{"Attach context", "Required knowledge or outline reference", u.attachDialog, outline && can},
		{"Review saved changes", "Choose invalidation for each saved change", u.changes, can},
		{"Reload external changes", "Reread files while the pipeline stays paused", u.reload, can},
		{"Export working manuscript", "Write exports/manuscript.md and its manifest", u.export, can},
		{"Browse navigator", "Useful in a narrow terminal", func() { u.navOnly = true; u.compactAssistant = false; u.layout(); u.app.SetFocus(u.tree) }, true},
		{"Assistant sessions", "Resume a saved conversation without retargeting", u.sessionMenu, can},
		{"Cancel active work", "Wait for cancellation before editing", u.stopWork, u.state.Busy},
		{"Help", "Keys, editing, model connections and limits", u.help, true},
	}
}
func (u *UI) commandMenu() {
	list := tview.NewList().ShowSecondaryText(true)
	query := tview.NewInputField().SetLabel("Find: ")
	items := u.commands()
	refresh := func(q string) {
		list.Clear()
		for _, c := range items {
			if !strings.Contains(strings.ToLower(c.label+" "+c.description), strings.ToLower(q)) {
				continue
			}
			cmd := c
			label := c.label
			desc := c.description
			if !c.enabled {
				label += " [unavailable]"
				desc = "Select the appropriate item; save the editor or stop active work first."
			}
			list.AddItem(label, desc, 0, func() {
				if !cmd.enabled {
					u.notice("Command unavailable in this state.")
					return
				}
				u.closeDialog()
				cmd.fn()
			})
		}
	}
	query.SetChangedFunc(refresh).SetDoneFunc(func(key tcell.Key) { u.app.SetFocus(list) })
	panel := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(query, 1, 0, true).AddItem(list, 0, 1, false).AddItem(u.button("Back", u.closeDialog), 1, 0, false)
	panel.SetBorder(true).SetTitle(" Commands · " + clean(u.state.Config.TitleOf(u.currentID())) + " ")
	refresh("")
	u.showDialog(panel, 90, 28, query)
}
func (u *UI) execute(cmd string) {
	if cmd == "mode" {
		if u.state.Busy {
			u.stopWork()
			return
		}
		if u.state.Editing {
			if u.editor != nil {
				u.notice("Save or cancel the open buffer first.")
				return
			}
			if err := u.core.FinishEditing(); err != nil {
				u.error(err)
				return
			}
			u.notice("Editing finished. Use Generate or Regenerate when ready.")
		} else {
			if err := u.core.BeginEditing(); err != nil {
				u.error(err)
				return
			}
			u.notice("Editing started. Generation is paused.")
		}
		u.refresh()
	}
}
func (u *UI) projectMenu() {
	list := tview.NewList().ShowSecondaryText(true)
	for _, c := range []command{{label: "Open another project", description: "Enter an existing project directory", fn: func() { u.openProject(false) }}, {label: "Create project", description: "Create in an empty directory", fn: func() { u.openProject(true) }}, {label: "Reload", description: "Reconcile external edits", fn: u.reload}, {label: "Export manuscript", description: "Working draft with gap markers", fn: u.export}} {
		cmd := c
		list.AddItem(cmd.label, cmd.description, 0, func() { u.closeDialog(); cmd.fn() })
	}
	list.AddItem("Back", "", 0, u.closeDialog)
	list.SetBorder(true).SetTitle(" Project ")
	u.showDialog(list, 72, 16, list)
}
func pretty(v any) string { b, _ := json.MarshalIndent(v, "", "  "); return string(b) }
