package application

import (
	"fmt"

	"github.com/aipokalyptik/iterauthor/internal/project"
)

func (s *Service) SaveText(id, field, text, expected string) error {
	return s.edit(func() error {
		if err := s.editableField(id, field, true); err != nil {
			return err
		}
		return s.store.SaveText(id, field, text, expected)
	})
}
func (s *Service) editableField(id, field string, private bool) error {
	if field == "notes" && private {
		_, err := s.store.Path(id, field)
		return err
	}
	if s.store.Config.Knowledge[id] != nil && field == "entry" {
		return nil
	}
	if n := s.store.Config.Nodes[id]; n != nil {
		if field == "outline" || field == "style" || (field == "prose" && len(n.Children) == 0) {
			return nil
		}
	}
	return fmt.Errorf("%s / %s is not an editable source", id, field)
}
func (s *Service) SaveConfig(c project.Config, description, target, expectedVersion string) error {
	return s.edit(func() error {
		if configVersion(s.store.Config) != expectedVersion {
			return ErrStale
		}
		// A dropdown may have refreshed since this form opened. Resolve any new
		// selections against the current catalog without altering other edits.
		c = c.Clone()
		live := s.modelConfig()
		if c.Connections == nil {
			c.Connections = map[string]project.Connection{}
		}
		add := func(id string) {
			if _, ok := c.Models[id]; ok {
				return
			}
			if m, ok := live.Models[id]; ok {
				c.Models[id] = m
				if con, ok := live.Connections[m.Connection]; ok {
					c.Connections[m.Connection] = con
				}
			}
		}
		add(c.BaseModel)
		for _, id := range c.Defaults {
			add(id)
		}
		for _, n := range c.Nodes {
			for _, id := range n.Models {
				add(id)
			}
		}
		return s.store.SaveConfig(c, description, target)
	})
}
func (s *Service) Reload() error { return s.edit(func() error { return s.store.Reload() }) }

func (s *Service) AddChild(parent, title, text, handling string) (string, error) {
	var id string
	err := s.edit(func() error { var err error; id, err = s.store.AddChild(parent, title, text, handling); return err })
	return id, err
}
func (s *Service) AddEntry(title, kind, text string) (string, error) {
	var id string
	err := s.edit(func() error { var err error; id, err = s.store.AddEntry(title, kind, text); return err })
	return id, err
}

// PrepareExternalEdit validates the source and establishes the editing pause.
// Launching an editor is an interface-specific responsibility.
func (s *Service) PrepareExternalEdit(id, field string) (string, error) {
	var path string
	err := s.edit(func() error {
		if err := s.editableField(id, field, true); err != nil {
			return err
		}
		var err error
		path, err = s.store.PrepareExternalEdit(id, field)
		return err
	})
	return path, err
}

func (s *Service) Invalidate(sel Selection) error {
	return s.edit(func() error {
		ids, err := s.passages(sel)
		if err != nil {
			return err
		}
		return s.store.Invalidate(ids)
	})
}
func (s *Service) InvalidateRun(id string) error {
	return s.edit(func() error {
		r, err := s.store.LoadRun(id)
		if err != nil {
			return err
		}
		s.store.State.InvalidRuns[id] = true
		if r.Kind == "prose" {
			if err = s.store.Invalidate([]string{r.Target}); err != nil {
				return err
			}
		}
		return s.store.SaveState()
	})
}

func (s *Service) Decide(id, choice string, selected []string) error {
	return s.edit(func() error {
		for index, c := range s.store.State.Changes {
			if c.ID == id {
				return s.decide(index, choice, selected)
			}
		}
		return fmt.Errorf("change is no longer pending")
	})
}
func (s *Service) DecideAll(choice string) error {
	return s.edit(func() error {
		if choice != "keep" && choice != "story" {
			return fmt.Errorf("choose keep or story")
		}
		for len(s.store.State.Changes) > 0 {
			if err := s.decide(0, choice, nil); err != nil {
				return err
			}
		}
		return nil
	})
}
func (s *Service) decide(index int, choice string, selected []string) error {
	c := s.store.State.Changes[index]
	var ids []string
	var err error
	switch choice {
	case "keep":
	case "selected", "story":
		ids, err = s.passages(Selection{Scope: choice, IDs: selected})
	case "branch":
		if s.store.Config.Nodes[c.Target] != nil {
			ids = s.store.Config.Leaves(c.Target)
		} else {
			for _, id := range s.store.Config.Leaves(s.store.Config.Root) {
				for _, ref := range s.store.Config.Required(id) {
					if ref == c.Target {
						ids = append(ids, id)
						break
					}
				}
			}
		}
	default:
		return fmt.Errorf("unknown invalidation choice")
	}
	if err != nil {
		return err
	}
	return s.store.Decide(index, choice, ids)
}

