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
	"sort"
	"strings"
	"time"

	"github.com/aipokalyptik/iterauthor/internal/project"
)

type Endpoint struct {
	URL    string `json:"url"`
	KeyEnv string `json:"key_env,omitempty"`
}
type AvailableModel struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Kind         string   `json:"kind,omitempty"`
	Tools        *bool    `json:"tools,omitempty"`
	Context      int      `json:"context,omitempty"`
	Loaded       *bool    `json:"loaded,omitempty"`
	Quantization string   `json:"quantization,omitempty"`
	Reasoning    []string `json:"reasoning,omitempty"`
}
type Catalog struct {
	URL      string           `json:"url"`
	Provider string           `json:"provider"`
	Models   []AvailableModel `json:"models"`
	Notice   string           `json:"notice"`
}
type ProbeResult struct {
	Model  project.Model `json:"model"`
	Text   bool          `json:"text"`
	Tools  bool          `json:"tools"`
	Detail string        `json:"detail"`
}

// APIBase accepts a server address, a compatible API base, or a models/chat URL.
// Native management URLs map to the corresponding compatible inference base.
func APIBase(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("enter your model server's API URL")
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("use an HTTP or HTTPS URL without credentials, query, or fragment")
	}
	p := strings.TrimRight(u.Path, "/")
	for _, ending := range []string{"/chat/completions", "/models", "/tags"} {
		p = strings.TrimSuffix(p, ending)
	}
	for _, ending := range []string{"/api/v1", "/api/v0", "/api"} {
		if strings.HasSuffix(p, ending) {
			p = strings.TrimSuffix(p, ending) + "/v1"
			break
		}
	}
	if p == "" {
		p = "/v1"
	}
	u.Path, u.RawPath = p, ""
	return strings.TrimRight(u.String(), "/"), nil
}

func endpointKey(env string) (string, error) {
	if env == "" {
		return "", nil
	}
	key := os.Getenv(env)
	if key == "" {
		return "", fmt.Errorf("environment variable %s is not set on the machine running Iterauthor", env)
	}
	return key, nil
}

