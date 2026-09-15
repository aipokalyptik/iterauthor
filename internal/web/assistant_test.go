package web_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aipokalyptik/iterauthor/internal/application"
	"github.com/aipokalyptik/iterauthor/internal/model"
	"github.com/aipokalyptik/iterauthor/internal/project"
	"github.com/aipokalyptik/iterauthor/internal/web"
)

func TestSingleOutlineAssistantShowsProgressFailureAndRetry(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages  []model.Message `json:"messages"`
			MaxTokens int             `json:"max_tokens"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		if len(body.Messages) == 0 || !strings.HasPrefix(body.Messages[0].Content, "ADVICE") {
			t.Error("single-outline request wasted a call selecting context already supplied")
		}
		n := calls.Add(1)
		if n == 1 {
			close(started)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":""},"finish_reason":"length"}],"usage":{"total_tokens":3551}}`))
			return
		}
		if body.MaxTokens != 5000 {
			t.Errorf("retry ignored updated output limit: %d", body.MaxTokens)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Seven outline points are ready."},"finish_reason":"stop"}],"usage":{"total_tokens":500}}`))
	}))
	defer provider.Close()
	p, err := project.Create(filepath.Join(t.TempDir(), "story"), "Single premise", false)
	if err != nil {
		t.Fatal(err)
	}
	c := p.Config.Clone()
	c.Models["base"] = project.Model{Name: "Trial model", Model: "fixture", URL: provider.URL + "/v1", Tools: true}
	if err := p.SaveConfig(c, "Setup", c.Root); err != nil {
		t.Fatal(err)
	}
	core := application.New(p, model.NewHTTP(), false)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := core.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	}()
	var logs bytes.Buffer
	core.SetLogger(slog.New(slog.NewTextHandler(&logs, nil)))
	srv := httptest.NewServer(web.New(core))
	defer srv.Close()
	var conversation project.Conversation
	if err := json.Unmarshal(request(t, srv, "/api/command", map[string]any{"action": "conversation", "target": "story", "kind": "advice", "model": "base"}, 200), &conversation); err != nil {
		t.Fatal(err)
	}
	prompt := "PRIVATE-REQUEST: make seven outline points"
	request(t, srv, "/api/command", map[string]any{"action": "send", "id": conversation.ID, "text": prompt}, 200)
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("model call never started")
	}
	var running application.View
	if err := json.Unmarshal(request(t, srv, "/api/state", nil, 200), &running); err != nil {
		t.Fatal(err)
	}
	close(release)
	if !running.Busy || running.Work == nil || running.Work.Conversation != conversation.ID || running.Work.Run == "" || running.Work.Started == "" || running.Work.Calls != 1 || !strings.Contains(running.Progress, "advice") {
		t.Fatalf("missing in-flight feedback: %+v", running.Work)
	}
	waitIdle(t, core)
	var failed project.Conversation
	if err := json.Unmarshal(request(t, srv, "/api/conversations/"+conversation.ID, nil, 200), &failed); err != nil {
		t.Fatal(err)
	}
	if len(failed.Turns) != 2 || failed.Turns[1].Status != "Failed" || !strings.Contains(failed.Turns[1].Text, "before returning any visible text") {
		t.Fatalf("empty token-limited reply hid the failure: %+v", failed.Turns)
	}
	v := core.View()
	if v.Work.Status != "Failed" || v.Work.Finished == "" || !strings.Contains(v.Progress, "Project settings") {
		t.Fatal("completed failure disappeared from status")
	}
	if !strings.Contains(logs.String(), "Operation accepted") || !strings.Contains(logs.String(), "Asking the writing assistant") || !strings.Contains(logs.String(), "status=Failed") || !strings.Contains(logs.String(), "finish_reason=length") || !strings.Contains(logs.String(), "level=ERROR") || !strings.Contains(logs.String(), "Output limit reached before any visible text") || strings.Contains(logs.String(), "PRIVATE-REQUEST") {
		t.Fatalf("console feedback missing or leaked the prompt: %s", logs.String())
	}
	v.Work.Status = "Tampered"
	if core.View().Work.Status != "Failed" {
		t.Fatal("View exposed mutable operation state")
	}
	v.Config.Limits.OutputTokens = 5000
	request(t, srv, "/api/command", map[string]any{"action": "config", "config": v.Config, "expected": v.ConfigVersion, "title": "Increase output limit", "target": "story"}, 200)
	request(t, srv, "/api/command", map[string]any{"action": "send", "id": conversation.ID, "text": prompt}, 200)
	waitIdle(t, core)
	if calls.Load() != 2 {
		t.Fatalf("unexpected automatic retries/context calls: %d", calls.Load())
	}
	result, err := core.LoadConversation(conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Turns) != 4 || result.Turns[3].Status != "Available" || result.Turns[3].Text != "Seven outline points are ready." {
		t.Fatalf("retry lost result or history: %+v", result.Turns)
	}
}