// Candidate/proposal commands reload persisted records by ID; callers cannot
// manufacture a Run payload or bypass its recorded scope and invalidation state.
func (s *Service) UseCandidate(id string, index int) error {
	return s.edit(func() error {
		r, err := s.store.LoadRun(id)
		if err != nil {
			return err
		}
		if r.Kind != "prose" {
			return fmt.Errorf("this run has no prose candidates")
		}
		return s.store.UseCandidate(r, index, true)
	})
}
func (s *Service) ImportOutline(id string) error {
	return s.edit(func() error {
		r, err := s.store.LoadRun(id)
		if err != nil {
			return err
		}
		if s.store.State.InvalidRuns[id] {
			return fmt.Errorf("this proposal is invalidated")
		}
		if r.Kind != "outline" || r.Text == "" {
			return fmt.Errorf("no outline proposal")
		}
		current, err := s.store.Read(r.Target, "outline")
		if err != nil {
			return err
		}
		return s.store.SaveText(r.Target, "outline", current+"\n\n"+r.Text, current)
	})
}
func (s *Service) ApplyEdits(id string) error {
	return s.edit(func() error {
		r, err := s.store.LoadRun(id)
		if err != nil {
			return err
		}
		if s.store.State.InvalidRuns[id] {
			return fmt.Errorf("this run is invalidated")
		}
		if r.Kind != "edit" || len(r.Edits) == 0 {
			return fmt.Errorf("no source edit proposals")
		}
		hash, err := s.store.Fingerprint()
		if err != nil {
			return err
		}
		if hash != r.Fingerprint {
			return fmt.Errorf("sources changed since this proposal; start a new turn using current files")
		}
		// Validate the entire proposal before the first write. Filesystem writes
		// are individually atomic, not a crash-atomic multi-file transaction.
		old := make([]string, len(r.Edits))
		seen := make(map[string]bool)
		for i, edit := range r.Edits {
			if !s.store.Config.Contains(r.Target, edit.ID) {
				return fmt.Errorf("proposal is outside its write scope")
			}
			if err = s.editableField(edit.ID, edit.Field, false); err != nil {
				return err
			}
			key := edit.ID + "/" + edit.Field
			if seen[key] {
				return fmt.Errorf("proposal contains duplicate source edits")
			}
			seen[key] = true
			old[i], err = s.store.Read(edit.ID, edit.Field)
			if err != nil {
				return err
			}
		}
		for i, edit := range r.Edits {
			if err = s.store.SaveText(edit.ID, edit.Field, edit.Text, old[i]); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Service) validateConversation(c project.Conversation) error {
	if c.Mode != "advice" && c.Mode != "edit" {
		return fmt.Errorf("unknown conversation mode")
	}
	if s.store.Config.Nodes[c.Target] == nil && s.store.Config.Knowledge[c.Target] == nil {
		return fmt.Errorf("conversation scope no longer exists")
	}
	m, ok := s.store.Config.Models[c.Model]
	if !ok {
		return fmt.Errorf("unknown conversation model")
	}
	if !m.Tools {
		return fmt.Errorf("interactive research and editing require a model with tools")
	}
	return nil
}
func (s *Service) NewConversation(target, mode, model string) (project.Conversation, error) {
	var c project.Conversation
	err := s.edit(func() error {
		if model == "" {
			model = s.store.State.LastManualModel
			if _, ok := s.store.Config.Models[model]; !ok {
				model = s.store.Config.BaseModel
			}
		}
		if err := s.rememberModel(model); err != nil {
			return err
		}
		c = project.Conversation{ID: project.NewID(), Target: target, Mode: mode, Model: model}
		if err := s.validateConversation(c); err != nil {
			return err
		}
		if err := s.store.SaveConversation(c); err != nil {
			return err
		}
		s.store.State.LastManualModel = model
		return s.store.SaveState()
	})
	return c, err
}
func (s *Service) SetConversationModel(id, model string) error {
	return s.edit(func() error {
		c, err := s.store.LoadConversation(id)
		if err != nil {
			return err
		}
		if err := s.rememberModel(model); err != nil {
			return err
		}
		c.Model = model
		if err = s.validateConversation(c); err != nil {
			return err
		}
		if err = s.store.SaveConversation(c); err != nil {
			return err
		}
		s.store.State.LastManualModel = model
		return s.store.SaveState()
	})
}

// SaveDraft changes only the draft; a stale UI copy cannot overwrite completed
// turns, scope, or model. It is safe while a conversation turn is running.
func (s *Service) SaveDraft(id, draft string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.available(); err != nil {
		return err
	}
	c, err := s.store.LoadConversation(id)
	if err != nil {
		return err
	}
	c.Draft = draft
	if err = s.store.SaveConversation(c); err != nil {
		return err
	}
	s.changed()
	return nil
}
