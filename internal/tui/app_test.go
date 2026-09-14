package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aipokalyptik/iterauthor/internal/model"
	"github.com/aipokalyptik/iterauthor/internal/project"
	"github.com/gdamore/tcell/v2"
)

type terminal struct {
	t      *testing.T
	u      *UI
	screen tcell.SimulationScreen
	done   chan error
}

type cancelClient struct{ started chan struct{} }

func (c cancelClient) Complete(ctx context.Context, _ project.Model, _ []model.Message, _ []model.Tool, _ int) (model.Response, error) {
	close(c.started)
	<-ctx.Done()
	return model.Response{}, ctx.Err()
}

func TestCancelStopsQueueBeforeEditing(t *testing.T) {
	tt := startTerminal(t, 120, 40)
	started := make(chan struct{})
	tt.onUI(func() {
		tt.u.client = cancelClient{started: started}
		tt.u.beginQueue([]string{"arrival", "visit"})
	})
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("model did not start")
	}
	tt.onUI(func() {
		tt.u.edit()
		if tt.u.editor != nil {
			t.Error("sources became editable while generation was active")
		}
		tt.u.stopWork()
	})
	tt.wait("prose: Canceled")
	tt.onUI(func() {
		if tt.u.busy || len(tt.u.queue) != 0 {
			t.Error("cancel left work active or queued")
		}
		tt.u.navigate("Outline", "visit")
		tt.u.edit()
		if tt.u.editor == nil {
			t.Error("editing did not become available after cancellation")
		}
	})
}

func startTerminal(t *testing.T, w, h int) *terminal {
	t.Helper()
	s, err := project.Create(filepath.Join(t.TempDir(), "story"), "Test", true)
	if err != nil {
		t.Fatal(err)
	}
	u := New(s, true)
	screen := tcell.NewSimulationScreen("UTF-8")
	u.app.SetScreen(screen)
	screen.SetSize(w, h)
	term := &terminal{t: t, u: u, screen: screen, done: make(chan error, 1)}
	go func() { term.done <- u.Run() }()
	t.Cleanup(func() {
		u.app.Stop()
		select {
		case err := <-term.done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(2 * time.Second):
			t.Error("terminal did not exit promptly")
		}
		u.Close()
	})
	term.wait("The Long Return")
	return term
}
func (tt *terminal) text() string {
	var b strings.Builder
	tt.onUI(func() {
		cells, w, h := tt.screen.GetContents()
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				cell := cells[y*w+x]
				if len(cell.Runes) == 0 {
					b.WriteRune(' ')
				} else {
					b.WriteRune(cell.Runes[0])
				}
			}
			b.WriteByte('\n')
		}
	})
	return b.String()
}

func (tt *terminal) wait(text string) {
	tt.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(tt.text(), text) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	tt.t.Fatalf("terminal did not show %q:\n%s", text, tt.text())
}
func (tt *terminal) key(key tcell.Key) { tt.u.app.QueueEvent(tcell.NewEventKey(key, 0, tcell.ModNone)) }
func (tt *terminal) typeText(text string) {
	for _, r := range text {
		tt.u.app.QueueEvent(tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone))
	}
}
func (tt *terminal) click(text string) {
	tt.t.Helper()
	tt.wait(text)
	rows := strings.Split(tt.text(), "\n")
	for y, row := range rows {
		if i := strings.Index(row, text); i >= 0 {
			x := len([]rune(row[:i]))
			tt.u.app.QueueEvent(tcell.NewEventMouse(x, y, tcell.Button1, tcell.ModNone))
			tt.u.app.QueueEvent(tcell.NewEventMouse(x, y, tcell.ButtonNone, tcell.ModNone))
			return
		}
	}
	tt.t.Fatal("click target disappeared")
}
func (tt *terminal) onUI(fn func()) {
	tt.t.Helper()
	done := make(chan struct{})
	go func() { tt.u.app.QueueUpdateDraw(func() { fn(); close(done) }) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		tt.t.Fatal("UI callback stalled")
	}
}

