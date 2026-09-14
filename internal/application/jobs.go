package application

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aipokalyptik/iterauthor/internal/engine"
	"github.com/aipokalyptik/iterauthor/internal/project"
)

// Selection scopes commands using stable item IDs, never UI row indices.
type Selection struct {
	Scope  string // branch, selected, story
	Target string
	IDs    []string
}

func (s *Service) passages(sel Selection) ([]string, error) {
	var ids []string
	switch sel.Scope {
	case "story":
		ids = s.store.Config.Leaves(s.store.Config.Root)
	case "branch":
		if s.store.Config.Nodes[sel.Target] == nil {
			return nil, fmt.Errorf("unknown outline %s", sel.Target)
		}
		ids = s.store.Config.Leaves(sel.Target)
	case "selected":
		ids = sel.IDs
	default:
		return nil, fmt.Errorf("unknown passage selection %q", sel.Scope)
	}
	seen := make(map[string]bool)
	var out []string
	for _, id := range ids {
		n := s.store.Config.Nodes[id]
		if n == nil || len(n.Children) != 0 {
			return nil, fmt.Errorf("%s is not a passage", id)
		}
		if !seen[id] {
			out = append(out, id)
			seen[id] = true
		}
	}
	return out, nil
}

func (s *Service) Generate(sel Selection, force bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.idle(); err != nil {
		return err
	}
	return s.generate(sel, force)
}

func (s *Service) generate(sel Selection, force bool) error {
	if len(s.store.State.Changes) > 0 {
		return ErrChanges
	}
	ids, err := s.passages(sel)
	if err != nil {
		return err
	}
	var eligible []string
	for _, id := range ids {
		status := s.store.Status(id)
		if force || status == "Missing" || status == "Invalidated" {
			eligible = append(eligible, id)
		}
	}
	if len(eligible) == 0 {
		s.progress = "No eligible passages. Regenerate requests replacement candidates."
		s.changed()
		return nil
	}
	snapshot, err := s.store.Snapshot()
	if err != nil {
		return err
	}
	s.editing = false
	s.queue = eligible[1:]
	ctx := s.start()
	go s.work(ctx, snapshot, Job{Kind: "prose", Target: eligible[0]}, "", nil)
	return nil
}

// Job describes operations with supplied input and retained output. Conversation
// turns use Send instead, so interfaces cannot forget to persist their history.
type Job struct{ Kind, Target, Model, Prompt string }

func (s *Service) Start(job Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.idle(); err != nil {
		return err
	}
	if job.Kind != "outline" && job.Kind != "test" {
		return fmt.Errorf("unsupported job kind %q", job.Kind)
	}
	if s.store.Config.Nodes[job.Target] == nil {
		return fmt.Errorf("choose an outline item")
	}
	if job.Kind == "test" && job.Model == "" {
		job.Model = s.store.Config.BaseModel
	}
	if job.Model != "" {
		if _, ok := s.store.Config.Models[job.Model]; !ok {
			return fmt.Errorf("unknown model")
		}
		if job.Kind == "test" && !s.store.Config.Models[job.Model].Tools {
			return fmt.Errorf("this model is configured without tools")
		}
	}
	snapshot, err := s.store.Snapshot()
	if err != nil {
		return err
	}
	s.editing = true
	ctx := s.start()
	go s.work(ctx, snapshot, job, "", nil)
	return nil
}

func (s *Service) start() context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.store.Config.Limits.Minutes)*time.Minute)
	s.cancel, s.done = cancel, make(chan struct{})
	s.busy, s.canceling = true, false
	s.progress = "Starting work"
	s.changed()
	return ctx
}

// One worker owns the whole queue, including commit and budget accounting. No
// observer callback advances it, saves replies, or activates candidates.
func (s *Service) work(ctx context.Context, snapshot project.Snapshot, job Job, conversationID string, history []project.Turn) {
	remaining := snapshot.Config.Limits.Calls
	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.cancel()
		s.cancel = nil
		s.busy, s.canceling = false, false
		s.queue = nil
		s.changed()
		close(s.done)
	}()
	for {
		s.mu.Lock()
		snapshot.Config.Limits.Calls = remaining
		s.progress = "Starting " + job.Kind + " for " + snapshot.Config.TitleOf(job.Target)
		s.changed()
		s.mu.Unlock()
		e := engine.Engine{Client: s.client, Demo: s.demo,
			Save:     func(r project.Run) error { s.mu.Lock(); defer s.mu.Unlock(); return s.store.SaveRun(r) },
			Progress: func(stage string) { s.mu.Lock(); defer s.mu.Unlock(); s.progress = stage; s.changed() }}
		// Even cancellation immediately after submission produces a retained run.
		run := e.Run(ctx, snapshot, job.Target, job.Kind, job.Model, job.Prompt, history)
		s.mu.Lock()
		remaining -= run.Calls
		var commitErr error
		if job.Kind == "prose" {
			commitErr = s.store.OfferRun(run, run.Status == "Available" && ctx.Err() == nil)
		}
		if conversationID != "" {
			commitErr = s.completeConversation(conversationID, run)
		}
		s.lastRun = run.ID
		s.progress = run.Kind + ": " + run.Status
		if run.Error != "" {
			s.progress += " · " + run.Error
		}
		if commitErr != nil {
			s.progress += " · saving result: " + commitErr.Error()
		}
		s.changed()
		if job.Kind != "prose" || run.Status == "Canceled" || run.Status == "Failed" || commitErr != nil || ctx.Err() != nil || len(s.queue) == 0 {
			s.mu.Unlock()
			return
		}
		if remaining <= 0 {
			s.progress = "Queue budget exhausted. Candidates are retained in Activity."
			s.mu.Unlock()
			return
		}
		var err error
		snapshot, err = s.store.Snapshot()
		if err != nil {
			s.progress = err.Error()
			s.mu.Unlock()
			return
		}
		job.Target, s.queue = s.queue[0], s.queue[1:]
		s.mu.Unlock()
	}
}

func (s *Service) Send(id, prompt string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.idle(); err != nil {
		return err
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return fmt.Errorf("message is empty")
	}
	c, err := s.store.LoadConversation(id)
	if err != nil {
		return err
	}
	if err = s.validateConversation(c); err != nil {
		return err
	}
	snapshot, err := s.store.Snapshot()
	if err != nil {
		return err
	}
	history := append([]project.Turn(nil), c.Turns...)
	c.Turns = append(c.Turns, project.Turn{Role: "author", Text: prompt})
	c.Draft = ""
	if err = s.store.SaveConversation(c); err != nil {
		return err
	}
	s.editing = true
	ctx := s.start()
	go s.work(ctx, snapshot, Job{Kind: c.Mode, Target: c.Target, Model: c.Model, Prompt: prompt}, c.ID, history)
	return nil
}

func (s *Service) completeConversation(id string, run project.Run) error {
	c, err := s.store.LoadConversation(id)
	if err != nil {
		return err
	}
	reply := run.Text
	if run.Error != "" {
		reply += "\n" + run.Error
	}
	if len(run.Edits) > 0 {
		reply += fmt.Sprintf("\n%d edit proposal(s) retained. Open this run in Activity to apply them.", len(run.Edits))
	}
	c.Turns = append(c.Turns, project.Turn{Role: "assistant", Text: reply, Run: run.ID})
	return s.store.SaveConversation(c)
}
