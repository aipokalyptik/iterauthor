package model

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aipokalyptik/iterauthor/internal/project"
)

func TestAutomaticBudgetsFitLoadedContextAndGrowWithToolHistory(t *testing.T) {
	m := project.Model{Name: "Local", Context: 8192, ContextSource: "loaded"}
	messages := []Message{{Role: "user", Content: strings.Repeat("story ", 100)}}
	b, err := PlanOutput("advice", m, messages, nil, -1)
	if err != nil || b.OutputTokens < 4096 || b.OutputTokens+b.EstimatedInputTokens+1024 > 8192 {
		t.Fatalf("unusable first-call allowance: %+v %v", b, err)
	}
	messages = append(messages, Message{Role: "tool", Content: strings.Repeat("source ", 700)})
	second, err := PlanOutput("advice", m, messages, nil, -1)
	if err != nil || second.OutputTokens >= b.OutputTokens {
		t.Fatalf("tool history not budgeted: %+v %v", second, err)
	}
	unlimited, err := PlanOutput("advice", m, messages, nil, 0)
	if err != nil || unlimited.OutputTokens != 0 {
		t.Fatal("unlimited acquired a hidden cap")
	}
	_, err = PlanOutput("prose", m, []Message{{Content: strings.Repeat("x", 30000)}}, nil, -1)
	if err == nil {
		t.Fatal("overfull estimated context was sent to server")
	}
	m.Context = 0
	m.Reasoning = "off"
	for stage, want := range map[string]int{"prose": 16384, "advice": 8192, "style": 4096} {
		got, err := PlanOutput(stage, m, nil, nil, -1)
		if err != nil || got.OutputTokens != want {
			t.Fatalf("%s: %+v %v", stage, got, err)
		}
	}
}

func TestHTTPBudgetsBeforeSendingAndRejectsInvalidSuccess(t *testing.T) {
	for _, tc := range []struct{ name, finish, content string }{
		{"whitespace", "stop", "  \n"}, {"filtered", "content_filter", "partial"}, {"unknown", "unexpected", "partial"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				tokens, ok := body["max_tokens"].(float64)
				if !ok || tokens > 7000 || tokens < 4096 {
					t.Errorf("invalid local budget %v", body["max_tokens"])
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": tc.content}, "finish_reason": tc.finish}}})
			}))
			defer srv.Close()
			got, err := NewHTTP().Complete(context.Background(), project.Model{URL: srv.URL, Model: "test", Context: 8192}, nil, nil, -1)
			if err == nil || got.Message.Content != tc.content || got.Budget == nil {
				t.Fatalf("failed response accepted or lost: %+v %v", got, err)
			}
		})
	}
}

func TestRejectMalformedToolCallsBeforeExecution(t *testing.T) {
	for _, calls := range []string{
		`[{"id":"","type":"function","function":{"name":"read_entry","arguments":"{}"}}]`,
		`[{"id":"x","type":"function","function":{"name":"read_entry","arguments":"{}"}},{"id":"x","type":"function","function":{"name":"read_entry","arguments":"{}"}}]`,
		`[{"id":"x","type":"function","function":{"name":"read_entry","arguments":"oops"}}]`,
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"choices":[{"finish_reason":"tool_calls","message":{"content":"","tool_calls":` + calls + `}}]}`))
		}))
		got, err := NewHTTP().Complete(context.Background(), project.Model{URL: srv.URL, Model: "test"}, nil, nil, 0)
		srv.Close()
		if err == nil || len(got.Message.ToolCalls) == 0 {
			t.Fatal("malformed calls accepted or not retained")
		}
	}
}
