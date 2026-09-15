package web

import (
	"net/http"
)

// Observe status and errors, not response bodies. Unwrap preserves streaming
// through http.ResponseController (including the event stream's Flush).
type loggedResponse struct {
	http.ResponseWriter
	status int
	err    error
}

func (w *loggedResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *loggedResponse) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *loggedResponse) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(data)
}
func (w *loggedResponse) RecordError(err error) { w.err = err }

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		record := &loggedResponse{ResponseWriter: w}
		defer func() {
			if record.status < 400 {
				return
			}
			// Pattern is the registered route, never a URL with caller-supplied values.
			route := r.Pattern
			if route == "" {
				route = "unmatched request"
			}
			s.core.LogError("Request failed", record.err, "route", route, "status", record.status, "reason", http.StatusText(record.status))
		}()
		next.ServeHTTP(record, r)
	})
}

func commandActivity(action string) string {
	switch action {
	case "save":
		return "Saving source text"
	case "config":
		return "Saving project settings"
	case "child":
		return "Adding an outline item"
	case "entry":
		return "Adding a wiki entry"
	case "begin-editing":
		return "Pausing generation for editing"
	case "finish-editing":
		return "Finishing editing"
	case "reload":
		return "Reloading project files"
	case "decide":
		return "Applying a prose revisit decision"
	case "invalidate", "invalidate-run":
		return "Invalidating generated content"
	case "use-candidate":
		return "Applying a prose candidate"
	case "import-outline":
		return "Importing an outline proposal"
	case "apply-edits":
		return "Applying source edit proposals"
	case "conversation":
		return "Starting a writing conversation"
	case "conversation-model":
		return "Changing the conversation model"
	case "save-connection":
		return "Saving an API connection"
	case "save-model":
		return "Saving model settings"
	case "export":
		return "Exporting the manuscript"
	}
	// Generation/cancellation has core lifecycle logging; autosaved drafts and
	// normal reads/polls need no additional console messages.
	return ""
}
