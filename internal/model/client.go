package model

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/aipokalyptik/iterauthor/internal/project"
)

type Function struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}
type ToolCall struct {
	ID       string   `json:"id"`
	Type     string   `json:"type"`
	Function Function `json:"function"`
}
type Message struct {
	ReasoningContent string          `json:"reasoning_content,omitempty"`
	Reasoning        json.RawMessage `json:"reasoning,omitempty"`
	Role             string          `json:"role"`
	Content          string          `json:"content"`
	ToolCalls        []ToolCall      `json:"tool_calls,omitempty"`
	ToolCallID       string          `json:"tool_call_id,omitempty"`
}
type Tool struct {
	Type     string     `json:"type"`
	Function Definition `json:"function"`
}
type Definition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}
type Response struct {
	Budget          *Budget `json:"budget,omitempty"`
	InputTokens     *int    `json:"input_tokens,omitempty"`
	OutputTokens    *int    `json:"output_tokens,omitempty"`
	ReasoningTokens *int    `json:"reasoning_tokens,omitempty"`
	Message         Message
	Tokens          int
	Finish          string
}
type Client interface {
	Complete(context.Context, project.Model, []Message, []Tool, int) (Response, error)
}

type HTTP struct{ Client *http.Client }

func NewHTTP() *HTTP {
	return &HTTP{Client: &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return fmt.Errorf("model endpoint redirected; configure its final URL")
	}}}
}
func (h *HTTP) Complete(ctx context.Context, m project.Model, messages []Message, tools []Tool, maxTokens int) (Response, error) {
	return h.complete(ctx, m, messages, tools, maxTokens, nil)
}

// CompleteStream uses the same single request and validation as Complete. Servers
// returning an ordinary JSON response still work; no retry spends a second call.
func (h *HTTP) CompleteStream(ctx context.Context, m project.Model, messages []Message, tools []Tool, maxTokens int, emit func(Delta)) (Response, error) {
	return h.complete(ctx, m, messages, tools, maxTokens, emit)
}

func (h *HTTP) complete(ctx context.Context, m project.Model, messages []Message, tools []Tool, maxTokens int, emit func(Delta)) (Response, error) {
	var result Response
	if strings.TrimSpace(m.Model) == "" {
		return result, fmt.Errorf("connect a writing model in Models")
	}
	u, err := url.Parse(strings.TrimRight(m.URL, "/"))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return result, fmt.Errorf("model URL must be an http(s) API base URL without credentials or query")
	}
	if !strings.HasSuffix(u.Path, "/chat/completions") {
		u.Path = strings.TrimRight(u.Path, "/") + "/chat/completions"
	}
	payload := map[string]any{"model": m.Model, "messages": messages, "stream": emit != nil}
	if emit != nil {
		payload["stream_options"] = map[string]bool{"include_usage": true}
	}
	field := m.TokenField
	if field == "" {
		field = "max_tokens"
		if m.Provider == "OpenAI" {
			field = "max_completion_tokens"
		}
	}
	if field != "max_tokens" && field != "max_completion_tokens" {
		return result, fmt.Errorf("token field must be max_tokens or max_completion_tokens")
	}
	budget, err := PlanOutput("", m, messages, tools, maxTokens)
	result.Budget = &budget
	if err != nil {
		return result, err
	}
	maxTokens = budget.OutputTokens

	if maxTokens > 0 {
		payload[field] = maxTokens
	}
	if err := ApplyReasoning(payload, m); err != nil {
		return result, err
	}
	if len(tools) > 0 {
		if !m.Tools {
			return result, fmt.Errorf("%s is configured without tool support", m.Name)
		}
		payload["tools"] = tools
		payload["tool_choice"] = "auto"
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return result, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(data))
	if err != nil {
		return result, err
	}
	req.Header.Set("Content-Type", "application/json")
	var key string
	if m.KeyEnv != "" {
		key = os.Getenv(m.KeyEnv)
		if key == "" {
			return result, fmt.Errorf("environment variable %s is not set", m.KeyEnv)
		}
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := h.Client.Do(req)
	if err != nil {
		return result, fmt.Errorf("model request: %w", err)
	}
	defer resp.Body.Close()
	if emit != nil && resp.StatusCode >= 200 && resp.StatusCode < 300 && strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		result, err := readStream(resp.Body, emit)
		result.Budget = &budget
		if err != nil {
			return result, err
		}
		return validateResponse(m, result)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 32*1024*1024+1))
	if err != nil {
		return result, err
	}
	if len(b) > 32*1024*1024 {
		return result, fmt.Errorf("model response exceeds 32 MiB")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := string(b)
		if key != "" {
			message = strings.ReplaceAll(message, key, "[redacted]")
		}
		if len(message) > 1000 {
			message = message[:1000]
		}
		return result, fmt.Errorf("model HTTP %d: %s", resp.StatusCode, message)
	}
	var envelope struct {
		Choices []struct {
			Message struct {
				Role             string          `json:"role"`
				Content          json.RawMessage `json:"content"`
				ToolCalls        []ToolCall      `json:"tool_calls"`
				ReasoningContent string          `json:"reasoning_content"`
				Reasoning        json.RawMessage `json:"reasoning"`
				Refusal          string          `json:"refusal"`
			} `json:"message"`
			Finish string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			Tokens  int  `json:"total_tokens"`
			Input   *int `json:"prompt_tokens"`
			Output  *int `json:"completion_tokens"`
			Details struct {
				Reasoning *int `json:"reasoning_tokens"`
			} `json:"completion_tokens_details"`
		} `json:"usage"`
	}
	if err = json.Unmarshal(b, &envelope); err != nil {
		return result, fmt.Errorf("invalid model response: %w", err)
	}
	if len(envelope.Choices) == 0 {
		return result, fmt.Errorf("model response contained no choices")
	}
	c := envelope.Choices[0]
	result = Response{Budget: &budget, Tokens: envelope.Usage.Tokens, Finish: c.Finish, InputTokens: envelope.Usage.Input, OutputTokens: envelope.Usage.Output, ReasoningTokens: envelope.Usage.Details.Reasoning}
	if c.Message.Refusal != "" {
		return result, fmt.Errorf("model refused: %s", c.Message.Refusal)
	}
	var content string
	if len(c.Message.Content) > 0 && string(c.Message.Content) != "null" {
		if err = json.Unmarshal(c.Message.Content, &content); err != nil {
			return result, fmt.Errorf("expected a text chat response")
		}
	}
	result.Message = Message{Role: "assistant", Content: content, ToolCalls: c.Message.ToolCalls, ReasoningContent: c.Message.ReasoningContent, Reasoning: c.Message.Reasoning}
	if emit != nil {
		emit(Delta{Text: content})
	}
	return validateResponse(m, result)
}

