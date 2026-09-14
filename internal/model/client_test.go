package model

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aipokalyptik/iterauthor/internal/project"
)

func TestHTTPContractAndPartialOutput(t *testing.T) {
	t.Setenv("ITERAUTHOR_TEST_KEY", "test-secret")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Error("missing auth")
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["max_completion_tokens"] != float64(100) {
			t.Error("wrong token field")
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Partial prose"},"finish_reason":"length"}],"usage":{"total_tokens":123}}`))
	}))
	defer srv.Close()
	m := project.Model{URL: srv.URL + "/v1", Model: "test", KeyEnv: "ITERAUTHOR_TEST_KEY", TokenField: "max_completion_tokens"}
	r, err := NewHTTP().Complete(context.Background(), m, []Message{{Role: "user", Content: "Write"}}, nil, 100)
	if err == nil || r.Message.Content != "Partial prose" || r.Tokens != 123 {
		t.Fatalf("partial result lost: %#v %v", r, err)
	}
}
func TestHTTPCancellationAndCredentialRedaction(t *testing.T) {
	t.Setenv("ITERAUTHOR_TEST_KEY", "secret-value")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/error/chat/completions" {
			http.Error(w, "secret-value rejected", 401)
			return
		}
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	}))
	defer srv.Close()
	m := project.Model{URL: srv.URL + "/error", Model: "test", KeyEnv: "ITERAUTHOR_TEST_KEY"}
	_, err := NewHTTP().Complete(context.Background(), m, nil, nil, 100)
	if err == nil || strings.Contains(err.Error(), "secret-value") {
		t.Fatalf("credential leaked: %v", err)
	}
	m.URL = srv.URL
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = NewHTTP().Complete(ctx, m, nil, nil, 100)
	if err == nil || time.Since(start) > 500*time.Millisecond {
		t.Fatal("request did not cancel promptly")
	}
}
