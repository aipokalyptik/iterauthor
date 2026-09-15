package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aipokalyptik/iterauthor/internal/model"
	"github.com/aipokalyptik/iterauthor/internal/project"
)

func TestStreamingToolsAndContextStaySeparateFromReply(t *testing.T) {
	_, snapshot := source(t)
	snapshot.Config.Nodes["visit"].Attachments = nil
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []model.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		last := body.Messages[len(body.Messages)-1]
		if last.Role != "tool" {
			if strings.HasPrefix(body.Messages[0].Content, "ADVICE") {
				fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Checking the source.\"}}]}\n\n")
			}
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"read-1\",\"type\":\"function\",\"function\":{\"name\":\"read_entry\",\"arguments\":\"{\\\"id\\\":\"}}]}}]}\n\n")
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"\\\"mara\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n")
			return
		}
		if last.ToolCallID != "read-1" || !strings.Contains(last.Content, "mara") || strings.Contains(last.Content, "NEVER-SEND-PRIVATE") {
			t.Errorf("bad streamed tool continuation: %+v", last)
		}
		text := "Internal context summary"
		if strings.HasPrefix(body.Messages[0].Content, "ADVICE") {
			text = "Visible answer for the author."
		}
		chunk, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]string{"content": text}, "finish_reason": "stop"}}})
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", chunk)
	}))
	defer server.Close()
	snapshot.Config.Models["base"] = project.Model{Name: "Test", Model: "fixture", URL: server.URL, Tools: true}
	var live []Live
	run := (Engine{Client: model.NewHTTP(), Live: func(event Live) { live = append(live, event) }}).Run(context.Background(), snapshot, "visit", "advice", "base", "Help diagnose the scene", nil)
	if run.Status != "Available" || run.Text != "Checking the source.\n\nVisible answer for the author." {
		t.Fatalf("streamed round trip failed: %+v", run)
	}
	stages := map[string]bool{}
	var visible strings.Builder
	for _, event := range live {
		stages[event.Stage] = true
		if event.Stage != "advice" && event.Text != "" {
			t.Fatalf("context text exposed in live chat: %+v", event)
		}
		if event.Reset {
			visible.Reset()
		}
		visible.WriteString(event.Text)
	}
	if !stages["knowledge"] || !stages["outline-context"] || !stages["advice"] || visible.String() != run.Text {
		t.Fatalf("context leaked or missing stages: %v %q", stages, visible.String())
	}
}
