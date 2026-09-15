package tui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/aipokalyptik/iterauthor/internal/application"
	"github.com/aipokalyptik/iterauthor/internal/project"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type UI struct {
	documentMain                                tview.Primitive
	app                                         *tview.Application
	core                                        *application.Service
	state                                       application.View
	screenMu                                    sync.Mutex
	screen                                      tcell.Screen
	presentedRun                                string
	pages                                       *tview.Pages
	root, work, document, tabbar, assistant     *tview.Flex
	tree                                        *tview.TreeView
	view, log, status, title                    *tview.TextView
	prompt, editor                              *tview.TextArea
	modeButton                                  *tview.Button
	section, selected, knowledge, tab           string
	assistantVisible, navOnly, compactAssistant bool
	width, height                               int
	editID, editField, editOriginal             string
	dialog                                      bool
	returnFocus                                 tview.Primitive
	conversation                                *project.Conversation
	quitPending                                 bool
	progress                                    string
	focusables                                  []tview.Primitive
	runs                                        []project.Run
	layoutState                                 string
}

func New(core *application.Service) *UI {
	state := core.View()
	u := &UI{core: core, state: state, section: "Outline", selected: state.Config.Root, tab: "Outline"}
	for id := range state.Config.Knowledge {
		u.knowledge = id
		break
	}
	u.app = tview.NewApplication().EnableMouse(true).EnablePaste(true)
	// tview 0.42 classifies rapid clicks at different coordinates as a double
	// click. Buttons ignore those, so normalize only clicks that moved targets.
	lastClickX, lastClickY := -1, -1
	u.app.SetMouseCapture(func(event *tcell.EventMouse, action tview.MouseAction) (*tcell.EventMouse, tview.MouseAction) {
		x, y := event.Position()
		if action == tview.MouseLeftDoubleClick && (x != lastClickX || y != lastClickY) {
			action = tview.MouseLeftClick
		}
		if action == tview.MouseLeftClick || action == tview.MouseLeftDoubleClick {
			lastClickX, lastClickY = x, y
		}
		return event, action
	})
	u.pages = tview.NewPages()
	u.root = tview.NewFlex().SetDirection(tview.FlexRow)
	u.title = tview.NewTextView()
	u.status = tview.NewTextView().SetDynamicColors(false)
	u.tree = tview.NewTreeView()
	u.tree.SetBorder(true).SetTitle(" Outline ")
	u.view = tview.NewTextView().SetWrap(true).SetWordWrap(true)
	u.view.SetBorderPadding(0, 0, 1, 1)
	u.log = tview.NewTextView().SetWrap(true).SetWordWrap(true)
	u.prompt = tview.NewTextArea().SetPlaceholder("Enter adds a newline. Ctrl+R sends.")
	u.prompt.SetBorder(true).SetTitle(" Message · Ctrl+R Send ")
	u.prompt.SetChangedFunc(func() {
		if u.conversation != nil {
			u.conversation.Draft = u.prompt.GetText()
		}
	})
	u.document = tview.NewFlex().SetDirection(tview.FlexRow)
	u.document.SetBorder(true)
	u.tabbar = tview.NewFlex()
	u.work = tview.NewFlex()
	u.assistant = tview.NewFlex().SetDirection(tview.FlexRow)
	u.modeButton = tview.NewButton("Finish editing").SetSelectedFunc(func() { u.execute("mode") })
	top := tview.NewFlex()
	top.AddItem(u.button("Project", func() { u.projectMenu() }), 10, 0, false).AddItem(u.button("Commands ^G", func() { u.commandMenu() }), 15, 0, false).AddItem(u.modeButton, 20, 0, false).AddItem(u.button("Assistant", func() { u.toggleAssistant() }), 12, 0, false).AddItem(u.button("Help", func() { u.help() }), 10, 0, false).AddItem(u.button("Quit", func() { u.quit() }), 7, 0, false)
	sections := tview.NewFlex()
	for _, name := range []string{"Outline", "Knowledge", "Manuscript", "Activity", "Settings"} {
		section := name
		sections.AddItem(u.button(name, func() { u.navigate(section, "") }), len(name)+3, 0, false)
	}
	u.root.AddItem(u.title, 1, 0, false).AddItem(top, 1, 0, false).AddItem(sections, 1, 0, false).AddItem(u.work, 0, 1, true).AddItem(u.status, 1, 0, false)
	u.pages.AddPage("main", u.root, true, true)
	u.tree.SetSelectedFunc(func(n *tview.TreeNode) {
		if len(n.GetChildren()) > 0 {
			n.SetExpanded(!n.IsExpanded())
		}
		u.navOnly = false
		u.layout()
		u.app.SetFocus(u.documentFocus())
	})
	u.app.SetRoot(u.pages, true).SetInputCapture(u.key)
	u.app.SetBeforeDrawFunc(func(screen tcell.Screen) bool {
		u.screenMu.Lock()
		u.screen = screen
		u.screenMu.Unlock()
		// Drawing holds tview's application lock. Only enqueue a wakeup here;
		// refreshing views, changing focus, and stopping belong to key handling.
		if u.core.Revision() != u.state.Revision {
			_ = screen.PostEvent(tcell.NewEventKey(tcell.KeyNUL, 0, tcell.ModNone))
		}
		w, h := screen.Size()
		if w != u.width || h != u.height {
			u.width, u.height = w, h
			u.layout()
		}
		return false
	})
	u.refresh()
	u.app.SetFocus(u.tree)
	return u
}

