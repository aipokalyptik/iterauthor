package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"regexp"
	"strings"
)

// LogActivity lets a host report its own operations using the same console.
// Messages and attributes must describe operations, never request/source text.
func (s *Service) LogActivity(message string, attrs ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log(slog.LevelInfo, message, nil, attrs...)
}

// LogError summarizes errors without copying provider bodies, prompts, or
// arbitrary request values to the console. Full errors remain in the UI/run.
func (s *Service) LogError(message string, err error, attrs ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log(slog.LevelError, message, err, attrs...)
}

// Callers of the internal helpers hold s.mu.
func (s *Service) log(level slog.Level, message string, err error, attrs ...any) {
	if s.logger == nil {
		return
	}
	if err != nil {
		attrs = append(attrs, "error", consoleError(err))
	}
	s.logger.Log(context.Background(), level, message, attrs...)
}
func (s *Service) logWorkAt(level slog.Level, message string, err error, attrs ...any) {
	if w := s.workInfo; w != nil {
		attrs = append(attrs, "kind", w.Kind, "target", w.Target, "run", w.Run, "calls", w.Calls)
	}
	s.log(level, message, err, attrs...)
}

var httpFailure = regexp.MustCompile(`(?:model HTTP|model server returned HTTP) ([1-5][0-9]{2})\b`)

func consoleError(err error) string {
	if err == nil {
		return ""
	}
	text := err.Error()
	// Provider response bodies and refusal explanations can echo story content.
	// Use only the status, never the rest of the response.
	if match := httpFailure.FindStringSubmatch(text); len(match) > 1 {
		return "Model API returned HTTP " + match[1] + "; inspect Activity for provider details"
	}
	if errors.Is(err, context.Canceled) || text == context.Canceled.Error() {
		return "Operation canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) || text == context.DeadlineExceeded.Error() {
		return "Operation time limit reached"
	}
	var network net.Error
	if errors.As(err, &network) {
		if network.Timeout() {
			return "Network request timed out"
		}
		return "Could not reach the model server"
	}
	var file *os.PathError
	if errors.As(err, &file) {
		return fmt.Sprintf("File operation failed: %v", file.Err)
	}
	for _, pair := range [][2]string{
		{"model refused:", "Model refused the request; inspect Activity for details"},
		{"invalid request:", "Invalid request JSON"},
		{"unknown command", "Unknown command"},
		{"this file changed; reload", "Source changed since it was opened; reload before saving"},
		{"files changed outside iterauthor", "Project files changed externally; reload before continuing"},
		{"output-token limit before", "Output limit reached before any visible text; adjust output/reasoning settings"},
		{"output reached its token limit", "Output limit reached; partial output retained"},
		{"model-call budget exhausted", "Model-call budget exhausted"},
		{"Draft limit reached", "Draft limit reached; candidates retained"},
		{"Context characters", "Input exceeds the context character limit"},
		{"required context is too large", "Required context exceeds the input budget"},
		{"leaves too little output space", "Input leaves too little space in the model context"},
		{"returned empty text", "Model returned no text or tool calls"},
		{"invalid or duplicate tool call", "Model returned an invalid tool call"},
		{"invalid model response", "Model returned an invalid response"},
		{"expected a text chat response", "Model did not return a text response"},
		{"contained no choices", "Model response contained no choices"},
		{"content filter", "Provider blocked the output"},
		{"did not honor", "Model/server did not honor the reasoning setting"},
		{"does not report support for reasoning", "Selected reasoning level is unsupported"},
		{"requires tools", "This operation requires tool support"},
		{"tool support", "Check this model's tool support settings"},
		{"tool exchange", "Model did not complete the tool exchange"},
		{"tool call", "Model did not complete the requested tool call"},
		{"environment variable", "The API key environment variable is not set or is invalid"},
		{"no models discovered", "No models discovered; check the API connection"},
		{"save run:", "Could not save the generation record"},
		{ErrBusy.Error(), ErrBusy.Error()}, {ErrStale.Error(), ErrStale.Error()},
		{ErrChanges.Error(), ErrChanges.Error()}, {ErrClosed.Error(), ErrClosed.Error()},
	} {
		if strings.Contains(text, pair[0]) {
			return pair[1]
		}
	}
	return "Operation failed; see the browser or Activity for details"
}

// Console stages are fixed text. Provider/source-controlled values stay out of
// log messages; model/run IDs remain available in the operation metadata.
func consoleStage(stage string) string {
	switch {
	case strings.HasPrefix(stage, "Preparing "):
		return "Preparing generation context"
	case strings.HasPrefix(stage, "No additional knowledge"):
		return "Knowledge context ready"
	case strings.HasPrefix(stage, "No additional outline"):
		return "Outline context ready"
	case strings.HasPrefix(stage, "Draft "):
		return "Draft complete; starting reviews"
	case strings.Contains(stage, " · tool "):
		_, tool, _ := strings.Cut(stage, " · tool ")
		switch tool {
		case "read_entry":
			return "Reading a project reference"
		case "search_entries":
			return "Searching project references"
		case "propose_edit":
			return "Staging a source edit proposal"
		}
		return "Running a model tool"
	}
	role, _, _ := strings.Cut(stage, " · ")
	switch role {
	case "knowledge":
		return "Selecting knowledge context"
	case "outline-context":
		return "Selecting outline context"
	case "outline":
		return "Developing the outline"
	case "prose":
		return "Writing prose"
	case "consistency":
		return "Checking story consistency"
	case "style":
		return "Checking writing style"
	case "advice":
		return "Asking the writing assistant"
	case "edit":
		return "Preparing source edit proposals"
	case "tool test":
		return "Testing model tools"
	}
	return "Working"
}

func consoleFinish(reason string) string {
	switch reason {
	case "stop", "length", "tool_calls", "content_filter":
		return reason
	}
	return "unknown"
}
