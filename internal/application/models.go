package application

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/aipokalyptik/iterauthor/internal/model"
	"github.com/aipokalyptik/iterauthor/internal/project"
)

type Endpoint = model.Endpoint

type modelSetup interface {
	Discover(context.Context, model.Endpoint) (model.Catalog, error)
	Probe(context.Context, project.Model) (model.ProbeResult, error)
}

func (s *Service) DiscoverModels(ctx context.Context, endpoint model.Endpoint) (catalog model.Catalog, err error) {
	s.LogActivity("Refreshing the model list")
	defer func() {
		if err != nil {
			s.LogError("Model list refresh failed", err)
		} else {
			s.LogActivity("Model list refreshed", "models", len(catalog.Models))
		}
	}()
	s.mu.Lock()
	err = s.available()
	demo := s.demo
	s.mu.Unlock()
	if err != nil {
		return model.Catalog{}, err
	}
	if demo {
		yes := true
		return model.Catalog{URL: "http://127.0.0.1:1234/v1", Provider: "Demo", Models: []model.AvailableModel{{ID: "demo", Name: "Demo writing model", Tools: &yes, Context: 32768}}, Notice: "Demo mode: this list is synthetic and no server was contacted."}, nil
	}
	setup, ok := s.client.(modelSetup)
	if !ok {
		return model.Catalog{}, fmt.Errorf("this model client does not support discovery")
	}
	return setup.Discover(ctx, endpoint)
}

func (s *Service) ProbeModel(ctx context.Context, candidate project.Model) (model.ProbeResult, error) {
	s.mu.Lock()
	if err := s.idle(); err != nil {
		s.mu.Unlock()
		return model.ProbeResult{}, err
	}
	workCtx := s.start(Job{Kind: "connection test"}, "")
	s.progress = "Testing model connection"
	s.logWork("Testing model text and tool support")
	s.changed()
	s.mu.Unlock()
	probeCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(workCtx, cancel)
	defer func() {
		stop()
		cancel()
		s.mu.Lock()
		defer s.mu.Unlock()
		s.cancel()
		s.cancel = nil
		s.busy = false
		s.canceling = false
		s.workInfo.Finished = project.Now()
		close(s.done)
		s.changed()
	}()
	var result model.ProbeResult
	var err error
	if s.demo {
		candidate.Tools = true
		candidate.TokenField = "max_tokens"
		result = model.ProbeResult{Model: candidate, Text: true, Tools: true, Detail: "Demo connection: synthetic results; no server was contacted."}
	} else if setup, ok := s.client.(modelSetup); ok {
		result, err = setup.Probe(probeCtx, candidate)
	} else {
		err = fmt.Errorf("this model client does not support connection testing")
	}
	if workCtx.Err() != nil {
		err = workCtx.Err()
	} else if probeCtx.Err() != nil {
		err = probeCtx.Err()
	}
	s.mu.Lock()
	if err != nil {
		s.progress = "Connection test failed: " + err.Error()
		s.workInfo.Status = "Failed"
		s.logWorkAt(slog.LevelError, "Model connection test failed", err)
	} else {
		s.progress = result.Detail
		s.workInfo.Status = "Available"
		if !result.Tools {
			s.logWorkAt(slog.LevelError, "Model tool test failed", fmt.Errorf("tool exchange failed"))
		}
	}
	s.logWork("Connection test finished", "status", s.workInfo.Status)
	s.mu.Unlock()
	return result, err
}

// SaveModel keeps connection setup and base-model capability rules in the core.
func (s *Service) SaveModel(id string, m project.Model, base bool, expected string) (string, error) {
	err := s.edit(func() error {
		if configVersion(s.store.Config) != expected {
			return ErrStale
		}
		if m.Model == "" {
			return fmt.Errorf("choose a model")
		}
		if m.Name == "" {
			m.Name = m.Model
		}
		c := s.store.Config.WithConnections()
		if id == "" {
			id = project.NewID()
		}
		if !project.ValidID(id) {
			return fmt.Errorf("invalid model identity")
		}
		if m.Connection != "" {
			con, ok := c.Connections[m.Connection]
			if !ok {
				return fmt.Errorf("unknown API connection")
			}
			m.URL, m.KeyEnv = con.URL, con.KeyEnv
			if con.Provider != "" {
				m.Provider = con.Provider
			}
		} else {
			var err error
			m.URL, err = model.APIBase(m.URL)
			if err != nil {
				return err
			}
		}
		if current, ok := s.modelConfig().Models[id]; ok && current.Connection == m.Connection && current.Model == m.Model {
			m.Context, m.ContextSource, m.MaxContext = current.Context, current.ContextSource, current.MaxContext
			m.ReasoningOptions = append([]string(nil), current.ReasoningOptions...)
		}
		if m.ToolsOverride != nil {
			m.Tools = *m.ToolsOverride
			m.ToolsUnverified = false
		}
		if err := m.ValidateReasoning(m.Reasoning); err != nil {
			return err
		}
		c.Models[id] = m
		if base {
			c.BaseModel = id
		}
		return s.store.SaveConfig(c, "Changed model settings", c.Root)
	})
	return id, err
}