// Notifications only request a redraw. PostEvent is nonblocking, unlike tview's
// QueueUpdateDraw, so a stopped terminal cannot strand a worker or UI observer.
func (u *UI) Run() error {
	events, unsubscribe := u.core.Subscribe()
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			case _, ok := <-events:
				if !ok {
					return
				}
				u.screenMu.Lock()
				screen := u.screen
				u.screenMu.Unlock()
				if screen != nil {
					_ = screen.PostEvent(tcell.NewEventKey(tcell.KeyNUL, 0, tcell.ModNone))
				}
			}
		}
	}()
	defer func() { close(stop); unsubscribe(); <-done }()
	return u.app.Run()
}
func (u *UI) Application() *tview.Application { return u.app }
func (u *UI) button(label string, fn func()) *tview.Button {
	b := tview.NewButton(tview.Escape(label)).SetSelectedFunc(fn)
	b.SetStyle(tcell.StyleDefault.Background(tcell.ColorDarkSlateGray).Foreground(tcell.ColorWhite))
	return b
}
func (u *UI) currentID() string {
	if u.section == "Knowledge" {
		return u.knowledge
	}
	return u.selected
}
func (u *UI) notice(s string) { u.progress = s; u.chrome() }
func (u *UI) chrome() {
	mode := "READY"
	label := "Begin editing"
	if u.state.Editing {
		mode = "EDITING"
		label = "Finish editing"
	}
	if u.state.Busy {
		mode = "WORKING"
		label = "Cancel work"
	}
	u.modeButton.SetLabel(label)
	demo := ""
	if u.state.Demo {
		demo = " · DEMO MODEL"
	}
	u.title.SetText(fmt.Sprintf(" Iterauthor · %s · %s%s", u.state.Config.Title, mode, demo))
	target := u.state.Config.TitleOf(u.currentID())
	u.status.SetText(fmt.Sprintf(" %s · %d changes · %s", target, len(u.state.State.Changes), u.progress))
}
func (u *UI) syncState() {
	state := u.core.View()
	if state.Progress != u.state.Progress {
		u.progress = state.Progress
	}
	if state.LastRun != u.state.LastRun && u.conversation != nil {
		if c, err := u.core.LoadConversation(u.conversation.ID); err == nil {
			c.Draft = u.prompt.GetText()
			u.conversation = &c
		}
	}
	u.state = state
}
func (u *UI) refresh() {
	u.syncState()
	u.rebuildTree()
	u.renderDocument()
	u.renderAssistant()
	u.layout()
	u.chrome()
}
func (u *UI) layout() {
	if u.work == nil {
		return
	}
	u.focusables = []tview.Primitive{u.tree, u.documentFocus()}
	if u.editor != nil {
		u.focusables[1] = u.editor
	}
	if u.assistantVisible {
		u.focusables = append(u.focusables, u.prompt)
	}
	// TreeView can report selection changes while drawing. Preserve the existing
	// containers when only document contents changed, or mouse events would hit
	// a replacement Flex which has not received its layout rectangle yet.
	state := fmt.Sprintf("%d/%d/%t/%t/%t", u.width, u.height, u.assistantVisible, u.compactAssistant, u.navOnly)
	if state == u.layoutState {
		return
	}
	u.layoutState = state
	u.work.Clear()
	narrow := u.width > 0 && (u.width < 100 || u.height < 32)
	if u.assistantVisible && narrow && u.compactAssistant {
		u.work.AddItem(u.assistant, 0, 1, true)
	} else {
		content := tview.NewFlex()
		if u.width >= 100 || u.navOnly {
			size := 25
			if u.navOnly && u.width < 100 {
				size = 0
			}
			content.AddItem(u.tree, size, 1, u.navOnly)
		}
		if !u.navOnly || u.width >= 100 {
			content.AddItem(u.document, 0, 3, !u.navOnly)
		}
		if u.assistantVisible && !narrow {
			stack := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(content, 0, 1, true).AddItem(u.assistant, 12, 0, false)
			u.work.AddItem(stack, 0, 1, true)
		} else {
			u.work.AddItem(content, 0, 1, true)
		}
	}
}
func (u *UI) navigate(section, id string) {
	u.guardBuffer(func() {
		u.section = section
		if id != "" {
			if section == "Knowledge" {
				u.knowledge = id
			} else {
				u.selected = id
			}
		}
		if section == "Knowledge" {
			u.tab = "Entry"
		} else {
			u.tab = "Outline"
		}
		u.navOnly = false
		u.compactAssistant = false
		u.refresh()
	})
}
func (u *UI) guardBuffer(next func()) {
	if u.editor != nil && u.editor.GetText() != u.editOriginal {
		u.choice("Unsaved text", "Save this buffer before leaving it?", []string{"Save and continue", "Discard buffer", "Keep editing"}, func(i int) {
			if i == 0 {
				if u.saveEdit() {
					next()
				}
			} else if i == 1 {
				u.editor = nil
				next()
			}
		})
		return
	}
	u.editor = nil
	next()
}
func (u *UI) writable() bool {
	if u.state.Busy {
		u.notice("Cancel work and wait for it to stop before editing.")
		return false
	}
	if u.editor != nil {
		u.notice("Save or cancel the current text buffer first.")
		return false
	}
	if err := u.core.BeginEditing(); err != nil {
		u.error(err)
		return false
	}
	u.syncState()
	return true
}
func (u *UI) key(e *tcell.EventKey) *tcell.EventKey {
	if u.core.Revision() != u.state.Revision {
		u.refresh()
	}
	if !u.state.Busy {
		if u.quitPending {
			u.quitPending = false
			if u.finishQuit() {
				return nil
			}
		}
		if u.state.LastRun != "" && u.presentedRun != u.state.LastRun {
			u.presentedRun = u.state.LastRun
			if run, err := u.core.LoadRun(u.state.LastRun); err == nil && (run.Kind == "outline" || run.Kind == "test") {
				u.inspectRun(run)
			}
		}
	}
	switch e.Key() {
	case tcell.KeyNUL:
		return nil
	case tcell.KeyCtrlC:
		u.quit()
		return nil
	case tcell.KeyCtrlG:
		if !u.dialog {
			u.commandMenu()
		}
		return nil
	case tcell.KeyCtrlO:
		if !u.dialog {
			u.navOnly = !u.navOnly
			u.compactAssistant = false
			u.layout()
			if u.navOnly {
				u.app.SetFocus(u.tree)
			} else {
				u.app.SetFocus(u.documentFocus())
			}
		}
		return nil
	case tcell.KeyCtrlT:
		if !u.dialog {
			u.cycleFocus()
		}
		return nil
	case tcell.KeyCtrlR:
		if !u.dialog {
			u.send()
		}
		return nil
	case tcell.KeyCtrlS:
		if u.editor != nil && !u.dialog {
			u.saveEdit()
			return nil
		}
	case tcell.KeyEscape:
		if u.dialog {
			u.closeDialog()
			return nil
		}
		if u.compactAssistant {
			u.compactAssistant = false
			u.layout()
			u.app.SetFocus(u.documentFocus())
			return nil
		}
	case tcell.KeyTab, tcell.KeyBacktab:
		if !u.dialog {
			u.cycleControl(e.Key() == tcell.KeyBacktab)
			return nil
		}
	}
	return e
}
func (u *UI) documentFocus() tview.Primitive {
	if u.editor != nil {
		return u.editor
	}
	if u.documentMain != nil {
		return u.documentMain
	}
	return u.view
}

