package application_test

import (
	"context"
	"encoding/json"
	"fmt"
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
)

func TestConnectionCatalogRefreshPreservesSelectionsAndCache(t *testing.T) {
	var revision atomic.Int32
	var inference atomic.Int32
	var denied atomic.Bool
	t.Setenv("CATALOG_KEY", "local-test-key")
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/chat/completions") {
			inference.Add(1)
			t.Error("catalog refresh ran inference")
			return
		}
		if r.Header.Get("Authorization") != "Bearer local-test-key" {
			t.Error("connection authentication lost")
		}
		if denied.Load() {
			http.Error(w, "offline", 503)
			return
		}
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		ids := []string{"writer", "second"}
		if revision.Load() > 0 {
			ids = []string{"second", "new-model"}
		}
		rows := []map[string]any{}
		for _, id := range ids {
			rows = append(rows, map[string]any{"id": id, "capabilities": map[string]any{"reasoning": map[string]any{"allowed_options": []string{"off", "on"}}}})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": rows})
	}))
	defer endpoint.Close()
	dir := filepath.Join(t.TempDir(), "story")
	p, err := project.Create(dir, "Test", false)
	check(t, err)
	core := application.New(p, model.NewHTTP(), false)
	t.Cleanup(func() { check(t, core.Shutdown(context.Background())) })
	id, err := core.SaveConnection("", project.Connection{Name: "Office", URL: endpoint.URL, KeyEnv: "CATALOG_KEY"}, core.View().ConfigVersion)
	check(t, err)
	before := core.View().ConfigVersion
	check(t, core.RefreshConnection(context.Background(), id))
	v := core.View()
	if len(v.Catalogs[id].Catalog.Models) != 2 || v.ConfigVersion != before || len(v.State.Changes) != 0 {
		t.Fatal("metadata refresh edited authored configuration")
	}
	ref := project.ModelID(id, "writer")
	if !v.Config.Models[ref].ToolsUnverified {
		t.Fatal("unknown tools advertised as verified")
	}
	c := v.Config
	c.BaseModel = ref
	check(t, core.SaveConfig(c, "Selected base model", c.Root, v.ConfigVersion))
	before = core.View().ConfigVersion
	zero := 0
	m := core.View().Config.Models[ref]
	m.Reasoning = "off"
	m.OutputTokens = &zero
	_, err = core.SaveModel(ref, m, false, before)
	check(t, err)
	before = core.View().ConfigVersion
	revision.Store(1)
	check(t, core.RefreshConnection(context.Background(), id))
	v = core.View()
	if v.ConfigVersion != before || v.Config.BaseModel != ref || v.Config.Models[ref].Reasoning != "off" || v.Config.Models[ref].OutputTokens == nil || *v.Config.Models[ref].OutputTokens != 0 || v.Config.Models[project.ModelID(id, "new-model")].Model == "" {
		t.Fatal("refresh lost selection, settings, or new model")
	}
	// Catalog additions are selectable immediately, even from an already-open form.
	oldForm := c.Clone()
	oldForm.BaseModel = project.ModelID(id, "new-model")
	check(t, core.SaveConfig(oldForm, "Choose new model", c.Root, v.ConfigVersion))
	before = core.View().ConfigVersion
	denied.Store(true)
	if err := core.RefreshConnection(context.Background(), id); err == nil {
		t.Fatal("offline refresh should explain failure")
	}
	v = core.View()
	if v.Catalogs[id].Error == "" || len(v.Catalogs[id].Catalog.Models) != 2 || v.ConfigVersion != before || inference.Load() != 0 {
		t.Fatal("offline refresh lost cache or made inference calls")
	}
	// Views are detached, including mutable metadata slices.
	state := v.Catalogs[id]
	state.Catalog.Models[0].ID = "corrupt"
	meta := v.Config.Models[project.ModelID(id, "second")]
	meta.ReasoningOptions[0] = "corrupt"
	if core.View().Config.Models[project.ModelID(id, "second")].ReasoningOptions[0] == "corrupt" {
		t.Fatal("model settings alias catalog memory")
	}
	if core.View().Catalogs[id].Catalog.Models[0].ID == "corrupt" {
		t.Fatal("catalog view aliases service memory")
	}
	check(t, core.Shutdown(context.Background()))
	p, err = project.Open(dir)
	check(t, err)
	reopened := application.New(p, model.NewHTTP(), false)
	defer reopened.Shutdown(context.Background())
	if reopened.View().Config.Models[project.ModelID(id, "new-model")].Model == "" || reopened.View().Catalogs[id].Updated == "" {
		t.Fatal("catalog selections did not survive restart")
	}
}

