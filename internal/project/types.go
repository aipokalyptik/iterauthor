package project

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

const Schema = 1

var Roles = []string{"knowledge", "outline-context", "outline", "prose", "consistency", "style"}

var RoleNames = map[string]string{
	"knowledge": "Knowledge selection", "outline-context": "Outline selection", "outline": "Outline expansion",
	"prose": "Prose writing/revision", "consistency": "Consistency review", "style": "Style review",
}

type Model struct {
	Connection       string   `json:"connection,omitempty"`
	Provider         string   `json:"provider,omitempty"`
	Reasoning        string   `json:"reasoning,omitempty"`
	ReasoningField   string   `json:"reasoning_field,omitempty"`
	OutputTokens     *int     `json:"output_tokens,omitempty"`
	Context          int      `json:"context,omitempty"`
	ContextSource    string   `json:"context_source,omitempty"`
	MaxContext       int      `json:"max_context,omitempty"`
	ToolsOverride    *bool    `json:"tools_override,omitempty"`
	ReasoningOptions []string `json:"reasoning_options,omitempty"`
	ToolsUnverified  bool     `json:"tools_unverified,omitempty"`
	Name             string   `json:"name"`
	URL              string   `json:"url"`
	Model            string   `json:"model"`
	KeyEnv           string   `json:"key_env,omitempty"`
	Tools            bool     `json:"tools"`
	TokenField       string   `json:"token_field,omitempty"`
}

type Limits struct {
	Drafts       int `json:"drafts"`
	Calls        int `json:"calls"`
	OutputTokens int `json:"output_tokens"`
	ContextChars int `json:"context_chars"`
	Minutes      int `json:"minutes"`
}

func DefaultLimits() Limits {
	return Limits{Drafts: 3, Calls: 24, OutputTokens: -1, ContextChars: 60000, Minutes: 20}
}

type Attachment struct {
	ID          string `json:"id"`
	Descendants bool   `json:"descendants,omitempty"`
}

type Node struct {
	Inference     map[string]Inference `json:"inference,omitempty"`
	ID            string               `json:"id"`
	Title         string               `json:"title"`
	Parent        string               `json:"parent,omitempty"`
	Children      []string             `json:"children,omitempty"`
	Models        map[string]string    `json:"models,omitempty"`
	Prompts       map[string]string    `json:"prompts,omitempty"`
	AutoKnowledge *bool                `json:"auto_knowledge,omitempty"`
	AutoOutline   *bool                `json:"auto_outline,omitempty"`
	Attachments   []Attachment         `json:"attachments,omitempty"`
	ReplaceStyle  bool                 `json:"replace_style,omitempty"`
}

type Entry struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Kind  string `json:"kind"`
}

type Config struct {
	Connections map[string]Connection `json:"connections,omitempty"`
	Inference   map[string]Inference  `json:"feature_inference,omitempty"`
	Schema      int                   `json:"schema"`
	Title       string                `json:"title"`
	Root        string                `json:"root"`
	Nodes       map[string]*Node      `json:"nodes"`
	Knowledge   map[string]*Entry     `json:"knowledge"`
	Models      map[string]Model      `json:"models"`
	BaseModel   string                `json:"base_model"`
	Defaults    map[string]string     `json:"feature_models,omitempty"`
	Limits      Limits                `json:"limits"`
	// Deprecated: kept for old project files; it never starts generation.
	AutoGenerate bool `json:"generate_after_editing"`
}

type Change struct {
	ID          string `json:"id"`
	Target      string `json:"target"`
	Description string `json:"description"`
	Decision    string `json:"decision,omitempty"`
	At          string `json:"at"`
}
type Passage struct {
	Status    string `json:"status"`
	ActiveRun string `json:"active_run,omitempty"`
	Candidate string `json:"candidate,omitempty"`
}
type State struct {
	Changes         []Change           `json:"changes,omitempty"`
	Decisions       []Change           `json:"decisions,omitempty"`
	Passages        map[string]Passage `json:"passages"`
	LastManualModel string             `json:"last_manual_model,omitempty"`
	InvalidRuns     map[string]bool    `json:"invalid_runs,omitempty"`
}