// Tab traverses the visible controls; Ctrl+T remains the shorter pane circuit.
func (u *UI) cycleControl(backward bool) {
	var controls []tview.Primitive
	var walk func(tview.Primitive)
	walk = func(p tview.Primitive) {
		if flex, ok := p.(*tview.Flex); ok {
			for i := 0; i < flex.GetItemCount(); i++ {
				walk(flex.GetItem(i))
			}
			return
		}
		if p != u.title && p != u.status {
			controls = append(controls, p)
		}
	}
	walk(u.root)
	if len(controls) == 0 {
		return
	}
	index := -1
	for i, p := range controls {
		if p == u.app.GetFocus() {
			index = i
			break
		}
	}
	step := 1
	if backward {
		step = -1
		if index < 0 {
			index = 0
		}
	}
	u.app.SetFocus(controls[(index+step+len(controls))%len(controls)])
}
func (u *UI) cycleFocus() {
	current := u.app.GetFocus()
	i := -1
	for n, p := range u.focusables {
		if p == current {
			i = n
		}
	}
	next := u.focusables[(i+1)%len(u.focusables)]
	if next == u.tree && u.width < 100 {
		u.navOnly = true
		u.compactAssistant = false
	}
	if next == u.prompt {
		u.compactAssistant = true
	}
	if next == u.documentFocus() {
		u.navOnly = false
		u.compactAssistant = false
	}
	u.layout()
	u.app.SetFocus(next)
}
func (u *UI) quit() {
	if u.dialog {
		u.closeDialog()
	}
	if u.state.Busy {
		u.choice("Work is running", "Cancel active work, save its partial history, and quit?", []string{"Cancel and quit", "Stay"}, func(i int) {
			if i == 0 {
				u.quitPending = true
				u.stopWork()
			}
		})
		return
	}
	u.guardBuffer(func() { u.finishQuit() })
}
func (u *UI) finishQuit() bool {
	if u.conversation != nil {
		if err := u.core.SaveDraft(u.conversation.ID, u.prompt.GetText()); err != nil {
			u.error(err)
			return false
		}
	}
	u.app.Stop()
	return true
}
func (u *UI) stopWork() {
	u.core.Cancel()
	u.refresh()
}
func (u *UI) externalEditor() {
	if !u.writable() {
		return
	}
	id := u.currentID()
	field := u.field()
	path, err := u.core.PrepareExternalEdit(id, field)
	if err != nil {
		u.error(err)
		return
	}
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	// The editor setting is a trusted user command; the source path is passed as $1,
	// never interpolated into shell text. Models have no access to this operation.
	u.app.Suspend(func() {
		cmd := exec.Command("sh", "-c", editor+` "$1"`, "iterauthor-editor", path)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err = cmd.Run()
	})
	if err != nil {
		u.error(fmt.Errorf("external editor: %w", err))
		return
	}
	u.reload()
}
func (u *UI) reload() {
	if !u.writable() {
		return
	}
	if err := u.core.Reload(); err != nil {
		u.error(err)
		return
	}
	u.state = u.core.View()
	if u.state.Config.Nodes[u.selected] == nil {
		u.selected = u.state.Config.Root
	}
	if u.state.Config.Knowledge[u.knowledge] == nil {
		u.knowledge = ""
		for id := range u.state.Config.Knowledge {
			u.knowledge = id
			break
		}
	}
	u.refresh()
	u.notice("Files reloaded. Generation remains paused.")
}
func (u *UI) field() string {
	if u.tab == "Notes" {
		return "notes"
	}
	if u.tab == "Prose" {
		return "prose"
	}
	if u.tab == "Guidance" {
		return "style"
	}
	if u.section == "Knowledge" {
		return "entry"
	}
	return "outline"
}
func (u *UI) edit() {
	if !u.writable() {
		return
	}
	id, field := u.currentID(), u.field()
	if n := u.state.Config.Nodes[id]; n != nil && field == "prose" && len(n.Children) > 0 {
		u.notice("Select a leaf to edit its prose.")
		return
	}
	text, err := u.core.Read(id, field)
	if err != nil {
		u.error(err)
		return
	}
	u.editID = id
	u.editField = field
	u.editOriginal = text
	u.editor = tview.NewTextArea().SetText(text, false)
	u.editor.SetBorder(true).SetTitle(" Edit " + field + " · Ctrl+S Save · Tab leaves editor ")
	u.renderDocument()
	u.layout()
	u.app.SetFocus(u.editor)
	u.chrome()
}
func (u *UI) saveEdit() bool {
	if u.editor == nil {
		return true
	}
	if err := u.core.SaveText(u.editID, u.editField, u.editor.GetText(), u.editOriginal); err != nil {
		u.error(err)
		return false
	}
	u.editor = nil
	u.refresh()
	u.app.SetFocus(u.documentFocus())
	if len(u.state.State.Changes) > 0 {
		u.notice("Saved. Review affected prose before generating again.")
	} else {
		u.notice("Saved. Generate only when you are ready.")
	}
	return true
}
func (u *UI) export() {
	path, err := u.core.Export()
	if err != nil {
		u.error(err)
		return
	}
	u.message("Working manuscript exported", path+"\n\nMissing and invalidated passages are visibly marked. The adjacent manifest records source hashes and passage status.")
}
func (u *UI) toggleAssistant() {
	if u.conversation == nil {
		u.newConversation()
		return
	}
	u.assistantVisible = !u.assistantVisible
	u.compactAssistant = u.assistantVisible
	u.renderAssistant()
	u.layout()
	if u.assistantVisible {
		u.app.SetFocus(u.prompt)
	}
}

