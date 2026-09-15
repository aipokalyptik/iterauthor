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

func TestReasoningAndUnlimitedRequestContract(t *testing.T) {
	cases := []struct {
		name, provider, field, reasoning, expected string
		tokens                                     int
	}{
		{"unlimited default", "", "", "", "{}", 0},
		{"off", "LM Studio", "", "off", `{"reasoning_effort":"none"}`, 0},
		{"on", "LM Studio", "", "on", `{"reasoning_effort":"medium"}`, 0},
		{"ollama", "Ollama", "", "low", `{"reasoning_effort":"low","max_tokens":8192}`, 8192},
		{"openai", "OpenAI", "", "high", `{"reasoning_effort":"high","max_completion_tokens":16384}`, 16384},
		{"llama off", "llama.cpp", "", "off", `{"chat_template_kwargs":{"enable_thinking":false}}`, 0},
		{"template effort", "", "chat_template_kwargs", "high", `{"chat_template_kwargs":{"enable_thinking":true,"reasoning_effort":"high"}}`, 0},
		{"explicit default", "", "", "default", `{}`, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var got map[string]any
				_ = json.NewDecoder(r.Body).Decode(&got)
				delete(got, "model")
				delete(got, "messages")
				delete(got, "stream")
				encoded, _ := json.Marshal(got)
				var want map[string]any
				_ = json.Unmarshal([]byte(tc.expected), &want)
				expected, _ := json.Marshal(want)
				if string(encoded) != string(expected) {
					t.Errorf("wire options %s, want %s", encoded, expected)
				}
				_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Ready","reasoning_content":"retained tool-turn state"},"finish_reason":"stop"}],"usage":{"prompt_tokens":50,"completion_tokens":25,"total_tokens":75,"completion_tokens_details":{"reasoning_tokens":20}}}`))
			}))
			defer srv.Close()
			response, err := NewHTTP().Complete(context.Background(), project.Model{URL: srv.URL, Model: "test", Provider: tc.provider, Reasoning: tc.reasoning, ReasoningField: tc.field}, nil, nil, tc.tokens)
			if tc.reasoning == "off" {
				if err == nil || !strings.Contains(err.Error(), "did not honor") {
					t.Fatalf("ignored reasoning Off not diagnosed: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if response.InputTokens == nil || *response.InputTokens != 50 || response.OutputTokens == nil || *response.OutputTokens != 25 || response.ReasoningTokens == nil || *response.ReasoningTokens != 20 || response.Message.ReasoningContent == "" {
				t.Fatalf("lost usage or tool-turn state: %+v", response)
			}
		})
	}
	if NewHTTP().Client.Timeout != 0 {
		t.Fatal("hidden HTTP timeout overrides operation limit")
	}
}

func TestUnsupportedReasoningIsNotSilentlySubstituted(t *testing.T) {
	m := project.Model{Name: "toggle", Reasoning: "high", ReasoningOptions: []string{"off", "on"}}
	if err := ApplyReasoning(map[string]any{}, m); err == nil {
		t.Fatal("unsupported setting silently accepted")
	}
	m.Reasoning = "off"
	if err := ApplyReasoning(map[string]any{}, m); err != nil {
		t.Fatal(err)
	}
}
