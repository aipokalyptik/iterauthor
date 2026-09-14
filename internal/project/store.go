package project

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/sys/unix"
)

type Store struct {
	Dir    string
	Config Config
	State  State
	Hashes map[string]string
	lock   *os.File
}

func Atomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".write-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
func WriteJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return Atomic(path, append(b, '\n'))
}
func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func Create(dir, title string, sample bool) (*Store, error) {
	if _, err := os.Stat(filepath.Join(dir, "project.json")); err == nil {
		return nil, fmt.Errorf("project already exists")
	}
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		return nil, fmt.Errorf("create projects in an empty directory")
	}
	if strings.TrimSpace(title) == "" {
		title = "Untitled story"
	}
	c := Config{Schema: Schema, Title: title, Root: "story", Nodes: map[string]*Node{"story": {ID: "story", Title: title}}, Knowledge: map[string]*Entry{}, BaseModel: "base", Models: map[string]Model{"base": {Name: "Base model", URL: "http://127.0.0.1:1234/v1", Model: "", Tools: true, TokenField: "max_tokens"}}, Limits: DefaultLimits()}
	texts := map[string]string{"outline/story/outline.md": "Describe your story here.\n", "outline/story/style.md": "Write concrete, deliberate prose. Follow the point of view and pacing in the outline.\n"}
	if sample {
		c.Title = "The Long Return"
		c.Nodes["story"].Title = c.Title
		c.Nodes["story"].Children = []string{"arrival", "visit"}
		c.Nodes["arrival"] = &Node{ID: "arrival", Title: "Arrival", Parent: "story"}
		c.Nodes["visit"] = &Node{ID: "visit", Title: "The kitchen scene", Parent: "story", Attachments: []Attachment{{ID: "mara"}, {ID: "cup"}, {ID: "arrival"}}}
		c.Knowledge["mara"] = &Entry{ID: "mara", Title: "Mara Venn", Kind: "Character"}
		c.Knowledge["cup"] = &Entry{ID: "cup", Title: "The chipped cup", Kind: "Object"}
		texts["outline/story/outline.md"] = "Mara returns home to investigate the letters her mother left behind. Each discovery changes what she believes about her brother Elias.\n"
		texts["outline/story/style.md"] = "Measured pacing. Restrained language. Close third person through Mara. Let gestures and dialogue carry tension.\n"
		texts["outline/arrival/outline.md"] = "Mara finds an opened, empty envelope in her mother's handwriting. She conceals it in her coat and resolves to ask Elias about it.\n"
		texts["outline/visit/outline.md"] = "Mara asks Elias about their mother's letters.\n\n- Elias denies having received them.\n- Mara recognizes the chipped cup on his table.\n- She conceals her reaction.\n- He notices her hesitation but mistakes it for grief.\n\nEnd before either person openly accuses the other.\n"
		texts["knowledge/mara/entry.md"] = "Mara Venn is thirty-four. Her left wrist never fully healed after a fall; she avoids putting weight on it. She trusts her memory of her mother more than Elias's account.\n"
		texts["knowledge/cup/entry.md"] = "A white cup with a thin blue line around the rim. Their mother kept the chipped edge turned toward the wall. Mara recognizes it during the kitchen scene.\n"
	}
	for file, text := range texts {
		if err := Atomic(filepath.Join(dir, file), []byte(text)); err != nil {
			return nil, err
		}
	}
	if err := WriteJSON(filepath.Join(dir, "project.json"), c); err != nil {
		return nil, err
	}
	return Open(dir)
}