func (u *UI) help() {
	u.inspectText("Iterauthor · test version", `Click sections, tree rows, tabs, buttons and fields. Click inside an editor to place the cursor. Mouse wheel scrolls the pane under the pointer.

Ctrl+G  Commands     Ctrl+O  Navigator/document
Ctrl+T  Next pane    Ctrl+R  Send message
Ctrl+S  Save text    Ctrl+C  Clean exit
Tab leaves an editor; Enter in a message inserts a newline.
Escape closes a dialog or returns from compact assistant view.
Ctrl+C requests a clean exit. All main actions also have buttons.

The viewed item and the conversation's edit scope are independent.
Save keeps the pipeline paused. Writing changes require review when prose exists.
While work is running, cancel and wait before editing any sources.
External editing uses VISUAL, then EDITOR, then vi; Reload on return.

Notes are private: models cannot read, search, or edit them.
Model edit proposals require Apply; inspect them in Activity.
This build uses one active operation at a time. Runs and proposals are saved in .twriter/runs. Run under tmux if you want work to survive an SSH disconnect.

Model endpoints are reached from the machine running iterauthor.
Demo mode uses marked synthetic outputs and never calls a provider.`)
}

func (u *UI) inspectText(title, text string) {
	view := tview.NewTextView().SetText(text).SetWrap(true).SetWordWrap(true)
	view.SetBorder(true).SetTitle(" " + title + " ")
	body := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(view, 0, 1, true).AddItem(u.button("Back", func() { u.closeDialog() }), 1, 0, false)
	u.showDialog(body, 90, 28, view)
}
func (u *UI) error(err error)            { u.message("Unable to complete operation", err.Error()) }
func (u *UI) message(title, text string) { u.choice(title, text, []string{"Back"}, func(int) {}) }
func (u *UI) choice(title, text string, labels []string, done func(int)) {
	m := tview.NewModal().SetText(title + "\n\n" + text).AddButtons(labels).SetDoneFunc(func(i int, _ string) { u.closeDialog(); done(i) })
	u.showDialog(m, 76, 18, m)
}
func (u *UI) showDialog(p tview.Primitive, width, height int, focus tview.Primitive) {
	if u.dialog {
		u.pages.RemovePage("dialog")
	} else {
		u.returnFocus = u.app.GetFocus()
	}
	u.dialog = true
	u.pages.AddPage("dialog", &overlay{Box: tview.NewBox(), child: p, width: width, height: height}, true, true)
	u.app.SetFocus(focus)
}
func (u *UI) closeDialog() {
	u.pages.RemovePage("dialog")
	u.dialog = false
	if u.returnFocus != nil {
		u.app.SetFocus(u.returnFocus)
	} else {
		u.app.SetFocus(u.documentFocus())
	}
}

