// Package application coordinates a single open writing project. It owns the
// store and worker lifecycle; interfaces render its views and submit commands.
package application

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/aipokalyptik/iterauthor/internal/model"
	"github.com/aipokalyptik/iterauthor/internal/project"
)

var (
	ErrBusy    = errors.New("cancel active work and wait before editing")
	ErrClosed  = errors.New("project service is closing or closed")
	ErrChanges = errors.New("decide how saved changes affect prose before generating")
	ErrStale   = errors.New("the edited configuration has changed; reopen it before saving")
)

// View is detached from the service. Revision changes on progress as well as
// source changes; ConfigVersion is the optimistic concurrency token for forms.
type View struct {
	Catalogs                                map[string]CatalogState
	Revision                                uint64
	ConfigVersion                           string
	Dir                                     string
	Config                                  project.Config
	State                                   project.State
	Editing, Busy, Canceling, Closing, Demo bool
	Queue                                   []string
	Progress                                string
	LastRun                                 string
	Work                                    *Work
}

// Work identifies the current or most recent operation independently of a UI.
// Progress remains readable after completion, including fast failures.
type Work struct {
	Kind         string `json:"kind"`
	Target       string `json:"target"`
	Conversation string `json:"conversation,omitempty"`
	Started      string `json:"started"`
	Finished     string `json:"finished,omitempty"`
	Run          string `json:"run,omitempty"`
	Status       string `json:"status"`
	Calls        int    `json:"calls"`
}

type Service struct {
	catalogs                                  map[string]CatalogState
	projectEpoch                              uint64
	mu                                        sync.Mutex
	store                                     *project.Store
	client                                    model.Client
	demo                                      bool
	editing, busy, canceling, closing, closed bool
	queue                                     []string
	cancel                                    context.CancelFunc
	done                                      chan struct{}
	progress, lastRun                         string
	revision                                  uint64
	subscribers                               map[chan struct{}]struct{}
	workInfo                                  *Work
	logger                                    *slog.Logger
}

// New transfers exclusive ownership of store to the service. The composition
// root supplies the model client; neither the engine nor a UI chooses transport.
func New(store *project.Store, client model.Client, demo bool) *Service {
	s := &Service{store: store, client: client, demo: demo, editing: true,
		subscribers: make(map[chan struct{}]struct{}), revision: 1}
	s.loadCatalogs()
	return s
}

func configVersion(c project.Config) string {
	b, _ := json.Marshal(c)
	return fmt.Sprintf("%x", sha256.Sum256(b))
}

func (s *Service) View() View {
	s.mu.Lock()
	defer s.mu.Unlock()
	var state project.State
	b, _ := json.Marshal(s.store.State)
	_ = json.Unmarshal(b, &state)
	var work *Work
	if s.workInfo != nil {
		copy := *s.workInfo
		work = &copy
	}
	return View{Revision: s.revision, ConfigVersion: configVersion(s.store.Config),
		Dir: s.store.Dir, Config: s.modelConfig(), Catalogs: s.catalogView(), State: state,
		Editing: s.editing, Busy: s.busy, Canceling: s.canceling, Closing: s.closing,
		Demo: s.demo, Queue: append([]string(nil), s.queue...), Progress: s.progress, LastRun: s.lastRun, Work: work}
}

// SetLogger enables operational console feedback for hosts that have a console.
// It logs lifecycle metadata, not prompts, sources, credentials or model replies.
func (s *Service) SetLogger(logger *slog.Logger) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logger = logger
}

func (s *Service) logWork(message string, attrs ...any) {
	s.logWorkAt(slog.LevelInfo, message, nil, attrs...)
}

// Revision lets a rendering adapter cheaply test whether it needs a new View.
func (s *Service) Revision() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.revision
}

// Subscribe sends coalesced wakeups, not a transaction log. Read View after a
// wakeup, including the initial one. An absent/slow subscriber never delays work.
// Unsubscribe is idempotent; Shutdown closes all remaining subscriptions.
func (s *Service) Subscribe() (<-chan struct{}, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch := make(chan struct{}, 1)
	if s.closed {
		close(ch)
	} else {
		s.subscribers[ch] = struct{}{}
		ch <- struct{}{}
	}
	return ch, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if _, ok := s.subscribers[ch]; ok {
			delete(s.subscribers, ch)
			close(ch)
		}
	}
}

func (s *Service) changed() {
	s.revision++
	for ch := range s.subscribers {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
func (s *Service) available() error {
	if s.closing {
		return ErrClosed
	}
	return nil
}
func (s *Service) idle() error {
	if err := s.available(); err != nil {
		return err
	}
	if s.busy {
		return ErrBusy
	}
	return nil
}

func (s *Service) edit(fn func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.idle(); err != nil {
		return err
	}
	s.editing = true
	defer s.changed()
	return fn()
}

func (s *Service) BeginEditing() error { return s.edit(func() error { return nil }) }

// FinishEditing ends the editing pause without starting or resuming model work.
// Only explicit Generate, Start, Send, and ProbeModel requests run inference.
func (s *Service) FinishEditing() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.idle(); err != nil {
		return err
	}
	s.editing = false
	s.changed()
	return nil
}

func (s *Service) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.busy {
		return
	}
	s.queue = nil
	s.canceling = true
	s.cancel()
	s.progress = "Cancellation requested; waiting for the model call to stop."
	s.logWork("Cancellation requested")
	s.changed()
}

// Shutdown cancels work and waits before releasing the project lock. A deadline
// error leaves the service closing and the lock held; callers may retry waiting.
func (s *Service) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closing = true
	s.queue = nil
	if s.cancel != nil {
		s.canceling = true
		s.cancel()
	}
	done := s.done
	s.changed()
	s.mu.Unlock()
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.store.Close()
		s.closed = true
		for ch := range s.subscribers {
			close(ch)
			delete(s.subscribers, ch)
		}
	}
	return nil
}

// SwitchProject preserves the old project if opening the next one fails.
func (s *Service) SwitchProject(dir, title string, create bool) error {
	return s.edit(func() error {
		var next *project.Store
		var err error
		if create {
			next, err = project.Create(dir, title, false)
		} else {
			next, err = project.Open(dir)
		}
		if err != nil {
			return err
		}
		s.store.Close()
		s.store = next
		s.projectEpoch++
		s.loadCatalogs()
		s.lastRun, s.progress = "", ""
		s.workInfo = nil
		return nil
	})
}
