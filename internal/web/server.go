// Package web adapts the application service to an embedded browser interface.
package web

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aipokalyptik/iterauthor/internal/application"
	"github.com/aipokalyptik/iterauthor/internal/project"
)

//go:embed assets/*
var assets embed.FS

type Server struct{ core *application.Service }

func New(core *application.Service) http.Handler {
	s := &Server{core: core}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", s.state)
	mux.HandleFunc("GET /api/events", s.events)
	mux.HandleFunc("GET /api/source/{id}/{field}", func(w http.ResponseWriter, r *http.Request) {
		text, err := core.Read(r.PathValue("id"), r.PathValue("field"))
		respond(w, map[string]string{"text": text}, err)
	})
	mux.HandleFunc("GET /api/context/{id}", func(w http.ResponseWriter, r *http.Request) {
		snapshot, err := core.Snapshot()
		if err != nil {
			respond(w, nil, err)
			return
		}
		respond(w, map[string]string{"style": snapshot.Style(r.PathValue("id"))}, nil)
	})
	mux.HandleFunc("GET /api/runs", s.runs)
	mux.HandleFunc("GET /api/runs/{id}", func(w http.ResponseWriter, r *http.Request) {
		run, err := core.LoadRun(r.PathValue("id"))
		respond(w, run, err)
	})
	mux.HandleFunc("GET /api/conversations", func(w http.ResponseWriter, r *http.Request) { rows, err := core.Conversations(); respond(w, rows, err) })
	mux.HandleFunc("GET /api/conversations/{id}", func(w http.ResponseWriter, r *http.Request) {
		c, err := core.LoadConversation(r.PathValue("id"))
		respond(w, c, err)
	})
	mux.HandleFunc("GET /api/history/{id}", func(w http.ResponseWriter, r *http.Request) {
		rows, err := core.SourceHistory(r.PathValue("id"))
		respond(w, rows, err)
	})
	mux.HandleFunc("GET /api/version", func(w http.ResponseWriter, r *http.Request) {
		text, err := core.ReadSourceVersion(r.URL.Query().Get("id"))
		respond(w, map[string]string{"text": text}, err)
	})
	mux.HandleFunc("GET /api/manuscript", func(w http.ResponseWriter, r *http.Request) {
		respond(w, map[string]string{"text": core.Manuscript()}, nil)
	})
	mux.HandleFunc("GET /download/manuscript", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="manuscript.md"`)
		_, _ = io.WriteString(w, core.Manuscript())
	})
	mux.HandleFunc("POST /api/command", s.command)
	mux.HandleFunc("POST /api/models/discover", func(w http.ResponseWriter, r *http.Request) {
		var input application.Endpoint
		if !decode(w, r, &input) {
			return
		}
		catalog, err := core.DiscoverModels(r.Context(), input)
		respond(w, catalog, err)
	})
	mux.HandleFunc("POST /api/models/probe", func(w http.ResponseWriter, r *http.Request) {
		var input project.Model
		if !decode(w, r, &input) {
			return
		}
		result, err := core.ProbeModel(r.Context(), input)
		respond(w, result, err)
	})
	files, _ := fs.Sub(assets, "assets")
	mux.Handle("GET /", http.FileServer(http.FS(files)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; object-src 'none'; frame-ancestors 'none'; base-uri 'none'")
		if !localHost(r.Host) {
			http.Error(w, "Use the localhost address printed by Iterauthor.", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || u.Host != r.Host || u.Scheme != "http" {
				http.Error(w, "Cross-origin requests are not allowed.", http.StatusForbidden)
				return
			}
		}
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			http.Error(w, "Cross-site requests are not allowed.", http.StatusForbidden)
			return
		}
		if r.Method == http.MethodPost && (r.Header.Get("X-Iterauthor") != "1" || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json")) {
			http.Error(w, "JSON application request required.", http.StatusForbidden)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func localHost(host string) bool {
	name, _, err := net.SplitHostPort(host)
	if err != nil {
		name = host
	}
	if name == "localhost" {
		return true
	}
	ip := net.ParseIP(strings.Trim(name, "[]"))
	return ip != nil && ip.IsLoopback()
}

// Listen deliberately keeps this single-author prototype on loopback. SSH
// forwarding provides remote browser access without exposing project commands.
func Listen(address string) (net.Listener, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("listen address must be host:port: %w", err)
	}
	if !localHost(host) {
		return nil, fmt.Errorf("use a loopback listen address such as 127.0.0.1:8080; connect remotely with SSH forwarding")
	}
	return net.Listen("tcp", address)
}

func Serve(ctx context.Context, listener net.Listener, handler http.Handler) error {
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, BaseContext: func(net.Listener) context.Context { return ctx }}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = server.Close()
		case <-done:
		}
	}()
	err := server.Serve(listener)
	close(done)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func decode(w http.ResponseWriter, r *http.Request, value any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 3*1024*1024)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		respond(w, nil, fmt.Errorf("invalid request: %w", err))
		return false
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		respond(w, nil, fmt.Errorf("expected one JSON request"))
		return false
	}
	return true
}
func respond(w http.ResponseWriter, value any, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, application.ErrBusy) || errors.Is(err, application.ErrStale) || errors.Is(err, application.ErrChanges) {
			status = http.StatusConflict
		}
		if errors.Is(err, application.ErrClosed) {
			status = http.StatusServiceUnavailable
		}
		w.WriteHeader(status)
		value = map[string]string{"error": err.Error()}
	}
	if value == nil {
		value = map[string]bool{"ok": true}
	}
	_ = json.NewEncoder(w).Encode(value)
}

func (s *Server) state(w http.ResponseWriter, r *http.Request) {
	v := s.core.View()
	statuses := map[string]string{}
	for _, id := range v.Config.Leaves(v.Config.Root) {
		statuses[id] = s.core.Status(id)
	}
	respond(w, map[string]any{"revision": v.Revision, "configVersion": v.ConfigVersion, "directory": v.Dir, "config": v.Config, "state": v.State, "editing": v.Editing, "busy": v.Busy, "canceling": v.Canceling, "demo": v.Demo, "queue": v.Queue, "progress": v.Progress, "lastRun": v.LastRun, "statuses": statuses}, nil)
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	events, unsubscribe := s.core.Subscribe()
	defer unsubscribe()
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	controller := http.NewResponseController(w)
	for {
		select {
		case <-r.Context().Done():
			return
		case _, ok := <-events:
			if !ok {
				return
			}
			_ = controller.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if _, err := fmt.Fprintf(w, "event: change\ndata: %d\n\n", s.core.Revision()); err != nil {
				return
			}
		case <-tick.C:
			_ = controller.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if _, err := io.WriteString(w, ": connected\n\n"); err != nil {
				return
			}
		}
		if controller.Flush() != nil {
			return
		}
	}
}

func (s *Server) runs(w http.ResponseWriter, r *http.Request) {
	runs, err := s.core.Runs()
	if err != nil {
		respond(w, nil, err)
		return
	}
	rows := make([]map[string]any, 0, len(runs))
	for _, run := range runs {
		rows = append(rows, map[string]any{"id": run.ID, "target": run.Target, "kind": run.Kind, "status": run.Status, "started": run.Started, "finished": run.Finished, "calls": run.Calls, "error": run.Error, "candidates": len(run.Candidates), "edits": len(run.Edits)})
	}
	respond(w, rows, nil)
}

type command struct {
	Action     string         `json:"action"`
	ID         string         `json:"id"`
	Target     string         `json:"target"`
	Field      string         `json:"field"`
	Text       string         `json:"text"`
	Expected   string         `json:"expected"`
	Title      string         `json:"title"`
	Kind       string         `json:"kind"`
	Handling   string         `json:"handling"`
	Choice     string         `json:"choice"`
	All        bool           `json:"all"`
	IDs        []string       `json:"ids"`
	Scope      string         `json:"scope"`
	Force      bool           `json:"force"`
	Index      int            `json:"index"`
	Model      string         `json:"model"`
	Base       bool           `json:"base"`
	Connection project.Model  `json:"connection"`
	Config     project.Config `json:"config"`
}

func (s *Server) command(w http.ResponseWriter, r *http.Request) {
	var c command
	if !decode(w, r, &c) {
		return
	}
	var value any
	var err error
	core := s.core
	switch c.Action {
	case "save":
		err = core.SaveText(c.ID, c.Field, c.Text, c.Expected)
	case "config":
		err = core.SaveConfig(c.Config, c.Title, c.Target, c.Expected)
	case "child":
		value, err = core.AddChild(c.Target, c.Title, c.Text, c.Handling)
	case "entry":
		value, err = core.AddEntry(c.Title, c.Kind, c.Text)
	case "generate":
		err = core.Generate(application.Selection{Scope: c.Scope, Target: c.Target, IDs: c.IDs}, c.Force)
	case "outline":
		err = core.Start(application.Job{Kind: "outline", Target: c.Target, Prompt: c.Text, Model: c.Model})
	case "cancel":
		core.Cancel()
	case "begin-editing":
		err = core.BeginEditing()
	case "finish-editing":
		err = core.FinishEditing()
	case "reload":
		err = core.Reload()
	case "decide":
		if c.All {
			err = core.DecideAll(c.Choice)
		} else {
			err = core.Decide(c.ID, c.Choice, c.IDs)
		}
	case "invalidate":
		err = core.Invalidate(application.Selection{Scope: c.Scope, Target: c.Target, IDs: c.IDs})
	case "invalidate-run":
		err = core.InvalidateRun(c.ID)
	case "use-candidate":
		err = core.UseCandidate(c.ID, c.Index)
	case "import-outline":
		err = core.ImportOutline(c.ID)
	case "apply-edits":
		err = core.ApplyEdits(c.ID)
	case "conversation":
		value, err = core.NewConversation(c.Target, c.Kind, c.Model)
	case "send":
		err = core.Send(c.ID, c.Text)
	case "draft":
		err = core.SaveDraft(c.ID, c.Text)
	case "conversation-model":
		err = core.SetConversationModel(c.ID, c.Model)
	case "save-model":
		value, err = core.SaveModel(c.ID, c.Connection, c.Base, c.Expected)
	case "export":
		value, err = core.Export()
	default:
		err = fmt.Errorf("unknown command %q", c.Action)
	}
	respond(w, value, err)
}
