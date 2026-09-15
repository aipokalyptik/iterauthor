package project

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"
)

// Connection owns the address and credential reference shared by its models.
// Catalogs are runtime metadata, not authored configuration.
type Connection struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	KeyEnv   string `json:"key_env,omitempty"`
	Provider string `json:"provider,omitempty"`
}

type Inference struct {
	// nil inherits; zero removes the application's output cap.
	OutputTokens *int `json:"output_tokens,omitempty"`
	// Empty inherits. "default" explicitly restores the server default.
	Reasoning string `json:"reasoning,omitempty"`
}

func ModelID(connection, model string) string {
	return fmt.Sprintf("m-%x", sha256.Sum256([]byte(connection+"\x00"+model)))[:34]
}

// WithConnections upgrades legacy inline endpoints without renaming model IDs.
// It is a view transformation; reading a project never writes its sources.
func (c Config) WithConnections() Config {
	c = c.Clone()
	if c.Connections == nil {
		c.Connections = map[string]Connection{}
	}
	for _, id := range c.ModelIDs() {
		m := c.Models[id]
		if m.Connection != "" || m.Model == "" || m.URL == "" {
			continue
		}
		ref := ""
		for key, con := range c.Connections {
			if con.URL == m.URL && con.KeyEnv == m.KeyEnv {
				ref = key
				break
			}
		}
		if ref == "" {
			ref = fmt.Sprintf("api-%x", sha256.Sum256([]byte(m.URL+"\x00"+m.KeyEnv)))[:24]
			c.Connections[ref] = Connection{Name: m.Name + " API", URL: m.URL, KeyEnv: m.KeyEnv}
		}
		m.Connection = ref
		c.Models[id] = m
	}
	return c
}

func (c Config) ConnectedModel(id string) Model {
	m := c.Models[id]
	if con, ok := c.Connections[m.Connection]; ok {
		m.URL, m.KeyEnv = con.URL, con.KeyEnv
		if con.Provider != "" {
			m.Provider = con.Provider
		}
	}
	return m
}

func (c Config) InferenceFor(node, role, modelID string) Inference {
	m := c.Models[modelID]
	tokens := c.Limits.OutputTokens
	result := Inference{OutputTokens: &tokens, Reasoning: m.Reasoning}
	if m.OutputTokens != nil {
		result.OutputTokens = m.OutputTokens
	}
	apply := func(v Inference) {
		if v.OutputTokens != nil {
			result.OutputTokens = v.OutputTokens
		}
		if v.Reasoning != "" {
			result.Reasoning = v.Reasoning
		}
	}
	apply(c.Inference[role])
	for _, id := range c.Ancestors(node) {
		apply(c.Nodes[id].Inference[role])
	}
	return result
}

func ValidOutputTokens(n int) bool { return n == 0 || n >= 64 && n <= 1048576 }
func (v Inference) Validate() error {
	if v.OutputTokens != nil && !ValidOutputTokens(*v.OutputTokens) {
		return fmt.Errorf("output tokens must be 0 (unlimited) or 64–1048576")
	}
	switch v.Reasoning {
	case "", "default", "off", "on", "none", "minimal", "low", "medium", "high", "xhigh":
	default:
		return fmt.Errorf("unknown reasoning setting %q", v.Reasoning)
	}
	return nil
}

// WorkContext shares the same policy between the service and standalone engine.
func WorkContext(ctx context.Context, minutes int) (context.Context, context.CancelFunc) {
	if minutes == 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, time.Duration(minutes)*time.Minute)
}
