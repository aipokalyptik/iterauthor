package application

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/aipokalyptik/iterauthor/internal/model"
	"github.com/aipokalyptik/iterauthor/internal/project"
)

// CatalogState is replaceable discovery metadata. It never changes author
// selections, source fingerprints, active snapshots, or configuration versions.
type CatalogState struct {
	Connection project.Connection `json:"connection"`
	Catalog    model.Catalog      `json:"catalog"`
	Checked    string             `json:"checked,omitempty"`
	Updated    string             `json:"updated,omitempty"`
	Error      string             `json:"error,omitempty"`
	Refreshing bool               `json:"refreshing"`
}

func (s *Service) loadCatalogs() {
	s.catalogs = map[string]CatalogState{}
	b, err := os.ReadFile(filepath.Join(s.store.Dir, ".twriter", "catalogs.json"))
	if err == nil {
		_ = json.Unmarshal(b, &s.catalogs)
	}
	if s.catalogs == nil {
		s.catalogs = map[string]CatalogState{}
	}
	for id, value := range s.catalogs {
		value.Refreshing = false
		s.catalogs[id] = value
	}
}

func (s *Service) catalogView() map[string]CatalogState {
	result := map[string]CatalogState{}
	c := s.store.Config.WithConnections()
	for id, state := range s.catalogs {
		if c.Connections[id] == state.Connection {
			result[id] = state
		}
	}
	b, _ := json.Marshal(result)
	_ = json.Unmarshal(b, &result)
	return result
}

func (s *Service) modelConfig() project.Config {
	c := s.store.Config.WithConnections()
	identities := map[string]string{}
	for _, id := range c.ModelIDs() {
		m := c.Models[id]
		key := m.Connection + "\x00" + m.Model
		if identities[key] == "" {
			identities[key] = id
		}
	}
	for connection, state := range s.catalogs {
		con, ok := c.Connections[connection]
		if !ok || con != state.Connection {
			continue
		}
		for _, found := range state.Catalog.Models {
			if !model.WritingModel(found) {
				continue
			}
			id := project.ModelID(connection, found.ID)
			if saved := identities[connection+"\x00"+found.ID]; saved != "" {
				id = saved
			}
			m, exists := c.Models[id]
			if !exists {
				m = project.Model{Name: found.Name, Model: found.ID, Connection: connection, Tools: found.Tools != nil && *found.Tools, ToolsUnverified: found.Tools == nil}
			}
			if m.ToolsOverride != nil {
				m.Tools = *m.ToolsOverride
				m.ToolsUnverified = false
			} else if found.Tools != nil {
				m.Tools = *found.Tools
				m.ToolsUnverified = false
			} else if m.ToolsUnverified {
				m.Tools = false
			}
			m.ContextSource, m.MaxContext = found.ContextSource, found.MaxContext
			m.Context, m.ReasoningOptions = found.Context, append([]string(nil), found.Reasoning...)
			if con.Provider == "" {
				m.Provider = state.Catalog.Provider
			}
			c.Models[id] = m
		}
	}
	for id := range c.Models {
		c.Models[id] = c.ConnectedModel(id)
	}
	return c
}

// rememberModel freezes a catalog selection in authored configuration before a
// conversation/job refers to it. Missing/offline models are never auto-replaced.
func (s *Service) rememberModel(id string) error {
	if id == "" {
		return nil
	}
	if _, exists := s.store.Config.Models[id]; exists {
		return nil
	}
	c := s.modelConfig()
	m, ok := c.Models[id]
	if !ok {
		return fmt.Errorf("model is no longer listed; refresh Models and choose again")
	}
	next := s.store.Config.WithConnections()
	next.Models[id] = m
	return s.store.SaveConfig(next, "Selected model from API", next.Root)
}

func (s *Service) SaveConnection(id string, con project.Connection, expected string) (string, error) {
	err := s.edit(func() error {
		if configVersion(s.store.Config) != expected {
			return ErrStale
		}
		var err error
		con.URL, err = model.APIBase(con.URL)
		if err != nil {
			return err
		}
		con.Name = strings.TrimSpace(con.Name)
		if con.Name == "" {
			return fmt.Errorf("name this API connection")
		}
		switch con.Provider {
		case "", "Compatible API", "LM Studio", "Ollama", "llama.cpp", "OpenAI":
		default:
			return fmt.Errorf("unknown API type")
		}
		c := s.store.Config.WithConnections()
		if id == "" {
			id = project.NewID()
		}
		if !project.ValidID(id) {
			return fmt.Errorf("invalid connection identity")
		}
		c.Connections[id] = con
		return s.store.SaveConfig(c, "Changed API connection", c.Root)
	})
	return id, err
}

func (s *Service) RefreshConnection(ctx context.Context, id string) error {
	s.mu.Lock()
	if err := s.available(); err != nil {
		s.mu.Unlock()
		return err
	}
	con, ok := s.store.Config.WithConnections().Connections[id]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("unknown API connection")
	}
	state := s.catalogs[id]
	if state.Refreshing && state.Connection == con {
		s.mu.Unlock()
		return nil
	}
	if state.Connection != con {
		state = CatalogState{Connection: con}
	}
	state.Refreshing = true
	s.catalogs[id] = state
	dir := s.store.Dir
	epoch := s.projectEpoch
	s.changed()
	s.mu.Unlock()
	catalog, err := s.DiscoverModels(ctx, model.Endpoint{URL: con.URL, KeyEnv: con.KeyEnv})
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing || s.projectEpoch != epoch || s.store.Dir != dir || s.store.Config.WithConnections().Connections[id] != con {
		return nil
	}
	state.Refreshing, state.Checked, state.Error = false, project.Now(), ""
	if err != nil {
		state.Error = err.Error()
	} else {
		state.Catalog, state.Updated = catalog, state.Checked
	}
	s.catalogs[id] = state
	if saveErr := project.WriteJSON(filepath.Join(dir, ".twriter", "catalogs.json"), s.catalogs); saveErr != nil {
		state.Error = "Could not cache model list: " + saveErr.Error()
		s.catalogs[id] = state
	}
	s.changed()
	return err
}

func (s *Service) RefreshConnections(ctx context.Context) {
	s.mu.Lock()
	c := s.store.Config.WithConnections()
	s.mu.Unlock()
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for id := range c.Connections {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return
		}
		wg.Add(1)
		go func(id string) { defer wg.Done(); defer func() { <-sem }(); _ = s.RefreshConnection(ctx, id) }(id)
	}
	wg.Wait()
}

// RunModelRefresh belongs to the application host, independent of browser tabs.
func (s *Service) RunModelRefresh(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		s.RefreshConnections(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) snapshot() (project.Snapshot, error) {
	snapshot, err := s.store.Snapshot()
	if err == nil {
		snapshot.Config = s.modelConfig()
	}
	return snapshot, err
}