func validateResponse(m project.Model, result Response) (Response, error) {
	content := result.Message.Content
	if result.Finish == "length" {
		if strings.TrimSpace(content) == "" {
			return result, fmt.Errorf("model reached its output-token limit before returning any visible text; adjust Output tokens or reasoning in Model settings or Project settings (or the server limit when unlimited), then retry")
		}
		return result, fmt.Errorf("model output reached its token limit; partial output retained; increase output_tokens or narrow the task")
	}
	if result.Finish == "content_filter" {
		return result, fmt.Errorf("model output was blocked by the provider's content filter; partial output retained")
	}
	if result.Finish != "" && result.Finish != "stop" && result.Finish != "tool_calls" {
		return result, fmt.Errorf("model ended with unsupported finish reason %q; inspect the retained response", result.Finish)
	}
	if (m.Reasoning == "off" || m.Reasoning == "none") && ((result.ReasoningTokens != nil && *result.ReasoningTokens > 0) || strings.TrimSpace(result.Message.ReasoningContent) != "") {
		return result, fmt.Errorf("the model produced reasoning although reasoning is Off; this server/model did not honor the selected reasoning control; check Model settings and the server")
	}
	seen := map[string]bool{}
	for _, call := range result.Message.ToolCalls {
		if call.ID == "" || seen[call.ID] || call.Type != "function" || call.Function.Name == "" || !json.Valid([]byte(call.Function.Arguments)) {
			return result, fmt.Errorf("model returned an invalid or duplicate tool call; inspect the retained response")
		}
		seen[call.ID] = true
	}
	if strings.TrimSpace(content) == "" && len(result.Message.ToolCalls) == 0 {
		return result, fmt.Errorf("model returned empty text and no tool calls")
	}
	return result, nil
}

// Demo exercises the same engine, tools, budgets and persistence without a server.
// Every synthetic passage is explicitly marked; this is never an automatic fallback.
type Demo struct{}

func (Demo) Complete(ctx context.Context, m project.Model, messages []Message, tools []Tool, maxTokens int) (Response, error) {
	select {
	case <-ctx.Done():
		return Response{}, ctx.Err()
	case <-time.After(50 * time.Millisecond):
	}
	system := messages[0].Content
	last := messages[len(messages)-1]
	if len(tools) > 0 && last.Role != "tool" {
		args := `{"query":"Mara","kind":"all"}`
		name := "search_entries"
		if strings.Contains(system, "TOOL TEST") {
			name = "read_entry"
			args = `{"id":"` + strings.TrimSpace(last.Content) + `"}`
		}
		return Response{Message: Message{Role: "assistant", ToolCalls: []ToolCall{{ID: "demo-call", Type: "function", Function: Function{Name: name, Arguments: args}}}}, Tokens: 25, Finish: "tool_calls"}, nil
	}
	text := "DEMO: Read the source entries above. The injured wrist and the chipped cup may matter here."
	switch {
	case strings.Contains(system, "REVIEW:"):
		text = `{"pass":true,"issues":[],"suggestions":["Demo review: use a real model to evaluate the writing."]}`
	case strings.Contains(system, "WRITE PROSE"):
		text = "[DEMO — synthetic passage, no model called]\n\nMara left her coat on. Across the table, Elias turned the chipped edge of the cup away from her.\n\n\"Nothing came,\" he said.\n\nShe kept her hands in her lap. The empty envelope pressed against her ribs whenever she breathed.\n\n\"You're tired.\"\n\nShe let him believe it."
	case strings.Contains(system, "EXPAND OUTLINE"):
		text = "[DEMO outline proposal]\n\n- Elias turns the chipped cup away from Mara.\n- She nearly mentions the envelope, then asks a less revealing question.\n- His answer leaves her with suspicion rather than proof."
	case strings.Contains(system, "EDIT SOURCE"):
		text = "[DEMO proposed outline]\n\n- Establish what each character wants to conceal.\n- Let a small physical detail interrupt their exchange.\n- End with the question still unanswered."
	case strings.Contains(system, "TOOL TEST"):
		text = "Tool read completed."
	}
	return Response{Message: Message{Role: "assistant", Content: text}, Tokens: 100, Finish: "stop"}, nil
}
