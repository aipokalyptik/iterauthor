package project

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// These metadata keys share the atomic input-hash checkpoint with file hashes.
// Raw file hashes still detect every external edit. The generation identity
// changes only when writing inputs change, rather than on operational settings.
const writingConfigKey = "@writing-config"
const generationKey = "@generation"

func writingConfigHash(c Config) string {
	w := c.Clone()
	w.Limits = Limits{}
	w.Connections = nil
	w.Inference = nil
	w.AutoGenerate = false
	w.BaseModel = ""
	w.Defaults = nil
	w.Models = map[string]Model{}
	for id, n := range w.Nodes {
		n.Models = map[string]string{}
		n.Inference = nil
		for _, role := range Roles {
			modelID, _ := c.ResolveModel(id, role)
			n.Models[role] = modelID
			// Identity and assignment affect model selection. Connection addresses,
			// credentials, display names and protocol capabilities are operational.
			w.Models[modelID] = Model{Model: c.Models[modelID].Model}
		}
		knowledge, _ := c.Automatic(id, "knowledge")
		outline, _ := c.Automatic(id, "outline")
		n.AutoKnowledge, n.AutoOutline = &knowledge, &outline
	}
	b, _ := json.Marshal(w)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func fileHashes(h map[string]string) map[string]string {
	files := make(map[string]string, len(h))
	for path, hash := range h {
		if path != writingConfigKey && path != generationKey {
			files[path] = hash
		}
	}
	return files
}

func writingFingerprint(h map[string]string) string {
	inputs := fileHashes(h)
	if hash := h[writingConfigKey]; hash != "" {
		inputs["project.json"] = hash
	}
	return fingerprint(inputs)
}

func generationFingerprint(h map[string]string) string {
	if hash := h[generationKey]; hash != "" {
		return hash
	}
	return fingerprint(fileHashes(h))
}

func stampGeneration(next, previous map[string]string) {
	// Preserve an existing project's exact generation identity on upgrade when
	// its configuration has not changed. Existing cached proposals stay usable.
	if previous != nil && previous[writingConfigKey] == "" && previous["project.json"] == next["project.json"] {
		legacy := fileHashes(previous)
		legacy[generationKey] = generationFingerprint(previous)
		legacy[writingConfigKey] = next[writingConfigKey]
		previous = legacy
	}
	next[generationKey] = writingFingerprint(next)
	if previous != nil && writingFingerprint(next) == writingFingerprint(previous) {
		next[generationKey] = generationFingerprint(previous)
	}
}
