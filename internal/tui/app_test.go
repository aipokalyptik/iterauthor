package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aipokalyptik/iterauthor/internal/application"
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
	started := make(chan struct{})
	tt := startTerminal(t, 120, 40, cancelClient{started: started})
	tt.onUI(func() {
		tt.u.generate(application.Selection{Scope: "selected", IDs: []string{"arrival", "visit"}}, false)
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
		if tt.u.state.Busy || len(tt.u.state.Queue) != 0 {
			t.Error("cancel left work active or queued")
		}
		tt.u.navigate("Outline", "visit")
		tt.u.edit()
		if tt.u.editor == nil {
			t.Error("editing did not become available after cancellation")
		}
	})
}

func startTerminal(t *testing.T, w, h int, clients ...model.Client) *terminal {
	t.Helper()
	s, err := project.Create(filepath.Join(t.TempDir(), "story"), "Test", true)
	if err != nil {
		t.Fatal(err)
	}
	var client model.Client = model.Demo{}
	if len(clients) > 0 {
		client = clients[0]
	}
	u := New(application.New(s, client, true))
	screen := tcell.NewSimulationScreen("UTF-8")
	u.app.SetScreen(screen)
	screen.SetSize(w, h)
	term := &terminal{t: t, u: u, screen: screen, done: make(chan error, 1)}
	go func() { defer close(term.done); term.done <- u.Run() }()
	t.Cleanup(func() {
		// Bound Stop itself too: a regression in draw/event locking must fail
		// this fixture instead of hanging its cleanup until the package timeout.
		go u.app.Stop()
		select {
		case err := <-term.done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(2 * time.Second):
			t.Error("terminal did not exit promptly")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := u.core.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})
	term.wait("The Long Return")
	return term
}

type heldDemo struct {
	started, release chan struct{}
	once             sync.Once
}

func (c *heldDemo) Complete(ctx context.Context, m project.Model, messages []model.Message, tools []model.Tool, limit int) (model.Response, error) {
	c.once.Do(func() { close(c.started) })
	select {
	case <-ctx.Done():
		return model.Response{}, ctx.Err()
	case <-c.release:
	}
	return (model.Demo{}).Complete(ctx, m, messages, tools, limit)
}

func TestDetachedTerminalDoesNotOwnWorkerCompletion(t *testing.T) {
	c := &heldDemo{started: make(chan struct{}), release: make(chan struct{})}
	tt := startTerminal(t, 120, 40, c)
	var release sync.Once
	unblock := func() { release.Do(func() { close(c.release) }) }
	defer unblock()
	tt.onUI(func() { tt.u.generate(application.Selection{Scope: "branch", Target: "visit"}, false) })
	select {
	case <-c.started:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	tt.u.app.Stop()
	select {
	case err := <-tt.done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("terminal waited on worker")
	}
	if !tt.u.core.View().Busy {
		t.Fatal("detaching terminal canceled worker")
	}
	unblock()
	deadline := time.Now().Add(3 * time.Second)
	for tt.u.core.View().Busy && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if tt.u.core.Status("visit") != "Available" {
		t.Fatal("worker needed terminal callback to commit result")
	}
}

func TestCancelAndQuitSavesConversationDraft(t *testing.T) {
	started := make(chan struct{})
	tt := startTerminal(t, 120, 40, cancelClient{started: started})
	var id string
	tt.onUI(func() {
		c, err := tt.u.core.NewConversation("visit", "advice", "")
		if err != nil {
			t.Error(err)
			return
		}
		id = c.ID
		tt.u.conversation = &c
		tt.u.prompt.SetText("First message", false)
		tt.u.send()
		tt.u.prompt.SetText("Unsent thought while waiting", false)
	})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	tt.key(tcell.KeyCtrlC)
	tt.click("Cancel and quit")
	select {
	case err := <-tt.done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancel and quit stalled")
	}
	c, err := tt.u.core.LoadConversation(id)
	if err != nil {
		t.Fatal(err)
	}
	if c.Draft != "Unsent thought while waiting" || len(c.Turns) != 2 {
		t.Fatalf("quit lost conversation state: %+v", c)
	}
}

func TestOutlineCompletionOpensInspector(t *testing.T) {
	tt := startTerminal(t, 120, 40)
	tt.onUI(func() { tt.u.startJob("outline", "visit", "", "Develop the cup detail") })
	tt.wait("Import into brief")
	tt.click("Import into brief")
	tt.wait("Imported as authored outline")
	tt.onUI(func() {
		text, err := tt.u.core.Read("visit", "outline")
		if err != nil || !strings.Contains(text, "DEMO outline proposal") {
			t.Errorf("outline import failed: %v", err)
		}
	})
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
	tt.wait("Saved.")
	tt.onUI(func() {
		text, err := tt.u.core.Read("visit", "outline")
		if err != nil {
			t.Error(err)
		}
		if !strings.Contains(text, "Inserted text.") {
			t.Error("text not saved")
		}
		if !tt.u.state.Editing {
			t.Error("save resumed generation")
		}
		if len(tt.u.state.State.Changes) != 0 || len(tt.u.state.State.Decisions) != 1 {
			t.Error("planning edit should be recorded without a prose review")
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
		text, _ := tt.u.core.Read("visit", "prose")
		if !strings.Contains(text, "DEMO") {
			t.Error("generated passage not saved")
		}
		runs, err := tt.u.core.Runs()
		if err != nil || len(runs) != 1 {
			t.Errorf("run not persisted: %v", err)
		}
		if runs[0].Calls > tt.u.state.Config.Limits.Calls {
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
		text, _ := tt.u.core.Read("visit", "outline")
		if strings.Contains(text, "DEMO") {
			t.Error("model proposal changed a source before Apply")
		}
		if tt.u.conversation.Target != "visit" || len(tt.u.conversation.Turns) != 2 {
			t.Error("conversation lost its scope or turn history")
		}
		runs, err := tt.u.core.Runs()
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
		text, _ := tt.u.core.Read("visit", "outline")
		other, _ := tt.u.core.Read("arrival", "outline")
		if !strings.Contains(text, "DEMO proposed outline") || strings.Contains(other, "DEMO") {
			t.Error("proposal applied to the wrong source")
		}
		if !tt.u.state.Editing || len(tt.u.state.State.Changes) != 0 || len(tt.u.state.State.Decisions) != 1 {
			t.Error("applying a proposal did not pause generation and record the change")
		}
	})
}