func Open(dir string) (s *Store, err error) {
	dir, err = filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if _, err = os.Stat(filepath.Join(dir, "project.json")); err != nil {
		return nil, fmt.Errorf("open project: %w", err)
	}
	s = &Store{Dir: dir}
	if err = os.MkdirAll(filepath.Join(dir, ".twriter"), 0700); err != nil {
		return nil, err
	}
	s.lock, err = os.OpenFile(filepath.Join(dir, ".twriter", "project.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = unix.Flock(int(s.lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		s.lock.Close()
		return nil, fmt.Errorf("project is already open in another iterauthor process")
	}
	opened := s
	defer func() {
		if err != nil {
			opened.Close()
		}
	}()
	err = readJSON(filepath.Join(dir, ".twriter", "state.json"), &s.State)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if s.State.Passages == nil {
		s.State.Passages = map[string]Passage{}
	}
	if s.State.InvalidRuns == nil {
		s.State.InvalidRuns = map[string]bool{}
	}
	var known map[string]string
	if e := readJSON(filepath.Join(dir, ".twriter", "inputs.json"), &known); e != nil && !errors.Is(e, os.ErrNotExist) {
		return nil, e
	}
	s.Hashes = known
	if err = s.Reload(); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *Store) Close() {
	if s.lock != nil {
		_ = unix.Flock(int(s.lock.Fd()), unix.LOCK_UN)
		_ = s.lock.Close()
		s.lock = nil
	}
}

func (s *Store) Path(id, field string) (string, error) {
	if !ValidID(id) {
		return "", fmt.Errorf("invalid item identity")
	}
	var rel string
	if s.Config.Nodes[id] != nil {
		if field != "outline" && field != "style" && field != "notes" && field != "prose" {
			return "", fmt.Errorf("invalid outline field")
		}
		rel = filepath.Join("outline", id, field+".md")
	} else if s.Config.Knowledge[id] != nil {
		if field != "entry" && field != "notes" {
			return "", fmt.Errorf("invalid knowledge field")
		}
		rel = filepath.Join("knowledge", id, field+".md")
	} else {
		return "", fmt.Errorf("item no longer exists")
	}
	path := filepath.Join(s.Dir, rel)
	for p := path; p != s.Dir; p = filepath.Dir(p) {
		if info, err := os.Lstat(p); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("symlinked project content is not supported: %s", rel)
		}
	}
	return path, nil
}
func (s *Store) Read(id, field string) (string, error) {
	path, err := s.Path(id, field)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if len(b) > 2*1024*1024 {
		return "", fmt.Errorf("source file exceeds 2 MiB: %s", path)
	}
	return string(b), err
}
func (s *Store) SaveState() error {
	return WriteJSON(filepath.Join(s.Dir, ".twriter", "state.json"), s.State)
}
func (s *Store) addChange(id, description string) {
	s.State.Changes = append(s.State.Changes, Change{ID: NewID(), Target: id, Description: description, At: Now()})
}

// Source changes only need an invalidation decision when prose exists. Keep
// setup and planning changes in history without asking the author to revisit
// nonexistent output. Reload also reconciles pending changes from older builds.
func (s *Store) saveSourceChanges() error {
	if len(s.State.Changes) > 0 {
		hasProse, err := s.hasProseToReview()
		if err != nil {
			return err
		}
		if !hasProse {
			for _, change := range s.State.Changes {
				change.Decision = "no-prose"
				s.State.Decisions = append(s.State.Decisions, change)
			}
			s.State.Changes = nil
		}
	}
	return s.SaveState()
}

func (s *Store) hasProseToReview() (bool, error) {
	for _, id := range s.Config.Leaves(s.Config.Root) {
		text, err := s.Read(id, "prose")
		if err != nil {
			return false, err
		}
		if strings.TrimSpace(text) != "" {
			return true, nil
		}
	}
	// A failed or interrupted draft can retain candidates without active prose.
	// Outline proposals, context summaries and connection tests are not prose.
	runs, err := s.Runs()
	if err != nil {
		return false, err
	}
	for _, run := range runs {
		n := s.Config.Nodes[run.Target]
		if run.Kind != "prose" || s.State.InvalidRuns[run.ID] || n == nil || len(n.Children) > 0 {
			continue
		}
		for _, candidate := range run.Candidates {
			if strings.TrimSpace(candidate.Text) != "" {
				return true, nil
			}
		}
	}
	return false, nil
}

