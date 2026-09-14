package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/aipokalyptik/iterauthor/internal/project"
	"github.com/rivo/tview"
)

func (u *UI) form(title string, f *tview.Form, save func() error) {
	f.SetBorder(true).SetTitle(" " + title + " ")
	f.SetItemPadding(0)
	f.AddButton("Save", func() {
		if err := save(); err != nil {
			u.notice(err.Error())
			f.SetTitle(" " + clean(err.Error()) + " ")
			return
		}
		u.closeDialog()
		u.refresh()
	}).AddButton("Cancel", u.closeDialog)
	f.SetCancelFunc(u.closeDialog)
	u.showDialog(f, 92, 28, f)
}
func (u *UI) itemForm(title string, save func(string, string) error) {
	name := tview.NewInputField().SetLabel("Title: ")
	body := tview.NewTextArea().SetLabel("Outline: ").SetSize(12, 0)
	f := tview.NewForm().AddFormItem(name).AddFormItem(body)
	u.form(title, f, func() error { return save(name.GetText(), body.GetText()) })
}

func (u *UI) addChild() {
	if !u.writable() {
		return
	}
	id := u.selected
	n := u.store.Config.Nodes[id]
	if n == nil {
		return
	}
	create := func(handling string) {
		u.itemForm("Add child to "+n.Title, func(title, text string) error {
			child, err := u.store.AddChild(id, title, text, handling)
			if err == nil {
				u.selected = child
				u.section = "Outline"
				u.tab = "Outline"
			}
			return err
		})
	}
	prose, err := u.store.Read(id, "prose")
	if err != nil {
		u.error(err)
		return
	}
	if len(n.Children) == 0 && prose != "" {
		u.choice("This leaf contains prose", "Adding a child makes it a parent. Keep the text as an outline reference for refinement, keep it as a private note, or discard its active contribution. Source history is retained.", []string{"Outline reference", "Private note", "Discard", "Cancel"}, func(i int) {
			if i < 3 {
				create([]string{"outline", "note", "discard"}[i])
			}
		})
	} else {
		create("")
	}
}
func (u *UI) addEntry() {
	if !u.writable() {
		return
	}
	name := tview.NewInputField().SetLabel("Title: ")
	kind := tview.NewInputField().SetLabel("Kind: ").SetText("World")
	body := tview.NewTextArea().SetLabel("Entry: ").SetSize(12, 0)
	f := tview.NewForm().AddFormItem(name).AddFormItem(kind).AddFormItem(body)
	u.form("New knowledge entry", f, func() error {
		id, err := u.store.AddEntry(name.GetText(), kind.GetText(), body.GetText())
		if err == nil {
			u.knowledge = id
			u.section = "Knowledge"
			u.tab = "Entry"
		}
		return err
	})
}
func (u *UI) rename() {
	if !u.writable() {
		return
	}
	id := u.currentID()
	name := tview.NewInputField().SetLabel("Title: ").SetText(u.store.Config.TitleOf(id))
	f := tview.NewForm().AddFormItem(name)
	u.form("Rename item", f, func() error {
		if strings.TrimSpace(name.GetText()) == "" {
			return fmt.Errorf("title is required")
		}
		c := u.store.Config.Clone()
		if n := c.Nodes[id]; n != nil {
			n.Title = name.GetText()
		} else if e := c.Knowledge[id]; e != nil {
			e.Title = name.GetText()
		} else {
			return fmt.Errorf("choose an outline or knowledge entry")
		}
		return u.store.SaveConfig(c, "Renamed item", id)
	})
}
func (u *UI) guidanceDialog() {
	if !u.writable() {
		return
	}
	id := u.selected
	c := u.store.Config.Clone()
	n := c.Nodes[id]
	if n == nil {
		return
	}
	if n.Models == nil {
		n.Models = map[string]string{}
	}
	f := tview.NewForm()
	f.AddCheckbox("Replace entire inherited style", n.ReplaceStyle, func(v bool) { n.ReplaceStyle = v })
	ids := append([]string{""}, c.ModelIDs()...)
	labels := []string{"Inherit"}
	for _, ref := range ids[1:] {
		labels = append(labels, c.Models[ref].Name+" ["+ref+"]")
	}
	for _, role := range project.Roles {
		r := role
		index := 0
		for i, ref := range ids {
			if ref == n.Models[r] {
				index = i
			}
		}
		f.AddDropDown(project.RoleNames[r], labels, index, func(_ string, i int) { n.Models[r] = ids[i] })
	}
	u.form("Local models and style mode", f, func() error {
		for role, ref := range n.Models {
			if ref != "" && requiresTools(role) && !c.Models[ref].Tools {
				return fmt.Errorf("%s requires tools", project.RoleNames[role])
			}
		}
		return u.store.SaveConfig(c, "Changed inherited guidance", id)
	})
}
func requiresTools(role string) bool {
	return role == "knowledge" || role == "outline-context" || role == "consistency"
}
func (u *UI) promptDialog() {
	if !u.writable() {
		return
	}
	id := u.selected
	c := u.store.Config.Clone()
	n := c.Nodes[id]
	if n == nil {
		return
	}
	if n.Prompts == nil {
		n.Prompts = map[string]string{}
	}
	role := project.Roles[0]
	text := tview.NewTextArea().SetLabel("Instructions: ").SetSize(12, 0).SetText(n.Prompts[role], false)
	var names []string
	for _, r := range project.Roles {
		names = append(names, project.RoleNames[r])
	}
	f := tview.NewForm().AddDropDown("Operation", names, 0, func(_ string, i int) {
		n.Prompts[role] = text.GetText()
		role = project.Roles[i]
		text.SetText(n.Prompts[role], false)
	}).AddFormItem(text)
	u.form("Inherited operation instructions", f, func() error {
		n.Prompts[role] = text.GetText()
		return u.store.SaveConfig(c, "Changed operation instructions", id)
	})
}
func (u *UI) contextDialog() {
	if !u.writable() {
		return
	}
	id := u.selected
	c := u.store.Config.Clone()
	n := c.Nodes[id]
	if n == nil {
		return
	}
	f := tview.NewForm()
	add := func(label string, value **bool) {
		idx := 0
		if *value != nil {
			idx = 2
			if **value {
				idx = 1
			}
		}
		f.AddDropDown(label, []string{"Inherit", "On", "Off"}, idx, func(_ string, i int) {
			if i == 0 {
				*value = nil
			} else {
				v := i == 1
				*value = &v
			}
		})
	}
	add("Automatic knowledge", &n.AutoKnowledge)
	add("Automatic outline summaries", &n.AutoOutline)
	u.form("Automatic context selection", f, func() error { return u.store.SaveConfig(c, "Changed automatic context", id) })
}
func (u *UI) attachDialog() {
	if !u.writable() {
		return
	}
	target := u.selected
	if u.store.Config.Nodes[target] == nil {
		return
	}
	c := u.store.Config.Clone()
	var ids, labels []string
	for id := range c.Knowledge {
		ids = append(ids, id)
	}
	for id := range c.Nodes {
		if id != target {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return c.TitleOf(ids[i]) < c.TitleOf(ids[j]) })
	for _, id := range ids {
		kind := "outline"
		if c.Knowledge[id] != nil {
			kind = "knowledge"
		}
		labels = append(labels, c.TitleOf(id)+" · "+kind)
	}
	if len(ids) == 0 {
		u.message("No references yet", "Create another outline or knowledge entry first.")
		return
	}
	index := 0
	desc := false
	f := tview.NewForm().AddDropDown("Reference", labels, 0, func(_ string, i int) { index = i }).AddCheckbox("Include outline descendants", false, func(v bool) { desc = v })
	u.form("Attach required context", f, func() error {
		ref := ids[index]
		for _, a := range c.Nodes[target].Attachments {
			if a.ID == ref {
				return fmt.Errorf("already attached at this item")
			}
		}
		c.Nodes[target].Attachments = append(c.Nodes[target].Attachments, project.Attachment{ID: ref, Descendants: desc && c.Nodes[ref] != nil})
		return u.store.SaveConfig(c, "Attached "+c.TitleOf(ref), target)
	})
}
func (u *UI) removeAttachment() {
	if !u.writable() {
		return
	}
	id := u.selected
	n := u.store.Config.Nodes[id]
	if n == nil {
		return
	}
	list := tview.NewList().ShowSecondaryText(true)
	for i, a := range n.Attachments {
		idx := i
		list.AddItem(u.store.Config.TitleOf(a.ID), "Remove this local attachment", 0, func() {
			c := u.store.Config.Clone()
			refs := c.Nodes[id].Attachments
			c.Nodes[id].Attachments = append(refs[:idx], refs[idx+1:]...)
			if err := u.store.SaveConfig(c, "Removed context attachment", id); err != nil {
				u.error(err)
				return
			}
			u.closeDialog()
			u.refresh()
		})
	}
	list.AddItem("Back", "Inherited attachments are edited at their origin.", 0, u.closeDialog)
	list.SetBorder(true).SetTitle(" Local attachments ")
	u.showDialog(list, 76, 20, list)
}
func (u *UI) modelMenu() {
	if !u.writable() {
		return
	}
	list := tview.NewList().ShowSecondaryText(true)
	for _, id := range u.store.Config.ModelIDs() {
		ref := id
		m := u.store.Config.Models[id]
		list.AddItem(m.Name+" ["+id+"]", m.URL+" · "+m.Model, 0, func() { u.closeDialog(); u.modelDialog(ref) })
	}
	list.AddItem("Add connection", "Each connection can point at a different local/LAN/hosted model.", 0, func() { u.closeDialog(); u.modelDialog("") })
	list.AddItem("Back", "", 0, u.closeDialog)
	list.SetBorder(true).SetTitle(" Model connections ")
	u.showDialog(list, 90, 24, list)
}
func (u *UI) modelDialog(id string) {
	c := u.store.Config.Clone()
	m := c.Models[id]
	if id == "" {
		id = project.NewID()
		m = project.Model{Name: "New model", URL: "http://127.0.0.1:1234/v1", Tools: true, TokenField: "max_tokens"}
	}
	f := tview.NewForm()
	f.AddInputField("Label", m.Name, 45, nil, func(v string) { m.Name = v }).AddInputField("API base URL", m.URL, 55, nil, func(v string) { m.URL = v }).AddInputField("Model identifier", m.Model, 55, nil, func(v string) { m.Model = v }).AddInputField("API key environment variable", m.KeyEnv, 40, nil, func(v string) { m.KeyEnv = v }).AddCheckbox("Supports tools (test separately)", m.Tools, func(v bool) { m.Tools = v })
	base := c.BaseModel == id
	f.AddCheckbox("Use as base model", base, func(v bool) { base = v })
	idx := 0
	if m.TokenField == "max_completion_tokens" {
		idx = 1
	}
	f.AddDropDown("Output limit field", []string{"max_tokens", "max_completion_tokens"}, idx, func(v string, _ int) { m.TokenField = v })
	u.form("Configure model connection", f, func() error {
		if strings.TrimSpace(m.Name) == "" || strings.TrimSpace(m.Model) == "" {
			return fmt.Errorf("label and model identifier are required")
		}
		if base && !m.Tools {
			return fmt.Errorf("the base model must support tools")
		}
		c.Models[id] = m
		if base {
			c.BaseModel = id
		}
		return u.store.SaveConfig(c, "Changed model connection", c.Root)
	})
}
func (u *UI) defaultsDialog() {
	if !u.writable() {
		return
	}
	c := u.store.Config.Clone()
	if c.Defaults == nil {
		c.Defaults = map[string]string{}
	}
	ids := append([]string{""}, c.ModelIDs()...)
	labels := []string{"Use base model"}
	for _, id := range ids[1:] {
		labels = append(labels, c.Models[id].Name)
	}
	f := tview.NewForm()
	for _, role := range project.Roles {
		r := role
		index := 0
		for i, id := range ids {
			if id == c.Defaults[r] {
				index = i
			}
		}
		f.AddDropDown(project.RoleNames[r], labels, index, func(_ string, i int) { c.Defaults[r] = ids[i] })
	}
	u.form("Feature model defaults", f, func() error {
		for r, id := range c.Defaults {
			if id != "" && requiresTools(r) && !c.Models[id].Tools {
				return fmt.Errorf("%s requires tools", project.RoleNames[r])
			}
		}
		return u.store.SaveConfig(c, "Changed feature model defaults", c.Root)
	})
}
func (u *UI) limitsDialog() {
	if !u.writable() {
		return
	}
	c := u.store.Config.Clone()
	f := tview.NewForm()
	add := func(label string, value *int) {
		f.AddInputField(label, strconv.Itoa(*value), 10, tview.InputFieldInteger, func(v string) { *value, _ = strconv.Atoi(v) })
	}
	add("Draft attempts (1–10)", &c.Limits.Drafts)
	add("Total model calls (1–200)", &c.Limits.Calls)
	add("Output tokens per call", &c.Limits.OutputTokens)
	add("Input characters per call", &c.Limits.ContextChars)
	add("Minutes per operation", &c.Limits.Minutes)
	f.AddCheckbox("Generate when editing is finished", c.AutoGenerate, func(v bool) { c.AutoGenerate = v })
	u.form("Generation limits", f, func() error { return u.store.SaveConfig(c, "Changed generation limits", c.Root) })
}

