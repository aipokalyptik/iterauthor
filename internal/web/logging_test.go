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
	"testing"

	"github.com/aipokalyptik/iterauthor/internal/application"
	"github.com/aipokalyptik/iterauthor/internal/model"
	"github.com/aipokalyptik/iterauthor/internal/project"
	"github.com/aipokalyptik/iterauthor/internal/web"
)

func TestConsoleLogsActivitiesAndRequestErrorsWithoutContentOrPolling(t *testing.T) {
	core, srv := setup(t, model.Demo{})
	var logs bytes.Buffer
	core.SetLogger(slog.New(slog.NewTextHandler(&logs, nil)))
	for range 3 {
		request(t, srv, "/api/state", nil, 200)
	}
	if logs.Len() != 0 {
		t.Fatal("routine state polling was logged")
	}
	old, err := core.Read("visit", "outline")
	if err != nil {
		t.Fatal(err)
	}
	request(t, srv, "/api/command", map[string]any{"action": "save", "id": "visit", "field": "outline", "text": "PRIVATE-STORY-TEXT", "expected": old}, 200)
	request(t, srv, "/api/command", map[string]any{"action": "save", "id": "visit", "field": "outline", "text": "MORE-PRIVATE-TEXT", "expected": "stale"}, 400)
	request(t, srv, "/api/command", map[string]any{"action": "SECRET-INVALID-ACTION"}, 400)
	request(t, srv, "/api/version?id=PRIVATE-QUERY", nil, 400)
	output := logs.String()
	for _, want := range []string{"Saving source text", "level=ERROR", "Request failed", "POST /api/command", "GET /api/version", "status=400", "Unknown command"} {
		if !strings.Contains(output, want) {
			t.Errorf("missing %q in %s", want, output)
		}
	}
	for _, private := range []string{"PRIVATE-STORY", "MORE-PRIVATE", "SECRET-INVALID", "PRIVATE-QUERY"} {
		if strings.Contains(output, private) {
			t.Errorf("request content leaked into logs: %s", output)
		}
	}
}

func TestConsoleLogsProviderFailureWithoutEchoedPrompt(t *testing.T) {
	t.Setenv("CONSOLE_TEST_KEY", "private-test-key")
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "PRIVATE-PROVIDER-ECHO private-test-key", http.StatusUnauthorized)
	}))
	defer provider.Close()
	p, err := project.Create(filepath.Join(t.TempDir(), "story"), "Private title", false)
	if err != nil {
		t.Fatal(err)
	}
	c := p.Config.Clone()
	c.Models["base"] = project.Model{Name: "PRIVATE-MODEL-NAME", Model: "fake", URL: provider.URL + "/v1", Tools: true, KeyEnv: "CONSOLE_TEST_KEY"}
	if err := p.SaveConfig(c, "Configure", c.Root); err != nil {
		t.Fatal(err)
	}
	core := application.New(p, model.NewHTTP(), false)
	defer core.Shutdown(context.Background())
	var logs bytes.Buffer
	core.SetLogger(slog.New(slog.NewTextHandler(&logs, nil)))
	srv := httptest.NewServer(web.New(core))
	defer srv.Close()
	var chat project.Conversation
	if err := json.Unmarshal(request(t, srv, "/api/command", map[string]any{"action": "conversation", "target": "story", "kind": "advice", "model": "base"}, 200), &chat); err != nil {
		t.Fatal(err)
	}
	request(t, srv, "/api/command", map[string]any{"action": "send", "id": chat.ID, "text": "PRIVATE-AUTHOR-PROMPT"}, 200)
	waitIdle(t, core)
	output := logs.String()
	for _, want := range []string{"Starting a writing conversation", "Preparing generation context", "Asking the writing assistant", "level=ERROR", "Generation stopped", "HTTP 401", "status=Failed"} {
		if !strings.Contains(output, want) {
			t.Errorf("missing %q in %s", want, output)
		}
	}
	for _, private := range []string{"PRIVATE-", "private-test-key"} {
		if strings.Contains(output, private) {
			t.Errorf("private data leaked: %s", output)
		}
	}
}

func TestConsoleReportsCatalogRefreshErrors(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "PRIVATE-CATALOG-BODY", 503) }))
	defer provider.Close()
	p, err := project.Create(filepath.Join(t.TempDir(), "story"), "Test", false)
	if err != nil {
		t.Fatal(err)
	}
	core := application.New(p, model.NewHTTP(), false)
	defer core.Shutdown(context.Background())
	var logs bytes.Buffer
	core.SetLogger(slog.New(slog.NewTextHandler(&logs, nil)))
	id, err := core.SaveConnection("", project.Connection{Name: "Test", URL: provider.URL}, core.View().ConfigVersion)
	if err != nil {
		t.Fatal(err)
	}
	if err := core.RefreshConnection(context.Background(), id); err == nil {
		t.Fatal("expected refresh failure")
	}
	output := logs.String()
	for _, want := range []string{"Refreshing the model list", "Model list refresh failed", "level=ERROR", "HTTP 503"} {
		if !strings.Contains(output, want) {
			t.Errorf("missing %q in %s", want, output)
		}
	}
	if strings.Contains(output, "PRIVATE-CATALOG-BODY") {
		t.Fatal("catalog error body leaked")
	}
}