func TestStaleCatalogCannotReplaceChangedConnection(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		close(started)
		<-release
		fmt.Fprint(w, `{"data":[{"id":"old"}]}`)
	}))
	defer server.Close()
	p, err := project.Create(filepath.Join(t.TempDir(), "story"), "Test", false)
	check(t, err)
	core := application.New(p, model.NewHTTP(), false)
	defer core.Shutdown(context.Background())
	id, err := core.SaveConnection("", project.Connection{Name: "API", URL: server.URL}, core.View().ConfigVersion)
	check(t, err)
	done := make(chan error, 1)
	go func() { done <- core.RefreshConnection(context.Background(), id) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("refresh did not start")
	}
	_, err = core.SaveConnection(id, project.Connection{Name: "New account", URL: server.URL, KeyEnv: "CHANGED"}, core.View().ConfigVersion)
	check(t, err)
	close(release)
	check(t, <-done)
	if _, ok := core.View().Config.Models[project.ModelID(id, "old")]; ok {
		t.Fatal("stale account catalog was applied")
	}
}

type refreshingClient struct {
	model.Demo
	started, release chan struct{}
	changed          atomic.Bool
}

func (c *refreshingClient) Discover(context.Context, model.Endpoint) (model.Catalog, error) {
	options := []string{"off", "on"}
	if c.changed.Load() {
		options = []string{"low", "high"}
	}
	return model.Catalog{Models: []model.AvailableModel{{ID: "writer", Name: "Writer", Reasoning: options}}}, nil
}
func (c *refreshingClient) Probe(context.Context, project.Model) (model.ProbeResult, error) {
	return model.ProbeResult{}, nil
}
func (c *refreshingClient) Complete(ctx context.Context, m project.Model, messages []model.Message, tools []model.Tool, limit int) (model.Response, error) {
	close(c.started)
	select {
	case <-c.release:
	case <-ctx.Done():
		return model.Response{}, ctx.Err()
	}
	if len(m.ReasoningOptions) != 2 || m.ReasoningOptions[0] != "off" {
		return model.Response{}, fmt.Errorf("active snapshot changed during catalog refresh")
	}
	return c.Demo.Complete(ctx, m, messages, tools, limit)
}
func TestCatalogRefreshDoesNotBlockOrMutateActiveGeneration(t *testing.T) {
	p, err := project.Create(filepath.Join(t.TempDir(), "story"), "Test", false)
	check(t, err)
	client := &refreshingClient{started: make(chan struct{}), release: make(chan struct{})}
	core := application.New(p, client, false)
	defer core.Shutdown(context.Background())
	id, err := core.SaveConnection("", project.Connection{Name: "Local", URL: "http://local"}, core.View().ConfigVersion)
	check(t, err)
	check(t, core.RefreshConnection(context.Background(), id))
	check(t, core.Start(application.Job{Kind: "outline", Target: "story", Model: project.ModelID(id, "writer")}))
	select {
	case <-client.started:
	case <-time.After(time.Second):
		t.Fatal("generation did not start")
	}
	version := core.View().ConfigVersion
	client.changed.Store(true)
	check(t, core.RefreshConnection(context.Background(), id))
	if !core.View().Busy || core.View().ConfigVersion != version {
		t.Fatal("refresh changed authoring state during generation")
	}
	close(client.release)
	idle(t, core)
	run, err := core.LoadRun(core.View().LastRun)
	check(t, err)
	if run.Error != "" || run.Status != "Proposal" {
		t.Fatalf("refresh disturbed frozen job: %+v", run)
	}
}
