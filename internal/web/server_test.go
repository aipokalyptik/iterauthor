package web_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aipokalyptik/iterauthor/internal/application"
	"github.com/aipokalyptik/iterauthor/internal/model"
	"github.com/aipokalyptik/iterauthor/internal/project"
	"github.com/aipokalyptik/iterauthor/internal/web"
)

func setup(t *testing.T, client model.Client) (*application.Service, *httptest.Server) {
	t.Helper()
	store, err := project.Create(filepath.Join(t.TempDir(), "story"), "Web trial", true)
	if err != nil {
		t.Fatal(err)
	}
	core := application.New(store, client, true)
	srv := httptest.NewServer(web.New(core))
	t.Cleanup(func() {
		srv.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := core.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})
	return core, srv
}
func request(t *testing.T, srv *httptest.Server, path string, body any, status int) []byte {
	t.Helper()
	method := http.MethodGet
	var b []byte
	var err error
	if body != nil {
		method = http.MethodPost
		b, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequest(method, srv.URL+path, bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Iterauthor", "1")
		req.Header.Set("Origin", srv.URL)
	}
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != status {
		t.Fatalf("%s %s: HTTP %d: %s", method, path, res.StatusCode, data)
	}
	return data
}
func waitIdle(t *testing.T, core *application.Service) {
	t.Helper()
	updates, unsubscribe := core.Subscribe()
	defer unsubscribe()
	timer := time.NewTimer(4 * time.Second)
	defer timer.Stop()
	for core.View().Busy {
		select {
		case <-updates:
		case <-timer.C:
			t.Fatal("worker did not finish")
		}
	}
}

func TestBrowserAPIEditingGenerationAndReview(t *testing.T) {
	core, srv := setup(t, model.Demo{})
	if html := request(t, srv, "/", nil, 200); !strings.Contains(string(html), "app.js") {
		t.Fatal("embedded application missing")
	}
	request(t, srv, "/api/command", map[string]any{"action": "generate", "scope": "branch", "target": "arrival"}, 200)
	waitIdle(t, core)
	v := core.View()
	original, err := core.Read("arrival", "outline")
	if err != nil {
		t.Fatal(err)
	}
	request(t, srv, "/api/command", map[string]any{"action": "save", "id": "arrival", "field": "outline", "text": "Mara arrives in rain.", "expected": original}, 200)
	request(t, srv, "/api/command", map[string]any{"action": "save", "id": "arrival", "field": "outline", "text": "Lost update", "expected": original}, 400)
	request(t, srv, "/api/command", map[string]any{"action": "config", "config": v.Config, "expected": "stale", "title": "stale", "target": v.Config.Root}, 409)
	request(t, srv, "/api/command", map[string]any{"action": "generate", "scope": "branch", "target": "arrival"}, 409)
	request(t, srv, "/api/command", map[string]any{"action": "decide", "all": true, "choice": "branch"}, 400)
	request(t, srv, "/api/command", map[string]any{"action": "decide", "all": true, "choice": "keep"}, 200)
	request(t, srv, "/api/command", map[string]any{"action": "generate", "scope": "branch", "target": "arrival", "force": true}, 200)
	waitIdle(t, core)
	var runs []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(request(t, srv, "/api/runs", nil, 200), &runs); err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 {
		t.Fatal("missing run")
	}
	var run project.Run
	if err := json.Unmarshal(request(t, srv, "/api/runs/"+runs[0].ID, nil, 200), &run); err != nil {
		t.Fatal(err)
	}
	if len(run.Candidates) != 1 || run.Candidates[0].Consistency == nil || run.Candidates[0].Style == nil || run.Context == "" {
		t.Fatal("review evidence missing")
	}
	request(t, srv, "/api/command", map[string]any{"action": "use-candidate", "id": run.ID, "index": 0}, 200)
	if !strings.Contains(string(request(t, srv, "/download/manuscript", nil, 200)), "synthetic passage") {
		t.Fatal("missing active prose")
	}
	request(t, srv, "/api/command", map[string]any{"action": "invalidate-run", "id": run.ID}, 200)
	request(t, srv, "/api/command", map[string]any{"action": "use-candidate", "id": run.ID, "index": 0}, 400)
	request(t, srv, "/api/source/unknown/outline", nil, 400)
	request(t, srv, "/api/version?id=../../project.json", nil, 400)
}

func TestModelSetupDoesNotBlockFirstDraft(t *testing.T) {
	core, srv := setup(t, model.Demo{})
	for _, id := range []string{"base", "writer"} {
		v := core.View()
		connection := project.Model{Name: id, Model: id, URL: "http://localhost:1234", Tools: id == "base"}
		request(t, srv, "/api/command", map[string]any{"action": "save-model", "id": id, "connection": connection, "base": id == "base", "expected": v.ConfigVersion}, 200)
	}
	v := core.View()
	v.Config.Defaults = map[string]string{"prose": "writer", "style": "writer"}
	request(t, srv, "/api/command", map[string]any{"action": "config", "config": v.Config, "expected": v.ConfigVersion, "title": "Changed feature models", "target": v.Config.Root}, 200)
	var after application.View
	if err := json.Unmarshal(request(t, srv, "/api/state", nil, 200), &after); err != nil {
		t.Fatal(err)
	}
	if len(after.State.Changes) != 0 || len(after.State.Decisions) != 3 || after.Busy {
		t.Fatalf("setup created a review barrier or scheduled work: %+v", after.State)
	}
	runs, err := core.Runs()
	if err != nil || len(runs) != 0 {
		t.Fatalf("setup should not generate prose: %v", err)
	}
	request(t, srv, "/api/command", map[string]any{"action": "generate", "scope": "branch", "target": "arrival"}, 200)
	waitIdle(t, core)
	if core.Status("arrival") != "Available" {
		t.Fatal("first draft did not finish")
	}
	v = core.View()
	connectionID := v.Config.Models["writer"].Connection
	connection := v.Config.Connections[connectionID]
	connection.URL = "http://another-host:1234/v1"
	request(t, srv, "/api/command", map[string]any{"action": "save-connection", "id": connectionID, "api": connection, "expected": v.ConfigVersion}, 200)
	if core.View().ConfigVersion == v.ConfigVersion || len(core.View().State.Changes) != 0 {
		t.Fatal("connection edit should update form version without requiring prose review")
	}
	v = core.View()
	v.Config.Defaults["style"] = "base"
	request(t, srv, "/api/command", map[string]any{"action": "config", "config": v.Config, "expected": v.ConfigVersion, "title": "Changed feature models", "target": v.Config.Root}, 200)
	request(t, srv, "/api/command", map[string]any{"action": "generate", "scope": "branch", "target": "arrival", "force": true}, 409)
}

func TestHTTPRequestOriginProtection(t *testing.T) {
	_, srv := setup(t, model.Demo{})
	for _, kind := range []string{"origin", "fetch-site", "missing-header", "bad-json", "extra-json", "unknown-field"} {
		t.Run(kind, func(t *testing.T) {
			body := `{"action":"cancel"}`
			status := 403
			switch kind {
			case "bad-json":
				body = `{"action":`
				status = 400
			case "extra-json":
				body += ` {}`
				status = 400
			case "unknown-field":
				body = `{"action":"cancel","shell":"no"}`
				status = 400
			}
			req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/command", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Iterauthor", "1")
			switch kind {
			case "origin":
				req.Header.Set("Origin", "https://attacker.test")
			case "fetch-site":
				req.Header.Set("Sec-Fetch-Site", "cross-site")
			case "missing-header":
				req.Header.Del("X-Iterauthor")
			}
			res, err := srv.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			if res.StatusCode != status {
				t.Errorf("HTTP %d", res.StatusCode)
			}
			if res.Header.Get("Access-Control-Allow-Origin") != "" {
				t.Error("unexpected CORS access")
			}
		})
	}
}

