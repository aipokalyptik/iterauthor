package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aipokalyptik/iterauthor/internal/project"
)

func TestAPIBase(t *testing.T) {
	for input, expected := range map[string]string{
		"localhost:1234":                                  "http://localhost:1234/v1",
		"http://127.0.0.1:11434/api/tags":                 "http://127.0.0.1:11434/v1",
		"https://example.test/proxy/api/v1/models":        "https://example.test/proxy/v1",
		"https://example.test/proxy/v1/chat/completions/": "https://example.test/proxy/v1",
		"https://example.test/custom":                     "https://example.test/custom",
	} {
		actual, err := APIBase(input)
		if err != nil || actual != expected {
			t.Errorf("%s => %s, %v", input, actual, err)
		}
	}
	for _, input := range []string{"", "ftp://host", "http://user:secret@host", "http://host?api_key=secret", "http://host/#secret"} {
		if _, err := APIBase(input); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
}

func TestDiscoverNativeMetadataAndCompatibleFallback(t *testing.T) {
	t.Setenv("ITERAUTHOR_DISCOVERY_KEY", "discovery-secret")
	for _, kind := range []string{"lmstudio", "ollama", "compatible"} {
		t.Run(kind, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer discovery-secret" {
					t.Error("authentication missing")
				}
				switch r.URL.Path {
				case "/proxy/v1/models":
					fmt.Fprint(w, `{"data":[{"id":"writer"}]}`)
				case "/proxy/api/v1/models":
					if kind != "lmstudio" {
						http.NotFound(w, r)
						return
					}
					fmt.Fprint(w, `{"models":[{"type":"llm","key":"writer","display_name":"Local writer","max_context_length":32768,"loaded_instances":[{"config":{"context_length":8192}}],"capabilities":{"trained_for_tool_use":true,"reasoning":{"allowed_options":["off","low","high"]}},"quantization":{"name":"Q4_K_M"}},{"type":"embedding","key":"embed"}]}`)
				case "/proxy/api/tags":
					if kind != "ollama" {
						http.NotFound(w, r)
						return
					}
					fmt.Fprint(w, `{"models":[{"name":"writer","details":{"quantization_level":"Q8_0"}}]}`)
				default:
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			catalog, err := NewHTTP().Discover(context.Background(), Endpoint{URL: srv.URL + "/proxy/v1", KeyEnv: "ITERAUTHOR_DISCOVERY_KEY"})
			if err != nil {
				t.Fatal(err)
			}
			if catalog.URL != srv.URL+"/proxy/v1" {
				t.Fatal("lost proxy path")
			}
			var writer AvailableModel
			for _, m := range catalog.Models {
				if m.ID == "writer" {
					writer = m
				}
			}
			if writer.ID == "" {
				t.Fatal("missing discovered model")
			}
			if kind == "lmstudio" && (catalog.Provider != "LM Studio" || writer.Tools == nil || !*writer.Tools || writer.Context != 8192 || writer.Loaded == nil || !*writer.Loaded || writer.Quantization != "Q4_K_M" || len(writer.Reasoning) != 3 || len(catalog.Models) != 2) {
				t.Fatalf("metadata lost: %+v", catalog)
			}
			if kind == "ollama" && (catalog.Provider != "Ollama" || writer.Quantization != "Q8_0") {
				t.Fatalf("metadata lost: %+v", catalog)
			}
			if kind == "compatible" && (writer.Tools != nil || writer.Context != 0) {
				t.Fatal("invented capabilities")
			}
		})
	}
}

func TestDiscoverErrorsAndRedirects(t *testing.T) {
	t.Setenv("ITERAUTHOR_DISCOVERY_KEY", "do-not-expose")
	for _, kind := range []string{"empty", "unauthorized", "redirect", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			var leaked atomic.Bool
			destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Store(true) }))
			defer destination.Close()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch kind {
				case "empty":
					if strings.HasSuffix(r.URL.Path, "/v1/models") {
						fmt.Fprint(w, `{"data":[]}`)
					} else {
						http.NotFound(w, r)
					}
				case "unauthorized":
					http.Error(w, "do-not-expose", 401)
				case "redirect":
					http.Redirect(w, r, destination.URL, 302)
				case "oversized":
					fmt.Fprint(w, strings.Repeat("x", 2*1024*1024+1))
				}
			}))
			defer srv.Close()
			_, err := NewHTTP().Discover(context.Background(), Endpoint{URL: srv.URL, KeyEnv: "ITERAUTHOR_DISCOVERY_KEY"})
			if err == nil || strings.Contains(err.Error(), "do-not-expose") || leaked.Load() {
				t.Fatalf("unsafe result: %v", err)
			}
			if kind == "empty" && !strings.Contains(err.Error(), "returned no models") {
				t.Fatalf("misleading empty response: %v", err)
			}
		})
	}
}

func TestProbeNegotiatesTokensAndVerifiesToolExchange(t *testing.T) {
	for _, kind := range []string{"tools", "text-only", "bad-arguments", "bad-continuation"} {
		t.Run(kind, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := calls.Add(1)
				if r.URL.Path != "/v1/chat/completions" {
					t.Errorf("unexpected path %s", r.URL.Path)
				}
				var req struct {
					Model      string    `json:"model"`
					MaxTokens  int       `json:"max_tokens"`
					Completion int       `json:"max_completion_tokens"`
					Messages   []Message `json:"messages"`
					Tools      []Tool    `json:"tools"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
				}
				if req.MaxTokens != 0 {
					http.Error(w, `use max_completion_tokens`, 400)
					return
				}
				if req.Completion != 512 || n > 4 {
					t.Error("unbounded connection test")
				}
				content := Message{Role: "assistant", Content: "Ready."}
				if len(req.Tools) > 0 && req.Messages[len(req.Messages)-1].Role != "tool" && kind != "text-only" {
					args := `{"value":"iterauthor"}`
					if kind == "bad-arguments" {
						args = `{"value":"wrong"}`
					}
					content = Message{Role: "assistant", ToolCalls: []ToolCall{{ID: "test-call", Type: "function", Function: Function{Name: "connection_check", Arguments: args}}}}
				}
				if req.Messages[len(req.Messages)-1].Role == "tool" {
					if req.Messages[len(req.Messages)-1].ToolCallID != "test-call" {
						t.Error("tool result not linked")
					}
					if kind == "bad-continuation" {
						http.Error(w, "unsupported tool result", 400)
						return
					}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": content, "finish_reason": "stop"}}})
			}))
			defer srv.Close()
			result, err := NewHTTP().Probe(context.Background(), project.Model{URL: srv.URL, Model: "writer"})
			if err != nil {
				t.Fatal(err)
			}
			if !result.Text || result.Model.TokenField != "max_completion_tokens" || result.Tools != (kind == "tools") || result.Model.Tools != result.Tools || calls.Load() > 4 {
				t.Fatalf("wrong probe result: %+v", result)
			}
		})
	}
}

func TestProbeCancellationIsNotTextOnlySuccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Tools []Tool `json:"tools"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if len(req.Tools) > 0 {
			cancel()
			<-r.Context().Done()
			return
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"Ready"},"finish_reason":"stop"}]}`)
	}))
	defer srv.Close()
	started := time.Now()
	_, err := NewHTTP().Probe(ctx, project.Model{URL: srv.URL, Model: "writer"})
	if !errors.Is(err, context.Canceled) || time.Since(started) > time.Second {
		t.Fatalf("cancellation not propagated: %v", err)
	}
}
