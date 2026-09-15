package web_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/aipokalyptik/iterauthor/internal/application"
	"github.com/aipokalyptik/iterauthor/internal/model"
	"github.com/aipokalyptik/iterauthor/internal/project"
	"github.com/aipokalyptik/iterauthor/internal/web"
)

type countedModel struct{ calls atomic.Int32 }

func (m *countedModel) Complete(ctx context.Context, p project.Model, messages []model.Message, tools []model.Tool, limit int) (model.Response, error) {
	m.calls.Add(1)
	return (model.Demo{}).Complete(ctx, p, messages, tools, limit)
}

func TestEditingAndLegacyFinishCommandNeverStartInference(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "story")
	p, err := project.Create(dir, "Manual generation", true)
	if err != nil {
		t.Fatal(err)
	}
	c := p.Config.Clone()
	c.AutoGenerate = true
	if err := p.SaveConfig(c, "Legacy preference", c.Root); err != nil {
		t.Fatal(err)
	}
	p.Close()
	p, err = project.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	client := &countedModel{}
	core := application.New(p, client, true)
	defer core.Shutdown(context.Background())
	srv := httptest.NewServer(web.New(core))
	defer srv.Close()
	assertQuiet := func() {
		t.Helper()
		v := core.View()
		runs, err := core.Runs()
		if err != nil {
			t.Fatal(err)
		}
		if v.Busy || len(v.Queue) != 0 || len(runs) != 0 || client.calls.Load() != 0 {
			t.Fatal("an editing operation started model work")
		}
	}
	assertQuiet()
	old, err := core.Read("visit", "outline")
	if err != nil {
		t.Fatal(err)
	}
	request(t, srv, "/api/command", map[string]any{"action": "save", "id": "visit", "field": "outline", "text": "They discuss the missing letter.", "expected": old}, 200)
	assertQuiet()
	v := core.View()
	v.Config.Limits.Calls = 12
	request(t, srv, "/api/command", map[string]any{"action": "config", "config": v.Config, "expected": v.ConfigVersion, "title": "Change limits", "target": v.Config.Root}, 200)
	request(t, srv, "/api/command", map[string]any{"action": "reload"}, 200)
	request(t, srv, "/api/command", map[string]any{"action": "invalidate", "scope": "branch", "target": "visit"}, 200)
	request(t, srv, "/api/command", map[string]any{"action": "finish-editing"}, 200)
	assertQuiet()
	var chat project.Conversation
	if err := json.Unmarshal(request(t, srv, "/api/command", map[string]any{"action": "conversation", "kind": "advice", "target": "visit", "model": "base"}, 200), &chat); err != nil {
		t.Fatal(err)
	}
	request(t, srv, "/api/command", map[string]any{"action": "draft", "id": chat.ID, "text": "An unsent message"}, 200)
	request(t, srv, "/api/command", map[string]any{"action": "conversation-model", "id": chat.ID, "model": "base"}, 200)
	request(t, srv, "/api/command", map[string]any{"action": "finish-editing"}, 200)
	assertQuiet()
	// Only this explicit command starts a pipeline, within the requested branch.
	request(t, srv, "/api/command", map[string]any{"action": "generate", "scope": "branch", "target": "visit"}, 200)
	waitIdle(t, core)
	if client.calls.Load() == 0 || core.Status("visit") != "Available" || core.Status("arrival") != "Missing" {
		t.Fatal("explicit request did not generate only the requested branch")
	}
	calls := client.calls.Load()
	old, err = core.Read("visit", "outline")
	if err != nil {
		t.Fatal(err)
	}
	request(t, srv, "/api/command", map[string]any{"action": "save", "id": "visit", "field": "outline", "text": "They discuss the letter over tea.", "expected": old}, 200)
	if len(core.View().State.Changes) == 0 {
		t.Fatal("expected a prose revisit decision")
	}
	request(t, srv, "/api/command", map[string]any{"action": "finish-editing"}, 200)
	request(t, srv, "/api/command", map[string]any{"action": "decide", "all": true, "choice": "story"}, 200)
	request(t, srv, "/api/command", map[string]any{"action": "finish-editing"}, 200)
	if core.View().Busy || client.calls.Load() != calls {
		t.Fatal("revisit decision or finishing an edit started regeneration")
	}
}