func TestMouseEditingAndModifierCommands(t *testing.T) {
	tt := startTerminal(t, 120, 40)
	tt.click("The kitchen scene")
	tt.wait("Mara asks Elias")
	tt.click("Edit")
	tt.wait("Ctrl+S Save")
	tt.typeText("Inserted text. ")
	tt.key(tcell.KeyCtrlS)
	tt.wait("1 changes")
	tt.onUI(func() {
		text, err := tt.u.store.Read("visit", "outline")
		if err != nil {
			t.Error(err)
		}
		if !strings.Contains(text, "Inserted text.") {
			t.Error("text not saved")
		}
		if !tt.u.editing {
			t.Error("save resumed generation")
		}
	})
	tt.key(tcell.KeyCtrlG)
	tt.wait("Find:")
	tt.typeText("help")
	tt.key(tcell.KeyEnter)
	tt.key(tcell.KeyEnter)
	tt.wait("Ctrl+R  Send message")
	tt.key(tcell.KeyEscape)
	tt.click("Knowledge")
	tt.wait("Mara Venn")
	if strings.Contains(tt.text(), "F8") || strings.Contains(tt.text(), "F2") {
		t.Fatal("function-key hints remain")
	}
}
func TestNarrowTerminalAndGeneration(t *testing.T) {
	tt := startTerminal(t, 80, 24)
	tt.wait("Notes")
	tt.key(tcell.KeyCtrlO)
	tt.wait("The kitchen scene")
	tt.click("The kitchen scene")
	tt.key(tcell.KeyEnter)
	tt.wait("Mara asks Elias")
	tt.click("Prose")
	tt.wait("Generate")
	tt.click("Regenerate")
	tt.wait("This branch")
	tt.click("This branch")
	tt.wait("prose: Available")
	tt.onUI(func() {
		text, _ := tt.u.store.Read("visit", "prose")
		if !strings.Contains(text, "DEMO") {
			t.Error("generated passage not saved")
		}
		runs, err := tt.u.store.Runs()
		if err != nil || len(runs) != 1 {
			t.Errorf("run not persisted: %v", err)
		}
		if runs[0].Calls > tt.u.store.Config.Limits.Calls {
			t.Error("run exceeded budget")
		}
	})
	if dir := os.Getenv("ITERAUTHOR_SCREEN_OUTPUT"); dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		_ = os.WriteFile(filepath.Join(dir, "terminal-80x24.txt"), []byte(tt.text()), 0644)
	}
}

func TestConversationScopeAndStagedEdits(t *testing.T) {
	tt := startTerminal(t, 120, 40)
	tt.click("The kitchen scene")
	tt.wait("Mara asks Elias")
	tt.click("Discuss")
	tt.wait("Start a focused conversation")
	tt.click("Advice / research")
	tt.key(tcell.KeyDown)
	tt.key(tcell.KeyEnter)
	tt.wait("Propose source edits")
	tt.click("Start conversation")
	tt.wait("Edits: The kitchen scene")
	tt.typeText("Add a beat where Mara notices the cup.")
	tt.onUI(func() {
		// Browsing to another item must not silently retarget this conversation.
		tt.u.navigate("Outline", "arrival")
	})
	tt.wait("Edits: The kitchen scene")
	tt.key(tcell.KeyCtrlR)
	tt.wait("edit: Available")
	tt.onUI(func() {
		text, _ := tt.u.store.Read("visit", "outline")
		if strings.Contains(text, "DEMO") {
			t.Error("model proposal changed a source before Apply")
		}
		if tt.u.conversation.Target != "visit" || len(tt.u.conversation.Turns) != 2 {
			t.Error("conversation lost its scope or turn history")
		}
		runs, err := tt.u.store.Runs()
		if err != nil || len(runs) != 1 {
			t.Errorf("expected one saved run: %v", err)
			return
		}
		tt.u.inspectRun(runs[0])
	})
	tt.wait("Apply proposals")
	tt.click("Apply proposals")
	tt.wait("Scoped edits applied")
	tt.onUI(func() {
		text, _ := tt.u.store.Read("visit", "outline")
		other, _ := tt.u.store.Read("arrival", "outline")
		if !strings.Contains(text, "DEMO proposed outline") || strings.Contains(other, "DEMO") {
			t.Error("proposal applied to the wrong source")
		}
		if !tt.u.editing || len(tt.u.store.State.Changes) != 1 {
			t.Error("applying a proposal did not pause generation and record the change")
		}
	})
}
