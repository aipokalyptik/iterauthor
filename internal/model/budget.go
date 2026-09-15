package model

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aipokalyptik/iterauthor/internal/project"
)

// Budget records estimates separately from provider-reported token usage.
// This heuristic is deliberately conservative for ordinary English fiction;
// it is not a tokenizer and never removes source text to make a request fit.
type Budget struct {
	Requested            int    `json:"requested"`
	OutputTokens         int    `json:"output_tokens"`
	EstimatedInputTokens int    `json:"estimated_input_tokens"`
	Context              int    `json:"context,omitempty"`
	ContextSource        string `json:"context_source,omitempty"`
	Note                 string `json:"note"`
}

func PlanOutput(stage string, m project.Model, messages []Message, tools []Tool, requested int) (Budget, error) {
	b := Budget{Requested: requested, OutputTokens: requested, Context: m.Context, ContextSource: m.ContextSource}
	if !project.ValidOutputTokens(requested) {
		return b, fmt.Errorf("output tokens must be Automatic (-1), Unlimited (0), or 64–1048576")
	}
	data, err := json.Marshal(struct {
		Messages []Message
		Tools    []Tool
	}{messages, tools})
	if err != nil {
		return b, err
	}
	b.EstimatedInputTokens = (len(data)+2)/3 + 16*len(messages) + 256
	b.Note = "Input tokens are estimated, including tools; the server's tokenizer and limits remain authoritative."
	if requested == 0 {
		b.Note += " Unlimited: no output limit parameter is sent; the server's configured default still applies."
		return b, nil
	}
	if requested == -1 {
		b.OutputTokens = 8192
		switch stage {
		case "knowledge", "outline-context", "consistency", "style":
			b.OutputTokens = 4096
		case "prose":
			b.OutputTokens = 16384
		}
		// Unknown/default reasoning may consume the entire completion allowance.
		// Reserve room without guessing capability from the model's name.
		if m.Reasoning != "off" && m.Reasoning != "none" && !(len(m.ReasoningOptions) == 1 && (m.ReasoningOptions[0] == "off" || m.ReasoningOptions[0] == "none")) {
			b.OutputTokens = 32768
		}
	}
	if m.Context > 0 {
		available := m.Context - b.EstimatedInputTokens - 1024
		if available < 64 {
			return b, fmt.Errorf("estimated input (%d tokens) leaves too little output space in %s's reported %d-token context; narrow the attached context or increase the model's loaded context, then refresh Models", b.EstimatedInputTokens, m.Name, m.Context)
		}
		if b.OutputTokens > available {
			b.OutputTokens = available
			b.Note += fmt.Sprintf(" Output allowance reduced to %d to reserve space for the input and a 1,024-token margin.", available)
		}
	} else {
		b.Note += " The API did not report context capacity."
	}
	return b, nil
}

func (b Budget) Label() string {
	output := fmt.Sprintf("%d output tokens", b.OutputTokens)
	if b.OutputTokens == 0 {
		output = "unlimited output"
	}
	return strings.TrimSpace(output + fmt.Sprintf(" · ~%d input tokens", b.EstimatedInputTokens))
}