func TestNetworkBindingAndRemoteHostRequests(t *testing.T) {
	core, _ := setup(t, model.Demo{})
	for _, address := range []string{"127.0.0.1:0", "0.0.0.0:0", ":0", "[::]:0"} {
		t.Run(address, func(t *testing.T) {
			listener, err := web.Listen(address)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- web.Serve(ctx, listener, web.New(core)) }()
			t.Cleanup(func() {
				cancel()
				select {
				case err := <-done:
					if err != nil {
						t.Error(err)
					}
				case <-time.After(time.Second):
					t.Error("HTTP server did not stop")
				}
			})
			addr := listener.Addr().(*net.TCPAddr)
			target := net.JoinHostPort("127.0.0.1", fmt.Sprint(addr.Port))
			if address == "[::]:0" {
				target = net.JoinHostPort("::1", fmt.Sprint(addr.Port))
			}
			client := &http.Client{Timeout: 2 * time.Second}
			for _, host := range []string{"debian.lan:8080", "192.168.1.10:8080", "[fd00::10]:8080"} {
				for _, path := range []string{"/", "/app.js", "/api/state", "/api/command"} {
					method, body := http.MethodGet, ""
					if path == "/api/command" {
						method, body = http.MethodPost, `{"action":"begin-editing"}`
					}
					req, _ := http.NewRequest(method, "http://"+target+path, strings.NewReader(body))
					req.Host = host
					req.Header.Set("Origin", "http://"+host)
					req.Header.Set("Content-Type", "application/json")
					req.Header.Set("X-Iterauthor", "1")
					res, err := client.Do(req)
					if err != nil {
						t.Fatal(err)
					}
					res.Body.Close()
					if res.StatusCode != http.StatusOK {
						t.Fatalf("%s %s: HTTP %d", host, path, res.StatusCode)
					}
				}
			}
		})
	}
}