func (s *Store) inputHashes() (map[string]string, error) {
	h := map[string]string{}
	add := func(rel string, b []byte) {
		sum := sha256.Sum256(b)
		h[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
	}
	b, err := os.ReadFile(filepath.Join(s.Dir, "project.json"))
	if err != nil {
		return nil, err
	}
	add("project.json", b)
	h[writingConfigKey] = writingConfigHash(s.Config)
	for id := range s.Config.Nodes {
		for _, field := range []string{"outline", "style", "prose"} {
			t, e := s.Read(id, field)
			if e != nil {
				return nil, e
			}
			add(filepath.Join("outline", id, field+".md"), []byte(t))
		}
	}
	for id := range s.Config.Knowledge {
		t, e := s.Read(id, "entry")
		if e != nil {
			return nil, e
		}
		add(filepath.Join("knowledge", id, "entry.md"), []byte(t))
	}
	return h, nil
}
func fingerprint(h map[string]string) string {
	b, _ := json.Marshal(h)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func (s *Store) Fingerprint() (string, error) {
	if err := s.CheckUnchanged(); err != nil {
		return "", err
	}
	return generationFingerprint(s.Hashes), nil
}
func (s *Store) SyncInputs() error {
	h, err := s.inputHashes()
	if err != nil {
		return err
	}
	stampGeneration(h, s.Hashes)
	if err = WriteJSON(filepath.Join(s.Dir, ".twriter", "inputs.json"), h); err != nil {
		return err
	}
	s.Hashes = h
	return nil
}

func (s *Store) Reload() error {
	var c Config
	if err := readJSON(filepath.Join(s.Dir, "project.json"), &c); err != nil {
		return err
	}
	if err := c.Validate(); err != nil {
		return err
	}
	old := s.Config
	s.Config = c
	h, err := s.inputHashes()
	if err != nil {
		s.Config = old
		return err
	}
	if s.Hashes != nil {
		keys := map[string]bool{}
		for k := range h {
			keys[k] = true
		}
		for k := range s.Hashes {
			keys[k] = true
		}
		var ordered []string
		for k := range keys {
			ordered = append(ordered, k)
		}
		sort.Strings(ordered)
		for _, k := range ordered {
			if k == writingConfigKey || k == generationKey {
				continue
			}
			if h[k] == s.Hashes[k] {
				continue
			}
			id := c.Root
			parts := strings.Split(k, "/")
			if len(parts) == 3 {
				id = parts[1]
			}
			if k == "project.json" && s.Hashes[writingConfigKey] != "" && s.Hashes[writingConfigKey] == h[writingConfigKey] {
				s.State.Decisions = append(s.State.Decisions, Change{ID: NewID(), Target: id, Description: "Reloaded " + k, At: Now(), Decision: "future-only"})
			} else {
				s.addChange(id, "Reloaded "+k)
			}
			if strings.HasSuffix(k, "/prose.md") {
				ps := s.State.Passages[id]
				ps.Status = "Authored"
				s.State.Passages[id] = ps
			}
		}
	}
	if err = s.saveSourceChanges(); err != nil {
		return err
	}
	return s.SyncInputs()
}
func (s *Store) CheckUnchanged() error {
	h, err := s.inputHashes()
	if err != nil {
		return err
	}
	if fingerprint(fileHashes(h)) != fingerprint(fileHashes(s.Hashes)) {
		return fmt.Errorf("files changed outside iterauthor; Reload before saving or generating")
	}
	return nil
}
func (s *Store) backup(id, field, old string) error {
	return Atomic(filepath.Join(s.Dir, ".twriter", "history", NewID(), id+"-"+field+".md"), []byte(old))
}
func (s *Store) SaveText(id, field, text, expected string) error {
	if err := s.CheckUnchanged(); err != nil {
		return err
	}
	old, err := s.Read(id, field)
	if err != nil {
		return err
	}
	if old != expected {
		return fmt.Errorf("this file changed; reload before saving")
	}
	if old == text {
		return nil
	}
	if err = s.backup(id, field, old); err != nil {
		return err
	}
	path, err := s.Path(id, field)
	if err != nil {
		return err
	}
	if err = Atomic(path, []byte(text)); err != nil {
		return err
	}
	if field != "notes" {
		s.addChange(id, "Edited "+field)
	}
	if field == "prose" {
		ps := s.State.Passages[id]
		ps.Status = "Authored"
		s.State.Passages[id] = ps
	}
	if err = s.saveSourceChanges(); err != nil {
		return err
	}
	return s.SyncInputs()
}
func (s *Store) SaveConfig(c Config, description, target string) error {
	if err := s.CheckUnchanged(); err != nil {
		return err
	}
	if err := c.Validate(); err != nil {
		return err
	}
	old, _ := json.MarshalIndent(s.Config, "", "  ")
	next, _ := json.MarshalIndent(c, "", "  ")
	if bytes.Equal(old, next) {
		return nil
	}
	if err := s.backup("project", "json", string(old)); err != nil {
		return err
	}
	if err := WriteJSON(filepath.Join(s.Dir, "project.json"), c); err != nil {
		return err
	}
	if writingConfigHash(s.Config) == writingConfigHash(c) {
		s.State.Decisions = append(s.State.Decisions, Change{ID: NewID(), Target: target, Description: description, At: Now(), Decision: "future-only"})
	} else {
		s.addChange(target, description)
	}
	s.Config = c
	if err := s.saveSourceChanges(); err != nil {
		return err
	}
	return s.SyncInputs()
}
func (c Config) Clone() Config {
	b, _ := json.Marshal(c)
	var copy Config
	_ = json.Unmarshal(b, &copy)
	return copy
}
func (s *Store) Snapshot() (Snapshot, error) {
	if err := s.CheckUnchanged(); err != nil {
		return Snapshot{}, err
	}
	v := Snapshot{Config: s.Config.Clone(), Sources: map[string]Source{}, Styles: map[string]string{}, Prose: map[string]string{}, Fingerprint: generationFingerprint(s.Hashes)}
	for id, n := range s.Config.Nodes {
		t, err := s.Read(id, "outline")
		if err != nil {
			return v, err
		}
		v.Sources[id] = Source{ID: id, Title: n.Title, Kind: "outline", Text: t}
		v.Styles[id], err = s.Read(id, "style")
		if err != nil {
			return v, err
		}
		if len(n.Children) == 0 {
			v.Prose[id], err = s.Read(id, "prose")
			if err != nil {
				return v, err
			}
		}
	}
	for id, e := range s.Config.Knowledge {
		t, err := s.Read(id, "entry")
		if err != nil {
			return v, err
		}
		v.Sources[id] = Source{ID: id, Title: e.Title, Kind: "knowledge", Text: t}
	}
	return v, nil
}

// AddChild commits tree metadata last. Prose is only removed from active assembly
// after the new child is visible; the original is always kept in local history.
func (s *Store) AddChild(parent, title, text, handling string) (string, error) {
	if err := s.CheckUnchanged(); err != nil {
		return "", err
	}
	n := s.Config.Nodes[parent]
	if n == nil {
		return "", fmt.Errorf("select an outline parent")
	}
	prose, err := s.Read(parent, "prose")
	if err != nil {
		return "", err
	}
	if len(n.Children) == 0 && prose != "" && handling != "note" && handling != "outline" && handling != "discard" {
		return "", fmt.Errorf("choose what happens to the existing prose")
	}
	if strings.TrimSpace(title) == "" {
		return "", fmt.Errorf("title is required")
	}
	id := NewID()
	c := s.Config.Clone()
	c.Nodes[id] = &Node{ID: id, Title: title, Parent: parent}
	c.Nodes[parent].Children = append(c.Nodes[parent].Children, id)
	if err = Atomic(filepath.Join(s.Dir, "outline", id, "outline.md"), []byte(text)); err != nil {
		return "", err
	}
	if prose != "" && len(n.Children) == 0 {
		if err = s.backup(parent, "prose", prose); err != nil {
			return "", err
		}
		if handling == "note" || handling == "outline" {
			field := "notes"
			addition := prose
			if handling == "outline" {
				field = "outline"
				addition = "Existing passage retained as an outline reference for refinement:\n\n" + prose
			}
			old, e := s.Read(parent, field)
			if e != nil {
				return "", e
			}
			if e = s.backup(parent, field, old); e != nil {
				return "", e
			}
			path, _ := s.Path(parent, field)
			if e = Atomic(path, []byte(old+"\n\n"+addition)); e != nil {
				return "", e
			}
		}
	}
	// Source text changes above are part of this single operation, not external edits.
	if err = WriteJSON(filepath.Join(s.Dir, "project.json"), c); err != nil {
		return "", err
	}
	s.Config = c
	s.addChange(parent, "Added child "+title)
	s.State.Passages[id] = Passage{Status: "Missing"}
	if err = s.saveSourceChanges(); err != nil {
		return "", err
	}
	return id, s.SyncInputs()
}
func (s *Store) AddEntry(title, kind, text string) (string, error) {
	if strings.TrimSpace(title) == "" {
		return "", fmt.Errorf("title is required")
	}
	if err := s.CheckUnchanged(); err != nil {
		return "", err
	}
	id := NewID()
	c := s.Config.Clone()
	c.Knowledge[id] = &Entry{ID: id, Title: title, Kind: kind}
	if err := Atomic(filepath.Join(s.Dir, "knowledge", id, "entry.md"), []byte(text)); err != nil {
		return "", err
	}
	return id, s.SaveConfig(c, "Added knowledge entry", id)
}
func (s *Store) Status(id string) string {
	if v := s.State.Passages[id].Status; v != "" {
		return v
	}
	t, _ := s.Read(id, "prose")
	if t != "" {
		return "Authored"
	}
	return "Missing"
}
func (s *Store) Invalidate(ids []string) error {
	for _, id := range ids {
		ps := s.State.Passages[id]
		if s.Status(id) != "Authored" {
			ps.Status = "Invalidated"
		}
		s.State.Passages[id] = ps
	}
	return s.SaveState()
}
func (s *Store) Decide(index int, choice string, ids []string) error {
	if index < 0 || index >= len(s.State.Changes) {
		return fmt.Errorf("change no longer exists")
	}
	c := s.State.Changes[index]
	c.Decision = choice
	if choice == "keep" {
		ids = s.Config.Leaves(c.Target)
		if s.Config.Knowledge[c.Target] != nil {
			ids = s.Config.Leaves(s.Config.Root)
		}
		for _, id := range ids {
			ps := s.State.Passages[id]
			if ps.Status == "Available" {
				ps.Status = "Retained after changes"
				s.State.Passages[id] = ps
			}
		}
	} else {
		if err := s.Invalidate(ids); err != nil {
			return err
		}
	}
	s.State.Decisions = append(s.State.Decisions, c)
	s.State.Changes = append(s.State.Changes[:index], s.State.Changes[index+1:]...)
	return s.SaveState()
}
func (s *Store) Manuscript() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", s.Config.Title)
	for _, id := range s.Config.Leaves(s.Config.Root) {
		text, _ := s.Read(id, "prose")
		fmt.Fprintf(&b, "## %s\n\n", s.Config.TitleOf(id))
		if text == "" {
			text = "[Missing passage]"
		}
		if status := s.Status(id); status != "Available" && status != "Authored" {
			fmt.Fprintf(&b, "[%s]\n\n", status)
		}
		b.WriteString(text + "\n\n")
	}
	return b.String()
}
