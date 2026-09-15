package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aipokalyptik/iterauthor/internal/project"
)

func TestStreamAssemblesTextReasoningToolsAndUsage(t *testing.T) {
	stream := ": keepalive\r\n\r\n" +
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"private thinking\"}}]}\r\n\r\n" +
		`data: {"choices":[{"index":0,"delta":{"content":"Looking ","tool_calls":[{"index":1,"id":"b","type":"function","function":{"name":"read_entry","arguments":"{\"id\":"}},{"index":0,"id":"a","type":"function","function":{"name":"search_entries","arguments":"{\"query\":"}}]}}]}` + "\n\n" +
		`data: {"choices":[{"index":0,"delta":{"content":"up references.","tool_calls":[{"index":0,"function":{"arguments":"\"fish\"}"}},{"index":1,"function":{"arguments":"\"story\"}"}}]},"finish_reason":"tool_calls"}]}` + "\n\n" +
		`data: {"choices":[],"usage":{"total_tokens":123,"prompt_tokens":100,"completion_tokens":23,"completion_tokens_details":{"reasoning_tokens":8}}}` + "\n\ndata: [DONE]\n\n"
	var updates []Delta
	result, err := readStream(strings.NewReader(stream), func(d Delta) { updates = append(updates, d) })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := validateResponse(project.Model{}, result); err != nil {
		t.Fatal(err)
	}
	if result.Message.Content != "Looking up references." || result.Message.ReasoningContent != "private thinking" || len(result.Message.ToolCalls) != 2 || result.Message.ToolCalls[0].ID != "a" || result.Message.ToolCalls[1].Function.Arguments != `{"id":"story"}` || result.Tokens != 123 || *result.ReasoningTokens != 8 {
		t.Fatalf("bad assembly: %+v", result)
	}
	encoded, _ := json.Marshal(updates)
	if !updates[0].Thinking || !updates[1].Tool || strings.Contains(string(encoded), "private thinking") || strings.Contains(string(encoded), "fish") {
		t.Fatalf("private data in public updates: %s", encoded)
	}
	if _, err := validateResponse(project.Model{Reasoning: "off"}, result); err == nil {
		t.Fatal("stream ignored reasoning Off validation")
	}
}

func TestStreamErrorsRetainPartialText(t *testing.T) {
	for _, tc := range []struct{ name, tail, want string }{
		{"disconnected", "", "before completing"},
		{"done without finish", "data: [DONE]\n\n", "before completing"},
		{"bad JSON", "data: broken\n\n", "invalid model stream"},
		{"provider error", "data: {\"error\":{\"message\":\"private server data\"}}\n\n", "streaming error"},
		{"huge event", "data: " + strings.Repeat("x", 1024*1024), "interrupted"},
		{"length", "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"length\"}]}\n\n", "token limit"},
		{"bad tool", "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":9}]}}]}\n\n", "tool index"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := readStream(strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"Partial reply\"}}]}\n\n"+tc.tail), func(Delta) {})
			if err == nil {
				result, err = validateResponse(project.Model{}, result)
			}
			if result.Message.Content != "Partial reply" || err == nil || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "private server data") {
				t.Fatalf("%+v: %v", result, err)
			}
		})
	}
}

func TestHTTPStreamingDeliversBeforeCompletionAndCancels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["stream"] != true || body["max_tokens"] != float64(100) {
			t.Errorf("stream request: %v", body)
		}
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Live text\"}}]}\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
			t.Error("client failed to cancel stream")
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var visible string
	result, err := NewHTTP().CompleteStream(ctx, project.Model{URL: server.URL, Model: "fixture"}, nil, nil, 100, func(d Delta) {
		visible += d.Text
		cancel() // The callback must arrive while the provider is still waiting.
	})
	if !errors.Is(err, context.Canceled) || visible != "Live text" || result.Message.Content != visible {
		t.Fatalf("no live text/cancellation: %+v %v", result, err)
	}
}

func TestStreamingAcceptsJSONResponseWithoutRetry(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprint(w, `{"choices":[{"message":{"content":"Complete reply"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	var visible string
	result, err := NewHTTP().CompleteStream(context.Background(), project.Model{URL: server.URL, Model: "fixture"}, nil, nil, 100, func(d Delta) { visible += d.Text })
	if err != nil || calls != 1 || visible != "Complete reply" || result.Message.Content != visible {
		t.Fatalf("JSON fallback: %+v %v calls=%d", result, err, calls)
	}
}
