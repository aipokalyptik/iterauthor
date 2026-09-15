package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/aipokalyptik/iterauthor/internal/model"
	"github.com/aipokalyptik/iterauthor/internal/project"
)

func source(t *testing.T) (*project.Store, project.Snapshot) {
	t.Helper()
	s, err := project.Create(filepath.Join(t.TempDir(), "story"), "Test", true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err = s.SaveText("mara", "notes", "NEVER-SEND-PRIVATE", ""); err != nil {
		t.Fatal(err)
	}
	snapshot, err := s.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	return s, snapshot
}
func TestHTTPGenerationToolsAndRevisions(t *testing.T) {
	s, snapshot := source(t)
	// Leave references for automatic selection rather than including all of
	// them as mandatory attachments before the selectors run.
	snapshot.Config.Nodes["visit"].Attachments = nil
	var mu sync.Mutex
	drafts := 0
	var requests []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []model.Message `json:"messages"`
			Tools    []model.Tool    `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		mu.Lock()
		defer mu.Unlock()
		b, _ := json.Marshal(body)
		requests = append(requests, string(b))
		system := body.Messages[0].Content
		last := body.Messages[len(body.Messages)-1]
		content := "Relevant reference: Mara [mara] has an injured left wrist."
		message := model.Message{Role: "assistant"}
		finish := "stop"
		if len(body.Tools) > 0 && last.Role != "tool" {
			message.ToolCalls = []model.ToolCall{{ID: "read-1", Type: "function", Function: model.Function{Name: "read_entry", Arguments: `{"id":"mara"}`}}}
			finish = "tool_calls"
		} else {
			if strings.Contains(system, "WRITE PROSE") {
				drafts++
				content = fmt.Sprintf("Draft %d: Mara kept her hands in her lap.", drafts)
			}
			if strings.Contains(system, "REVIEW: consistency") {
				if drafts == 1 {
					content = `{"pass":false,"issues":["Make the cup visible [cup]."],"suggestions":[]}`
				} else {
					content = `{"pass":true,"issues":[],"suggestions":[]}`
				}
			}
			if strings.Contains(system, "REVIEW: style") {
				content = `{"pass":true,"issues":[],"suggestions":[]}`
			}
			message.Content = content
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": message, "finish_reason": finish}}, "usage": map[string]int{"total_tokens": 50}})
	}))
	defer srv.Close()
	snapshot.Config.Models["base"] = project.Model{Name: "Test", URL: srv.URL + "/v1", Model: "fixture", Tools: true}
	e := Engine{Client: model.NewHTTP(), Save: s.SaveRun}
	run := e.Run(context.Background(), snapshot, "visit", "prose", "", "", nil)
	if run.Status != "Available" {
		t.Fatalf("%s: %s", run.Status, run.Error)
	}
	if len(run.Candidates) != 2 {
		t.Fatalf("expected revised candidate, got %d", len(run.Candidates))
	}
	if run.Candidates[0].Style != nil {
		t.Fatal("style checked a candidate already rejected for consistency")
	}
	last := run.Candidates[1]
	if last.Consistency == nil || !last.Consistency.Pass || last.Style == nil || !last.Style.Pass {
		t.Fatal("reviews did not apply to same final candidate")
	}
	if run.Calls > snapshot.Config.Limits.Calls {
		t.Fatal("call budget exceeded")
	}
	for _, request := range requests {
		if strings.Contains(request, "NEVER-SEND-PRIVATE") {
			t.Fatal("private notes sent to HTTP server")
		}
	}
	recorded, err := s.LoadRun(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(recorded.Trace) == 0 || recorded.Context == "" {
		t.Fatal("provenance not persisted")
	}
}

type fake func(context.Context, project.Model, []model.Message, []model.Tool, int) (model.Response, error)

func (f fake) Complete(c context.Context, m project.Model, ms []model.Message, ts []model.Tool, n int) (model.Response, error) {
	return f(c, m, ms, ts, n)
}
func TestToolLoopBudgetAndUnknownTool(t *testing.T) {
	_, snapshot := source(t)
	snapshot.Config.Limits.Calls = 3
	calls := 0
	e := Engine{Client: fake(func(_ context.Context, _ project.Model, ms []model.Message, _ []model.Tool, _ int) (model.Response, error) {
		calls++
		if calls > 1 && !strings.Contains(ms[len(ms)-1].Content, "tool not available") {
			t.Fatal("unknown tool was executed")
		}
		return model.Response{Message: model.Message{Role: "assistant", ToolCalls: []model.ToolCall{{ID: "bad", Function: model.Function{Name: "shell", Arguments: `{"command":"cat private-notes"}`}}}}}, nil
	})}
	r := e.Run(context.Background(), snapshot, "visit", "advice", "base", "Help", nil)
	if r.Calls != 3 || calls != 3 || !strings.Contains(r.Error, "budget exhausted") {
		t.Fatalf("unbounded tool loop: %d %d %s", r.Calls, calls, r.Error)
	}
}
func TestScopedEditProposalCannotWriteElsewhere(t *testing.T) {
	_, snapshot := source(t)
	off := false
	snapshot.Config.Nodes["story"].AutoKnowledge = &off
	snapshot.Config.Nodes["story"].AutoOutline = &off
	step := 0
	e := Engine{Client: fake(func(_ context.Context, _ project.Model, ms []model.Message, _ []model.Tool, _ int) (model.Response, error) {
		step++
		if step == 1 {
			return model.Response{Message: model.Message{Role: "assistant", ToolCalls: []model.ToolCall{{ID: "outside", Function: model.Function{Name: "propose_edit", Arguments: `{"id":"arrival","field":"outline","text":"Overwrite outside scope"}`}}, {ID: "notes", Function: model.Function{Name: "propose_edit", Arguments: `{"id":"visit","field":"notes","text":"Write private notes"}`}}, {ID: "inside", Function: model.Function{Name: "propose_edit", Arguments: `{"id":"visit","field":"outline","text":"Allowed revision"}`}}}}}, nil
		}
		if !strings.Contains(ms[len(ms)-3].Content, "outside") || !strings.Contains(ms[len(ms)-2].Content, "notes") {
			t.Fatal("edit scope checks not reported")
		}
		return model.Response{Message: model.Message{Role: "assistant", Content: "One proposal."}}, nil
	})}
	r := e.Run(context.Background(), snapshot, "visit", "edit", "base", "Revise", nil)
	if r.Status != "Available" || len(r.Edits) != 1 || r.Edits[0].ID != "visit" || r.Edits[0].Field != "outline" {
		t.Fatalf("invalid scoped results: %#v", r)
	}
}
func TestMalformedReviewNeverPasses(t *testing.T) {
	_, snapshot := source(t)
	off := false
	snapshot.Config.Nodes["story"].AutoKnowledge = &off
	snapshot.Config.Nodes["story"].AutoOutline = &off
	e := Engine{Client: fake(func(_ context.Context, _ project.Model, ms []model.Message, _ []model.Tool, _ int) (model.Response, error) {
		text := "Unreviewed passage"
		if strings.Contains(ms[0].Content, "REVIEW:") {
			text = "Seems fine."
		}
		return model.Response{Message: model.Message{Role: "assistant", Content: text}}, nil
	})}
	r := e.Run(context.Background(), snapshot, "visit", "prose", "", "", nil)
	if r.Status != "Needs review" || len(r.Candidates) != 1 || r.Candidates[0].Consistency != nil {
		t.Fatalf("bad review accepted: %#v", r)
	}
}
func TestCancellationRetainsCompletedDraft(t *testing.T) {
	_, snapshot := source(t)
	off := false
	snapshot.Config.Nodes["story"].AutoKnowledge = &off
	snapshot.Config.Nodes["story"].AutoOutline = &off
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e := Engine{Client: fake(func(ctx context.Context, _ project.Model, ms []model.Message, _ []model.Tool, _ int) (model.Response, error) {
		if strings.Contains(ms[0].Content, "REVIEW:") {
			cancel()
			return model.Response{}, ctx.Err()
		}
		return model.Response{Message: model.Message{Role: "assistant", Content: "Completed draft"}}, nil
	})}
	r := e.Run(ctx, snapshot, "visit", "prose", "", "", nil)
	if r.Status != "Canceled" || len(r.Candidates) != 1 || r.Candidates[0].Text != "Completed draft" {
		t.Fatal("cancel lost completed draft")
	}
}
