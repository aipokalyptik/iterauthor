package web_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aipokalyptik/iterauthor/internal/application"
	"github.com/aipokalyptik/iterauthor/internal/model"
	"github.com/aipokalyptik/iterauthor/internal/project"
	"github.com/aipokalyptik/iterauthor/internal/web"
)

func TestLiveReplySurvivesBrowserDetachAndRetainsPartialOnStop(t *testing.T) {
	for _, ending := range []string{"complete", "cancel", "disconnect"} {
		t.Run(ending, func(t *testing.T) {
			textReady, finish := make(chan struct{}), make(chan struct{})
			var calls atomic.Int32
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"PRIVATE THOUGHT\"}}]}\n\n")
				w.(http.Flusher).Flush()
				select {
				case <-textReady:
				case <-r.Context().Done():
					return
				}
				fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"First live sentence.\"}}]}\n\n")
				w.(http.Flusher).Flush()
				select {
				case <-finish:
				case <-r.Context().Done():
					return
				}
				if ending == "complete" {
					fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\" Final sentence.\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
				}
			}))
			defer provider.Close()
			p, err := project.Create(filepath.Join(t.TempDir(), "story"), "Live chat", false)
			if err != nil {
				t.Fatal(err)
			}
			config := p.Config.Clone()
			config.Models["base"] = project.Model{Name: "Fixture", Model: "fixture", URL: provider.URL, Tools: true}
			if err := p.SaveConfig(config, "Setup", config.Root); err != nil {
				t.Fatal(err)
			}
			core := application.New(p, model.NewHTTP(), false)
			defer func() {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				if err := core.Shutdown(ctx); err != nil {
					t.Error(err)
				}
			}()
			srv := httptest.NewServer(web.New(core))
			defer srv.Close()
			var conversation project.Conversation
			if err := json.Unmarshal(request(t, srv, "/api/command", map[string]any{"action": "conversation", "target": "story", "kind": "advice", "model": "base"}, 200), &conversation); err != nil {
				t.Fatal(err)
			}
			request(t, srv, "/api/command", map[string]any{"action": "send", "id": conversation.ID, "text": "Help me outline"}, 200)
			wait := func(match func(application.View) bool) application.View {
				t.Helper()
				updates, unsubscribe := core.Subscribe()
				defer unsubscribe()
				deadline := time.NewTimer(2 * time.Second)
				defer deadline.Stop()
				for {
					v := core.View()
					if match(v) {
						return v
					}
					select {
					case <-updates:
					case <-deadline.C:
						t.Fatal("live state did not arrive")
					}
				}
			}
			v := wait(func(v application.View) bool { return v.Work != nil && v.Work.Phase == "thinking" })
			if v.Work.Preview != "" {
				t.Fatal("reasoning leaked into preview")
			}
			close(textReady)
			v = wait(func(v application.View) bool { return v.Work.Preview == "First live sentence." })
			if !v.Busy || v.Work.Phase != "writing" || v.Work.Model != "Fixture" {
				t.Fatalf("missing activity: %+v", v.Work)
			}
			// A real browser SSE subscription can disconnect without stopping work.
			client := &http.Client{Timeout: time.Second}
			resp, err := client.Get(srv.URL + "/api/events")
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			var reconnected application.View
			state := request(t, srv, "/api/state", nil, 200)
			if err := json.Unmarshal(state, &reconnected); err != nil {
				t.Fatal(err)
			}
			if !reconnected.Busy || reconnected.Work.Preview != v.Work.Preview || reconnected.Work.Run != v.Work.Run || strings.Contains(string(state), "PRIVATE THOUGHT") {
				t.Fatal("reconnect lost preview or exposed reasoning")
			}
			c, _ := core.LoadConversation(conversation.ID)
			if len(c.Turns) != 1 {
				t.Fatal("partial updates were persisted as duplicate turns")
			}
			if ending == "cancel" {
				request(t, srv, "/api/command", map[string]any{"action": "cancel"}, 200)
			} else {
				close(finish)
			}
			waitIdle(t, core)
			c, err = core.LoadConversation(conversation.ID)
			if err != nil {
				t.Fatal(err)
			}
			status := map[string]string{"complete": "Available", "cancel": "Canceled", "disconnect": "Failed"}[ending]
			if calls.Load() != 1 || len(c.Turns) != 2 || c.Turns[1].Status != status || !strings.Contains(c.Turns[1].Text, "First live sentence.") {
				t.Fatalf("reply lifecycle failed: calls=%d turns=%+v", calls.Load(), c.Turns)
			}
			if ending == "complete" && c.Turns[1].Text != "First live sentence. Final sentence." {
				t.Fatal("final text was duplicated or lost")
			}
		})
	}
}