type held struct {
	once             sync.Once
	started, release chan struct{}
}

func (c *held) Complete(ctx context.Context, m project.Model, messages []model.Message, tools []model.Tool, max int) (model.Response, error) {
	c.once.Do(func() {
		close(c.started)
		select {
		case <-c.release:
		case <-ctx.Done():
		}
	})
	return (model.Demo{}).Complete(ctx, m, messages, tools, max)
}
func TestDisconnectedBrowserDoesNotOwnWork(t *testing.T) {
	client := &held{started: make(chan struct{}), release: make(chan struct{})}
	defer close(client.release)
	core, srv := setup(t, client)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/events", nil)
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(res.Body).ReadString('\n')
	if err != nil || line != "event: change\n" {
		t.Fatalf("no initial SSE: %q %v", line, err)
	}
	cancel()
	res.Body.Close()
	request(t, srv, "/api/command", map[string]any{"action": "generate", "scope": "branch", "target": "arrival"}, 200)
	select {
	case <-client.started:
	case <-time.After(time.Second):
		t.Fatal("worker not started")
	}
	request(t, srv, "/api/command", map[string]any{"action": "save", "id": "arrival", "field": "notes", "text": "Must not write while running", "expected": ""}, 409)
	request(t, srv, "/api/command", map[string]any{"action": "cancel"}, 200)
	waitIdle(t, core)
	runs, err := core.Runs()
	if err != nil || len(runs) != 1 || runs[0].Status != "Canceled" {
		t.Fatalf("cancel not retained: %+v %v", runs, err)
	}
	request(t, srv, "/api/command", map[string]any{"action": "save", "id": "arrival", "field": "notes", "text": "Editable again", "expected": ""}, 200)
}

func TestModelWizardAPIKeepsStaleConfigurationSafe(t *testing.T) {
	core, srv := setup(t, model.Demo{})
	v := core.View()
	catalog := request(t, srv, "/api/models/discover", map[string]any{"url": "http://localhost:1234"}, 200)
	if !strings.Contains(string(catalog), "synthetic") {
		t.Fatal("demo discovery not labeled")
	}
	connection := project.Model{Name: "Trial model", Model: "demo", URL: "http://localhost:1234", Tools: true}
	probe := request(t, srv, "/api/models/probe", connection, 200)
	if !strings.Contains(string(probe), `"tools":true`) {
		t.Fatal("probe missing")
	}
	request(t, srv, "/api/command", map[string]any{"action": "save-model", "id": "base", "connection": connection, "base": true, "expected": v.ConfigVersion}, 200)
	if core.View().Config.Models["base"].URL != "http://localhost:1234/v1" {
		t.Fatal("URL not normalized")
	}
	request(t, srv, "/api/command", map[string]any{"action": "save-model", "id": "base", "connection": connection, "base": true, "expected": v.ConfigVersion}, 409)
	connection.Tools = false
	v = core.View()
	request(t, srv, "/api/command", map[string]any{"action": "save-model", "id": "base", "connection": connection, "base": true, "expected": v.ConfigVersion}, 400)
}
