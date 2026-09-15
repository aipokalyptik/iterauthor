package model

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"
)

// enrichCatalog only reads server metadata. It never loads models or runs them.
func (h *HTTP) enrichCatalog(ctx context.Context, root, key string, catalog *Catalog, models map[string]AvailableModel) {
	if catalog.Provider == "Ollama" {
		sem := make(chan struct{}, 4)
		var wg sync.WaitGroup
		var mu sync.Mutex
		jobs := make([]AvailableModel, 0, len(models))
		for _, m := range models {
			jobs = append(jobs, m)
		}
		for _, m := range jobs {
			id := m.ID
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				wg.Wait()
				return
			}
			wg.Add(1)
			go func(id string, m AvailableModel) {
				defer wg.Done()
				defer func() { <-sem }()
				sub, cancel := context.WithTimeout(ctx, 3*time.Second)
				defer cancel()
				data, err := h.metadata(sub, root+"/api/show", key, map[string]string{"model": id})
				if err != nil {
					return
				}
				data["id"], _ = json.Marshal(id)
				details := parseAvailable(data)
				m.Tools, m.Reasoning = details.Tools, details.Reasoning
				var caps []string
				_ = json.Unmarshal(data["capabilities"], &caps)
				for _, cap := range caps {
					if cap == "embedding" {
						m.Kind = "embedding"
					}
					if cap == "completion" {
						m.Kind = "llm"
					}
				}
				var info map[string]json.RawMessage
				_ = json.Unmarshal(data["model_info"], &info)
				for name, value := range info {
					if strings.HasSuffix(name, ".context_length") {
						_ = json.Unmarshal(value, &m.MaxContext)
					}
				}
				// /show reports the model maximum, not the server's loaded context.
				mu.Lock()
				models[id] = m
				mu.Unlock()
			}(id, m)
		}
		wg.Wait()
		sub, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		data, err := h.metadata(sub, root+"/api/ps", key, nil)
		if err == nil {
			var rows []struct {
				Name    string `json:"name"`
				Model   string `json:"model"`
				Context int    `json:"context_length"`
			}
			if json.Unmarshal(data["models"], &rows) == nil {
				for _, row := range rows {
					id := row.Model
					if id == "" {
						id = row.Name
					}
					if m, ok := models[id]; ok {
						yes := true
						m.Loaded = &yes
						m.Context = row.Context
						m.ContextSource = "loaded"
						models[id] = m
					}
				}
			}
		}
		return
	}
	if catalog.Provider != "Compatible API" {
		return
	}
	sub, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	data, err := h.metadata(sub, root+"/props", key, nil)
	if err != nil {
		return
	}
	var settings struct {
		Context int `json:"n_ctx"`
	}
	if json.Unmarshal(data["default_generation_settings"], &settings) != nil || settings.Context <= 0 {
		return
	}
	catalog.Provider = "llama.cpp"
	for id, m := range models {
		m.Context = settings.Context
		m.ContextSource = "loaded"
		models[id] = m
	}
}

// WritingModel excludes explicitly typed non-chat models. Unknown metadata is
// kept visible so compatible APIs don't depend on model-name guessing.
func WritingModel(m AvailableModel) bool {
	switch strings.ToLower(m.Kind) {
	case "embedding", "embeddings", "image", "audio", "reranker", "rerank", "transcription", "tts":
		return false
	}
	return true
}