func (h *HTTP) metadata(ctx context.Context, endpoint, key string, body any) (map[string]json.RawMessage, error) {
	method := http.MethodGet
	var data []byte
	if body != nil {
		method = http.MethodPost
		data, _ = json.Marshal(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := h.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach model server: %w", err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, 2*1024*1024+1))
	if err != nil {
		return nil, err
	}
	if len(b) > 2*1024*1024 {
		return nil, fmt.Errorf("model list exceeds 2 MiB")
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		// Servers occasionally echo request credentials in error pages.
		return nil, fmt.Errorf("model server returned HTTP %d", res.StatusCode)
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(b, &envelope); err != nil {
		return nil, fmt.Errorf("the server did not return a JSON model list")
	}
	return envelope, nil
}

func (h *HTTP) Discover(ctx context.Context, endpoint Endpoint) (Catalog, error) {
	base, err := APIBase(endpoint.URL)
	if err != nil {
		return Catalog{}, err
	}
	key, err := endpointKey(endpoint.KeyEnv)
	if err != nil {
		return Catalog{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	// Native model metadata is optional and bounded. All probes stay on the
	// supplied origin and preserve a reverse proxy's path prefix.
	root := strings.TrimSuffix(base, "/v1")
	type attempt struct{ path, provider string }
	paths := []attempt{{base + "/models", "Compatible API"}, {root + "/api/v1/models", "LM Studio"}, {root + "/api/v0/models", "LM Studio"}, {root + "/api/tags", "Ollama"}}
	result := Catalog{URL: base, Provider: "Compatible API", Notice: "Capability metadata is advisory. Test the selected model before using it."}
	byID := map[string]AvailableModel{}
	var firstErr error
	listed := false
	for _, a := range paths {
		attemptCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		envelope, err := h.metadata(attemptCtx, a.path, key, nil)
		cancel()
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		raw, ok := envelope["data"]
		if !ok {
			raw, ok = envelope["models"]
		}
		if !ok {
			continue
		}
		var rows []map[string]json.RawMessage
		if json.Unmarshal(raw, &rows) != nil {
			continue
		}
		listed = true
		if a.provider != "Compatible API" {
			result.Provider = a.provider
		}
		for _, row := range rows {
			m := parseAvailable(row)
			if m.ID == "" {
				continue
			}
			old, exists := byID[m.ID]
			if exists {
				if m.Tools == nil {
					m.Tools = old.Tools
				}
				if m.Context == 0 {
					m.Context = old.Context
				}
				if m.Loaded == nil {
					m.Loaded = old.Loaded
				}
				if m.Quantization == "" {
					m.Quantization = old.Quantization
				}
			}
			byID[m.ID] = m
		}
		if a.provider == "LM Studio" {
			break
		}
	}
	if len(byID) == 0 {
		if firstErr != nil && !listed {
			return result, fmt.Errorf("no models discovered: %w; check the API URL and optional authentication", firstErr)
		}
		return result, fmt.Errorf("the server returned no models; load or download a model in your model server, then discover again")
	}
	for _, m := range byID {
		result.Models = append(result.Models, m)
	}
	sort.Slice(result.Models, func(i, j int) bool { return result.Models[i].Name < result.Models[j].Name })
	return result, nil
}

func parseAvailable(row map[string]json.RawMessage) AvailableModel {
	str := func(key string) string { var v string; _ = json.Unmarshal(row[key], &v); return v }
	m := AvailableModel{ID: str("id"), Name: str("display_name"), Kind: str("type")}
	if m.ID == "" {
		m.ID = str("key")
	}
	if m.ID == "" {
		m.ID = str("model")
	}
	if m.ID == "" {
		m.ID = str("name")
	}
	if m.Name == "" {
		m.Name = m.ID
	}
	_ = json.Unmarshal(row["max_context_length"], &m.Context)
	if m.Context == 0 {
		_ = json.Unmarshal(row["context_window"], &m.Context)
	}
	if str("state") != "" {
		loaded := str("state") == "loaded"
		m.Loaded = &loaded
	}
	if b, ok := row["loaded_instances"]; ok {
		var instances []struct {
			Config struct {
				Context int `json:"context_length"`
			} `json:"config"`
		}
		if json.Unmarshal(b, &instances) == nil {
			loaded := len(instances) > 0
			m.Loaded = &loaded
			if loaded && instances[0].Config.Context > 0 {
				m.Context = instances[0].Config.Context
			}
		}
	}
	var caps struct {
		Tools     *bool `json:"trained_for_tool_use"`
		Reasoning struct {
			Options []string `json:"allowed_options"`
		} `json:"reasoning"`
	}
	if json.Unmarshal(row["capabilities"], &caps) == nil {
		m.Tools = caps.Tools
		m.Reasoning = caps.Reasoning.Options
	} else {
		var names []string
		if json.Unmarshal(row["capabilities"], &names) == nil {
			tools := false
			for _, name := range names {
				if name == "tools" || name == "tool_use" {
					tools = true
				}
			}
			m.Tools = &tools
		}
	}
	var quant struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(row["quantization"], &quant) == nil {
		m.Quantization = quant.Name
	} else {
		m.Quantization = str("quantization")
	}
	var details struct {
		Quant string `json:"quantization_level"`
	}
	if json.Unmarshal(row["details"], &details) == nil && m.Quantization == "" {
		m.Quantization = details.Quant
	}
	return m
}

// Probe uses at most four small inference calls. No project content or external
// tools are supplied. A tool capability is verified only after a full round trip.
func (h *HTTP) Probe(ctx context.Context, m project.Model) (ProbeResult, error) {
	base, err := APIBase(m.URL)
	if err != nil {
		return ProbeResult{}, err
	}
	m.URL = base
	if m.Name == "" {
		m.Name = m.Model
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	messages := []Message{{Role: "system", Content: "This is a brief connection test. Reply with Ready."}, {Role: "user", Content: "Reply with Ready."}}
	m.TokenField = "max_tokens"
	response, err := h.Complete(ctx, m, messages, nil, 512)
	if err != nil && strings.Contains(err.Error(), "max_completion_tokens") {
		m.TokenField = "max_completion_tokens"
		response, err = h.Complete(ctx, m, messages, nil, 512)
	}
	if err != nil {
		return ProbeResult{}, err
	}
	if strings.TrimSpace(response.Message.Content) == "" || len(response.Message.ToolCalls) != 0 {
		return ProbeResult{}, fmt.Errorf("the model did not produce text in the text-only test")
	}
	result := ProbeResult{Model: m, Text: true, Detail: "Text generation works. Tool use has not been verified."}
	result.Model.Tools = false
	m.Tools = true
	messages = []Message{{Role: "system", Content: "You are testing a tool connection. Call connection_check with value iterauthor, then reply Ready after reading the tool result."}, {Role: "user", Content: "Call connection_check now."}}
	tool := Tool{Type: "function", Function: Definition{Name: "connection_check", Description: "Checks the connection; has no side effects.", Parameters: map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{"type": "string"}}, "required": []string{"value"}, "additionalProperties": false}}}
	response, err = h.Complete(ctx, m, messages, []Tool{tool}, 512)
	if ctx.Err() != nil {
		return ProbeResult{}, ctx.Err()
	}
	if err != nil {
		result.Detail = "Text works; tool test failed: " + err.Error()
		return result, nil
	}
	if len(response.Message.ToolCalls) != 1 {
		result.Detail = "Text works, but the model did not make the requested tool call. Use it for outlining, prose, or style."
		return result, nil
	}
	call := response.Message.ToolCalls[0]
	var args struct {
		Value string `json:"value"`
	}
	if call.ID == "" || call.Function.Name != "connection_check" || json.Unmarshal([]byte(call.Function.Arguments), &args) != nil || args.Value != "iterauthor" {
		result.Detail = "Text works, but the model returned an invalid tool call."
		return result, nil
	}
	messages = append(messages, response.Message, Message{Role: "tool", ToolCallID: call.ID, Content: `{"ok":true}`})
	response, err = h.Complete(ctx, m, messages, []Tool{tool}, 512)
	if ctx.Err() != nil {
		return ProbeResult{}, ctx.Err()
	}
	if err != nil || response.Message.Content == "" || len(response.Message.ToolCalls) != 0 {
		result.Detail = "Text works, but the model did not complete the tool exchange."
		return result, nil
	}
	result.Tools, result.Model.Tools = true, true
	result.Detail = "Text generation and a complete tool exchange passed. This model is available for all task assignments."
	return result, nil
}
