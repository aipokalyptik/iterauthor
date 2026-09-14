package project

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Review struct {
	Pass        bool     `json:"pass"`
	Issues      []string `json:"issues"`
	Suggestions []string `json:"suggestions,omitempty"`
}
type Candidate struct {
	Text        string  `json:"text"`
	Consistency *Review `json:"consistency,omitempty"`
	Style       *Review `json:"style,omitempty"`
}
type Trace struct {
	Stage    string `json:"stage"`
	Model    string `json:"model"`
	Request  any    `json:"request"`
	Response any    `json:"response,omitempty"`
	Tool     string `json:"tool,omitempty"`
}
type Edit struct {
	ID    string `json:"id"`
	Field string `json:"field"`
	Text  string `json:"text"`
}
type Run struct {
	ID          string      `json:"id"`
	Target      string      `json:"target"`
	Kind        string      `json:"kind"`
	Status      string      `json:"status"`
	Started     string      `json:"started"`
	Finished    string      `json:"finished,omitempty"`
	Fingerprint string      `json:"fingerprint"`
	Context     string      `json:"context,omitempty"`
	Candidates  []Candidate `json:"candidates,omitempty"`
	Text        string      `json:"text,omitempty"`
	Error       string      `json:"error,omitempty"`
	Calls       int         `json:"calls"`
	Tokens      int         `json:"reported_tokens"`
	Trace       []Trace     `json:"trace,omitempty"`
	Edits       []Edit      `json:"edits,omitempty"`
	Demo        bool        `json:"demo"`
}

func (s *Store) SaveRun(r Run) error {
	if !ValidID(r.ID) {
		return fmt.Errorf("invalid run identity")
	}
	return WriteJSON(filepath.Join(s.Dir, ".twriter", "runs", r.ID+".json"), r)
}
func (s *Store) LoadRun(id string) (Run, error) {
	var r Run
	if !ValidID(id) {
		return r, fmt.Errorf("invalid run identity")
	}
	err := readJSON(filepath.Join(s.Dir, ".twriter", "runs", id+".json"), &r)
	return r, err
}
func (s *Store) Runs() ([]Run, error) {
	files, err := os.ReadDir(filepath.Join(s.Dir, ".twriter", "runs"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var runs []Run
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".json") {
			r, e := s.LoadRun(strings.TrimSuffix(f.Name(), ".json"))
			if e != nil {
				return nil, e
			}
			runs = append(runs, r)
		}
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].Started > runs[j].Started })
	return runs, nil
}
func (s *Store) OfferRun(r Run, automatic bool) error {
	if r.Kind != "prose" || len(r.Candidates) == 0 {
		return nil
	}
	ps := s.State.Passages[r.Target]
	ps.Candidate = r.ID
	s.State.Passages[r.Target] = ps
	if automatic && r.Status == "Available" && s.Status(r.Target) != "Authored" && r.Fingerprint == generationFingerprint(s.Hashes) {
		return s.UseCandidate(r, len(r.Candidates)-1, false)
	}
	return s.SaveState()
}
func (s *Store) UseCandidate(r Run, index int, explicit bool) error {
	if s.State.InvalidRuns[r.ID] {
		return fmt.Errorf("this run has been invalidated")
	}
	if n := s.Config.Nodes[r.Target]; n == nil || len(n.Children) > 0 {
		return fmt.Errorf("candidate target must still be a leaf")
	}
	if index < 0 || index >= len(r.Candidates) {
		return fmt.Errorf("candidate not found")
	}
	if err := s.CheckUnchanged(); err != nil {
		return err
	}
	if !explicit && r.Fingerprint != generationFingerprint(s.Hashes) {
		return fmt.Errorf("generation inputs changed; candidate retained for review")
	}
	c := r.Candidates[index]
	passed := c.Consistency != nil && c.Consistency.Pass && c.Style != nil && c.Style.Pass
	if !explicit && !passed {
		return fmt.Errorf("candidate needs author review")
	}
	old, err := s.Read(r.Target, "prose")
	if err != nil {
		return err
	}
	if err = s.backup(r.Target, "prose", old); err != nil {
		return err
	}
	path, _ := s.Path(r.Target, "prose")
	if err = Atomic(path, []byte(c.Text)); err != nil {
		return err
	}
	status := "Available"
	if !passed {
		status = "Kept with findings"
	}
	s.State.Passages[r.Target] = Passage{Status: status, ActiveRun: r.ID, Candidate: r.ID}
	if err = s.SaveState(); err != nil {
		return err
	}
	return s.SyncInputs()
}

type Turn struct {
	Role string `json:"role"`
	Text string `json:"text"`
	Run  string `json:"run,omitempty"`
}
type Conversation struct {
	ID     string `json:"id"`
	Target string `json:"target"`
	Mode   string `json:"mode"`
	Model  string `json:"model"`
	Turns  []Turn `json:"turns"`
	Draft  string `json:"draft,omitempty"`
}

func (s *Store) SaveConversation(c Conversation) error {
	if !ValidID(c.ID) {
		return fmt.Errorf("invalid conversation")
	}
	return WriteJSON(filepath.Join(s.Dir, ".twriter", "conversations", c.ID+".json"), c)
}
func (s *Store) LoadConversation(id string) (Conversation, error) {
	var c Conversation
	if !ValidID(id) {
		return c, fmt.Errorf("invalid conversation")
	}
	err := readJSON(filepath.Join(s.Dir, ".twriter", "conversations", id+".json"), &c)
	return c, err
}
func (s *Store) Conversations() ([]Conversation, error) {
	files, err := os.ReadDir(filepath.Join(s.Dir, ".twriter", "conversations"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var list []Conversation
	for _, f := range files {
		if !strings.HasSuffix(f.Name(), ".json") {
			continue
		}
		var c Conversation
		if err = readJSON(filepath.Join(s.Dir, ".twriter", "conversations", f.Name()), &c); err != nil {
			return nil, err
		}
		list = append(list, c)
	}
	return list, nil
}
