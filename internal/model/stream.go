package model

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/aipokalyptik/iterauthor/internal/project"
)

// Delta contains public answer text and activity signals, never reasoning text
// or tool arguments. Callbacks run synchronously inside the request's lifetime.
type Delta struct {
	Text     string
	Thinking bool
	Tool     bool
}

// StreamingClient is optional; clients implementing only Complete remain usable.
type StreamingClient interface {
	CompleteStream(context.Context, project.Model, []Message, []Tool, int, func(Delta)) (Response, error)
}

// readStream assembles one Chat Completions choice, including tool arguments
// split across events. Limits apply to the wire data, including keepalives.
func readStream(body io.Reader, emit func(Delta)) (Response, error) {
	result := Response{Message: Message{Role: "assistant"}}
	limited := &io.LimitedReader{R: body, N: 32*1024*1024 + 1}
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	var text, reasoning, event strings.Builder
	var calls [8]*ToolCall
	finish := func(err error) (Response, error) {
		result.Message.Content = text.String()
		result.Message.ReasoningContent = reasoning.String()
		for _, call := range calls {
			if call != nil {
				result.Message.ToolCalls = append(result.Message.ToolCalls, *call)
			}
		}
		return result, err
	}
	consume := func() (bool, error) {
		data := strings.TrimSpace(event.String())
		event.Reset()
		if data == "" {
			return false, nil
		}
		if data == "[DONE]" {
			return true, nil
		}
		var chunk struct {
			Error   json.RawMessage `json:"error"`
			Choices []struct {
				Index  int    `json:"index"`
				Finish string `json:"finish_reason"`
				Delta  struct {
					Content          string `json:"content"`
					ReasoningContent string `json:"reasoning_content"`
					Reasoning        string `json:"reasoning"`
					Refusal          string `json:"refusal"`
					ToolCalls        []struct {
						Index    int      `json:"index"`
						ID       string   `json:"id"`
						Type     string   `json:"type"`
						Function Function `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
			Usage *struct {
				Tokens  int  `json:"total_tokens"`
				Input   *int `json:"prompt_tokens"`
				Output  *int `json:"completion_tokens"`
				Details struct {
					Reasoning *int `json:"reasoning_tokens"`
				} `json:"completion_tokens_details"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return false, fmt.Errorf("invalid model stream event; partial output retained: %w", err)
		}
		if len(chunk.Error) > 0 && string(chunk.Error) != "null" {
			return false, fmt.Errorf("model reported a streaming error; partial output retained; check the model server")
		}
		if u := chunk.Usage; u != nil {
			result.Tokens, result.InputTokens, result.OutputTokens, result.ReasoningTokens = u.Tokens, u.Input, u.Output, u.Details.Reasoning
		}
		for _, c := range chunk.Choices {
			if c.Index != 0 {
				continue
			}
			if c.Delta.Refusal != "" {
				return false, fmt.Errorf("model refused the request; partial output retained")
			}
			if result.Finish != "" {
				return false, fmt.Errorf("model sent additional output after finishing; partial output retained")
			}
			text.WriteString(c.Delta.Content)
			reasoning.WriteString(c.Delta.ReasoningContent)
			// Preserve the provider's alternate reasoning field for tool round trips.
			if c.Delta.Reasoning != "" {
				var previous string
				_ = json.Unmarshal(result.Message.Reasoning, &previous)
				result.Message.Reasoning, _ = json.Marshal(previous + c.Delta.Reasoning)
			}
			for _, part := range c.Delta.ToolCalls {
				if part.Index < 0 || part.Index >= len(calls) {
					return false, fmt.Errorf("model returned an invalid tool index or more than eight tools")
				}
				if calls[part.Index] == nil {
					calls[part.Index] = &ToolCall{}
				}
				call := calls[part.Index]
				call.ID += part.ID
				call.Type += part.Type
				call.Function.Name += part.Function.Name
				call.Function.Arguments += part.Function.Arguments
			}
			emit(Delta{Text: c.Delta.Content, Thinking: c.Delta.ReasoningContent != "" || c.Delta.Reasoning != "", Tool: len(c.Delta.ToolCalls) > 0})
			if c.Finish != "" {
				result.Finish = c.Finish
			}
		}
		return false, nil
	}
	done := false
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			var err error
			done, err = consume()
			if err != nil {
				return finish(err)
			}
			if done {
				break
			}
		} else if strings.HasPrefix(line, "data:") {
			event.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
			event.WriteByte('\n')
			if event.Len() > 1024*1024 {
				return finish(fmt.Errorf("model stream event exceeds 1 MiB"))
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return finish(fmt.Errorf("model stream interrupted; partial output retained: %w", err))
	}
	if limited.N == 0 {
		return finish(fmt.Errorf("model response exceeds 32 MiB"))
	}
	if !done && event.Len() > 0 {
		if _, err := consume(); err != nil {
			return finish(err)
		}
	}
	// A finish reason is required even if [DONE] arrives. Some compatible servers
	// close immediately after the finish event instead of sending [DONE].
	if result.Finish == "" {
		return finish(fmt.Errorf("model stream ended before completing the reply; partial output retained; retry explicitly when ready"))
	}
	return finish(nil)
}