type overlay struct {
	*tview.Box
	child         tview.Primitive
	width, height int
}

func (o *overlay) Draw(s tcell.Screen) {
	x, y, w, h := o.GetInnerRect()
	cw, ch := min(o.width, w), min(o.height, h)
	o.child.SetRect(x+(w-cw)/2, y+(h-ch)/2, cw, ch)
	o.child.Draw(s)
}
func (o *overlay) Focus(delegate func(tview.Primitive)) { delegate(o.child) }
func (o *overlay) HasFocus() bool                       { return o.child.HasFocus() }
func (o *overlay) InputHandler() func(*tcell.EventKey, func(tview.Primitive)) {
	return o.WrapInputHandler(func(event *tcell.EventKey, focus func(tview.Primitive)) {
		if handler := o.child.InputHandler(); handler != nil {
			handler(event, focus)
		}
	})
}
func (o *overlay) PasteHandler() func(string, func(tview.Primitive)) {
	return o.WrapPasteHandler(func(text string, focus func(tview.Primitive)) {
		if handler := o.child.PasteHandler(); handler != nil {
			handler(text, focus)
		}
	})
}
func (o *overlay) MouseHandler() func(tview.MouseAction, *tcell.EventMouse, func(tview.Primitive)) (bool, tview.Primitive) {
	return o.WrapMouseHandler(func(a tview.MouseAction, e *tcell.EventMouse, f func(tview.Primitive)) (bool, tview.Primitive) {
		if h := o.child.MouseHandler(); h != nil {
			return h(a, e, f)
		}
		return false, nil
	})
}

func clean(text string) string { return strings.ReplaceAll(text, "\x1b", "") }