func (u *UI) changes() {
	if !u.writable() {
		return
	}
	if len(u.store.State.Changes) == 0 {
		u.message("Saved changes", "No unresolved changes. Use Finish editing to release the pause.")
		return
	}
	list := tview.NewList().ShowSecondaryText(true)
	for i, c := range u.store.State.Changes {
		index := i
		list.AddItem(u.store.Config.TitleOf(c.Target), c.Description, 0, func() { u.closeDialog(); u.decideChange(index) })
	}
	list.AddItem("Keep existing prose for ALL changes", "Explicitly retain current text; recorded inputs remain historical.", 0, func() {
		for len(u.store.State.Changes) > 0 {
			if err := u.store.Decide(0, "keep", nil); err != nil {
				u.error(err)
				return
			}
		}
		u.closeDialog()
		u.refresh()
		u.notice("Choices saved. Finish editing when ready.")
	})
	list.AddItem("Invalidate entire story for ALL changes", "Authored prose stays protected; generation creates candidates.", 0, func() {
		for len(u.store.State.Changes) > 0 {
			if err := u.store.Decide(0, "story", u.store.Config.Leaves(u.store.Config.Root)); err != nil {
				u.error(err)
				return
			}
		}
		u.closeDialog()
		u.refresh()
	})
	list.AddItem("Back to editing", "", 0, u.closeDialog)
	list.SetBorder(true).SetTitle(" Saved changes · each needs a decision ")
	u.showDialog(list, 90, 28, list)
}
func (u *UI) decideChange(index int) {
	c := u.store.State.Changes[index]
	branchLabel := "This branch"
	if u.store.Config.Knowledge[c.Target] != nil {
		branchLabel = "Linked passages"
	}
	u.choice("Reconsider generated prose", u.store.Config.TitleOf(c.Target)+"\n"+c.Description, []string{"Keep", branchLabel, "Selected passages", "Entire story", "Back"}, func(i int) {
		if i == 4 {
			return
		}
		var ids []string
		choice := "keep"
		switch i {
		case 1:
			choice = "branch"
			if u.store.Config.Nodes[c.Target] != nil {
				ids = u.store.Config.Leaves(c.Target)
			} else {
				for id := range u.store.Config.Nodes {
					for _, ref := range u.store.Config.Required(id) {
						if ref == c.Target {
							ids = append(ids, u.store.Config.Leaves(id)...)
							break
						}
					}
				}
			}
		case 2:
			u.selectPassages("Select affected passages", func(ids []string) {
				if err := u.store.Decide(index, "selected", ids); err != nil {
					u.error(err)
					return
				}
				u.refresh()
			})
			return
		case 3:
			choice = "story"
			ids = u.store.Config.Leaves(u.store.Config.Root)
		}
		if err := u.store.Decide(index, choice, ids); err != nil {
			u.error(err)
			return
		}
		u.refresh()
		u.notice("Invalidation choice saved. Finish editing when ready.")
	})
}
func (u *UI) selectPassages(title string, done func([]string)) {
	selected := map[string]bool{}
	f := tview.NewForm()
	for _, id := range u.store.Config.Leaves(u.store.Config.Root) {
		ref := id
		f.AddCheckbox(u.store.Config.TitleOf(id), false, func(v bool) { selected[ref] = v })
	}
	f.AddButton("Apply", func() {
		var ids []string
		for _, id := range u.store.Config.Leaves(u.store.Config.Root) {
			if selected[id] {
				ids = append(ids, id)
			}
		}
		if len(ids) == 0 {
			u.notice("Select at least one passage.")
			return
		}
		u.closeDialog()
		done(ids)
	}).AddButton("Back", u.closeDialog)
	f.SetBorder(true).SetTitle(" " + title + " ")
	u.showDialog(f, 90, 28, f)
}
func (u *UI) invalidateDialog() {
	if !u.writable() {
		return
	}
	id := u.selected
	u.choice("Invalidate prose", u.store.Config.TitleOf(id)+"\nOld text stays inspectable. Authored text remains protected.", []string{"This branch", "Selected passages", "Entire story", "Back"}, func(i int) {
		var ids []string
		switch i {
		case 0:
			ids = u.store.Config.Leaves(id)
		case 1:
			u.selectPassages("Invalidate selected passages", func(ids []string) {
				if err := u.store.Invalidate(ids); err != nil {
					u.error(err)
				}
				u.refresh()
			})
			return
		case 2:
			ids = u.store.Config.Leaves(u.store.Config.Root)
		default:
			return
		}
		if err := u.store.Invalidate(ids); err != nil {
			u.error(err)
		}
		u.refresh()
	})
}
func (u *UI) invalidateCache() {
	if !u.writable() {
		return
	}
	runs, err := u.store.Runs()
	if err != nil {
		u.error(err)
		return
	}
	list := tview.NewList().ShowSecondaryText(true)
	for _, r := range runs {
		if !u.store.Config.Contains(u.selected, r.Target) {
			continue
		}
		run := r
		list.AddItem(run.Kind+" · "+u.store.Config.TitleOf(run.Target), run.Started+" · "+run.Status, 0, func() {
			u.store.State.InvalidRuns[run.ID] = true
			if run.Kind == "prose" {
				_ = u.store.Invalidate([]string{run.Target})
			}
			if err := u.store.SaveState(); err != nil {
				u.error(err)
				return
			}
			u.closeDialog()
			u.refresh()
			u.notice("Run invalidated; source imports and prior records retained.")
		})
	}
	list.AddItem("Back", "", 0, u.closeDialog)
	list.SetBorder(true).SetTitle(" Invalidate a generated run in this branch ")
	u.showDialog(list, 90, 28, list)
}
func (u *UI) openProject(create bool) {
	if !u.writable() {
		return
	}
	dir := ""
	title := "My story"
	f := tview.NewForm().AddInputField("Project directory", "", 60, nil, func(v string) { dir = v })
	if create {
		f.AddInputField("Story title", title, 50, nil, func(v string) { title = v })
	}
	u.form("Project directory on this machine", f, func() error {
		if strings.HasPrefix(dir, "~/") {
			home, _ := os.UserHomeDir()
			dir = filepath.Join(home, dir[2:])
		}
		if dir == "" {
			return fmt.Errorf("directory is required")
		}
		var next *project.Store
		var err error
		if create {
			next, err = project.Create(dir, title, false)
		} else {
			next, err = project.Open(dir)
		}
		if err != nil {
			return err
		}
		if u.conversation != nil {
			if err = u.store.SaveConversation(*u.conversation); err != nil {
				next.Close()
				return err
			}
		}
		u.store.Close()
		u.store = next
		u.selected = next.Config.Root
		u.knowledge = ""
		for id := range next.Config.Knowledge {
			u.knowledge = id
			break
		}
		u.conversation = nil
		u.assistantVisible = false
		u.section = "Outline"
		u.tab = "Outline"
		return nil
	})
}