type Source struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Kind  string `json:"kind"`
	Text  string `json:"text"`
}

// Snapshot deliberately contains no private Notes. Workers only receive snapshots.
type Snapshot struct {
	Config      Config            `json:"config"`
	Sources     map[string]Source `json:"sources"`
	Styles      map[string]string `json:"styles"`
	Prose       map[string]string `json:"prose"`
	Fingerprint string            `json:"fingerprint"`
}

func NewID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func Now() string { return time.Now().UTC().Format(time.RFC3339Nano) }
func (c Config) TitleOf(id string) string {
	if n := c.Nodes[id]; n != nil {
		return n.Title
	}
	if e := c.Knowledge[id]; e != nil {
		return e.Title
	}
	return id
}
func (c Config) Ancestors(id string) []string {
	var ids []string
	seen := map[string]bool{}
	for n := c.Nodes[id]; n != nil && !seen[n.ID]; n = c.Nodes[n.Parent] {
		seen[n.ID] = true
		ids = append([]string{n.ID}, ids...)
	}
	return ids
}
func (c Config) Leaves(id string) []string {
	n := c.Nodes[id]
	if n == nil {
		return nil
	}
	if len(n.Children) == 0 {
		return []string{id}
	}
	var ids []string
	for _, child := range n.Children {
		ids = append(ids, c.Leaves(child)...)
	}
	return ids
}
func (c Config) Contains(parent, id string) bool {
	for _, a := range c.Ancestors(id) {
		if a == parent {
			return true
		}
	}
	return parent == id
}
func (c Config) ResolveModel(id, role string) (string, string) {
	chain := c.Ancestors(id)
	for i := len(chain) - 1; i >= 0; i-- {
		n := c.Nodes[chain[i]]
		if m := n.Models[role]; m != "" {
			return m, n.Title
		}
	}
	if m := c.Defaults[role]; m != "" {
		return m, "feature default"
	}
	return c.BaseModel, "base model"
}
func (c Config) Automatic(id, kind string) (bool, string) {
	chain := c.Ancestors(id)
	for i := len(chain) - 1; i >= 0; i-- {
		n := c.Nodes[chain[i]]
		v := n.AutoKnowledge
		if kind == "outline" {
			v = n.AutoOutline
		}
		if v != nil {
			return *v, n.Title
		}
	}
	return true, "story default"
}
func (s Snapshot) Style(id string) string {
	var parts []string
	for _, a := range s.Config.Ancestors(id) {
		if s.Config.Nodes[a].ReplaceStyle {
			parts = nil
		}
		if v := strings.TrimSpace(s.Styles[a]); v != "" {
			parts = append(parts, s.Config.TitleOf(a)+":\n"+v)
		}
	}
	return strings.Join(parts, "\n\n")
}
func (c Config) Prompt(id, role string) string {
	var parts []string
	for _, a := range c.Ancestors(id) {
		if p := c.Nodes[a].Prompts[role]; p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, "\n")
}
func (c Config) Required(id string) []string {
	seen := map[string]bool{}
	var ids []string
	add := func(id string) {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	var walk func(string)
	walk = func(id string) {
		add(id)
		if n := c.Nodes[id]; n != nil {
			for _, child := range n.Children {
				walk(child)
			}
		}
	}
	for _, a := range c.Ancestors(id) {
		for _, ref := range c.Nodes[a].Attachments {
			if ref.Descendants {
				walk(ref.ID)
			} else {
				add(ref.ID)
			}
		}
	}
	return ids
}
func (c Config) ModelIDs() []string {
	var ids []string
	for id := range c.Models {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
func (c Config) Validate() error {
	if c.Schema != Schema {
		return fmt.Errorf("unsupported project schema %d", c.Schema)
	}
	if c.Nodes[c.Root] == nil {
		return fmt.Errorf("missing root outline")
	}
	seen := map[string]bool{}
	var visit func(string, string) error
	visit = func(id, parent string) error {
		n := c.Nodes[id]
		if n == nil {
			return fmt.Errorf("missing outline %s", id)
		}
		if !ValidID(id) || n.ID != id || seen[id] {
			return fmt.Errorf("invalid or repeated outline identity %q", id)
		}
		if n.Parent != parent {
			return fmt.Errorf("parent mismatch for %s", id)
		}
		seen[id] = true
		for _, child := range n.Children {
			if err := visit(child, id); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visit(c.Root, ""); err != nil {
		return err
	}
	if len(seen) != len(c.Nodes) {
		return fmt.Errorf("orphaned outline nodes")
	}
	for id, e := range c.Knowledge {
		if !ValidID(id) || e == nil || e.ID != id || seen[id] {
			return fmt.Errorf("invalid knowledge identity %q", id)
		}
	}
	for _, n := range c.Nodes {
		for _, a := range n.Attachments {
			if c.Nodes[a.ID] == nil && c.Knowledge[a.ID] == nil {
				return fmt.Errorf("%s references missing item %s", n.Title, a.ID)
			}
		}
		for role, m := range n.Models {
			if m != "" {
				if _, ok := c.Models[m]; !ok {
					return fmt.Errorf("%s: unknown %s model %s", n.Title, role, m)
				}
				if RequiresTools(role) && !c.Models[m].Tools {
					return fmt.Errorf("%s requires tools", RoleNames[role])
				}
			}
		}
	}
	if _, ok := c.Models[c.BaseModel]; !ok {
		return fmt.Errorf("base model is missing")
	}
	if !c.Models[c.BaseModel].Tools {
		return fmt.Errorf("the base model must support tools")
	}
	for role, m := range c.Defaults {
		if _, ok := c.Models[m]; !ok && m != "" {
			return fmt.Errorf("unknown feature model %s", m)
		}
		if m != "" && RequiresTools(role) && !c.Models[m].Tools {
			return fmt.Errorf("%s requires tools", RoleNames[role])
		}
	}
	for id, con := range c.Connections {
		if !ValidID(id) || con.URL == "" || con.Name == "" {
			return fmt.Errorf("invalid API connection %q", id)
		}
	}
	for id, m := range c.Models {
		if m.Connection != "" {
			if _, ok := c.Connections[m.Connection]; !ok {
				return fmt.Errorf("model %s references a missing API connection", id)
			}
		}
		if err := (Inference{OutputTokens: m.OutputTokens, Reasoning: m.Reasoning}).Validate(); err != nil {
			return err
		}
		switch m.ReasoningField {
		case "", "reasoning_effort", "chat_template_kwargs":
		default:
			return fmt.Errorf("unknown reasoning transport")
		}
	}
	for _, options := range c.Inference {
		if err := options.Validate(); err != nil {
			return err
		}
	}
	for _, node := range c.Nodes {
		for _, options := range node.Inference {
			if err := options.Validate(); err != nil {
				return err
			}
		}
	}
	l := c.Limits
	if l.Drafts < 1 || l.Drafts > 10 || l.Calls < 1 || l.Calls > 200 || !ValidOutputTokens(l.OutputTokens) || l.ContextChars < 1000 || l.ContextChars > 1000000 || l.Minutes < 0 || l.Minutes > 240 {
		return fmt.Errorf("invalid generation limits")
	}
	return nil
}
func ValidID(id string) bool {
	if id == "" || len(id) > 80 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

// RequiresTools expresses the capability contract for pipeline assignments.
func RequiresTools(role string) bool {
	return role == "knowledge" || role == "outline-context" || role == "consistency"
}
