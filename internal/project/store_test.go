package project

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T) *Store {
	t.Helper()
	s, err := Create(filepath.Join(t.TempDir(), "story"), "Test", true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}
func TestPrivateNotesAndParentConversion(t *testing.T) {
	s := fixture(t)
	if err := s.SaveText("visit", "notes", "PRIVATE-NOTE-MARKER", ""); err != nil {
		t.Fatal(err)
	}
	if len(s.State.Changes) != 0 {
		t.Fatal("private note changed generation inputs")
	}
	if err := s.SaveText("visit", "prose", "PREVIOUS-PROSE-MARKER", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddChild("visit", "Child", "A new brief", "note"); err != nil {
		t.Fatal(err)
	}
	notes, _ := s.Read("visit", "notes")
	if !strings.Contains(notes, "PREVIOUS-PROSE-MARKER") {
		t.Fatal("old prose not retained in notes")
	}
	snap, err := s.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(snap)
	for _, marker := range []string{"PRIVATE-NOTE-MARKER", "PREVIOUS-PROSE-MARKER"} {
		if strings.Contains(string(b), marker) {
			t.Fatalf("private content leaked into snapshot: %s", marker)
		}
	}
	if strings.Contains(s.Manuscript(), "PREVIOUS-PROSE-MARKER") {
		t.Fatal("parent still contributes prose")
	}
}
func TestExternalChangesAndInvalidReloadPreserveCurrentProject(t *testing.T) {
	s := fixture(t)
	old, _ := s.Read("visit", "outline")
	path, _ := s.Path("visit", "outline")
	if err := os.WriteFile(path, []byte("External source edit"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveText("visit", "outline", "Overwrite", old); err == nil {
		t.Fatal("saved over an external edit")
	}
	if err := s.Reload(); err != nil {
		t.Fatal(err)
	}
	if len(s.State.Changes) != 1 {
		t.Fatalf("expected one change, got %d", len(s.State.Changes))
	}
	c := s.Config.Clone()
	c.Nodes["visit"].Children = []string{"story"}
	if err := WriteJSON(filepath.Join(s.Dir, "project.json"), c); err != nil {
		t.Fatal(err)
	}
	if err := s.Reload(); err == nil {
		t.Fatal("accepted cyclic tree")
	}
	if len(s.Config.Nodes["visit"].Children) != 0 {
		t.Fatal("invalid reload mutated active metadata")
	}
}
func TestInheritanceAndRequiredContext(t *testing.T) {
	s := fixture(t)
	c := s.Config.Clone()
	off := false
	c.Nodes["story"].AutoKnowledge = &off
	c.Nodes["story"].Attachments = []Attachment{{ID: "mara"}}
	c.Models["writer"] = Model{Name: "Writer", Tools: false}
	c.Nodes["story"].Models = map[string]string{"prose": "writer"}
	if err := s.SaveConfig(c, "guidance", "story"); err != nil {
		t.Fatal(err)
	}
	ref, origin := c.ResolveModel("visit", "prose")
	if ref != "writer" || origin != c.Title {
		t.Fatalf("unexpected resolution %s %s", ref, origin)
	}
	on, _ := c.Automatic("visit", "knowledge")
	if on {
		t.Fatal("off failed to inherit")
	}
	on, _ = c.Automatic("visit", "outline")
	if !on {
		t.Fatal("knowledge toggle disabled outlines")
	}
	if len(c.Required("visit")) != 3 {
		t.Fatalf("required refs not inherited/deduplicated: %v", c.Required("visit"))
	}
}
func TestLockAndFailedOpenRelease(t *testing.T) {
	s := fixture(t)
	if second, err := Open(s.Dir); err == nil {
		second.Close()
		t.Fatal("second writer opened project")
	}
	s.Close()
	path := filepath.Join(s.Dir, "project.json")
	b, _ := os.ReadFile(path)
	if err := os.WriteFile(path, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(s.Dir); err == nil {
		t.Fatal("invalid project opened")
	}
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	next, err := Open(s.Dir)
	if err != nil {
		t.Fatal("failed open leaked lock:", err)
	}
	next.Close()
}
func TestAuthoredProseProtectedAndOldRunNotActivated(t *testing.T) {
	s := fixture(t)
	snap, _ := s.Snapshot()
	pass := Review{Pass: true}
	r := Run{ID: NewID(), Target: "visit", Kind: "prose", Status: "Available", Fingerprint: snap.Fingerprint, Candidates: []Candidate{{Text: "Candidate", Consistency: &pass, Style: &pass}}}
	if err := s.SaveText("visit", "prose", "Author version", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.OfferRun(r, true); err != nil {
		t.Fatal(err)
	}
	text, _ := s.Read("visit", "prose")
	if text != "Author version" {
		t.Fatal("authored prose overwritten")
	}
	s.State.Passages["visit"] = Passage{Status: "Invalidated"}
	if err := s.OfferRun(r, true); err != nil {
		t.Fatal(err)
	}
	text, _ = s.Read("visit", "prose")
	if text != "Author version" {
		t.Fatal("old fingerprint activated")
	}
	if err := s.UseCandidate(r, 0, true); err != nil {
		t.Fatal(err)
	}
	text, _ = s.Read("visit", "prose")
	if text != "Candidate" {
		t.Fatal("explicit candidate choice failed")
	}
}
func TestExternalChangesDiscoveredAfterRestart(t *testing.T) {
	s := fixture(t)
	path, _ := s.Path("visit", "outline")
	s.Close()
	if err := os.WriteFile(path, []byte("After exit"), 0600); err != nil {
		t.Fatal(err)
	}
	next, err := Open(s.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if len(next.State.Changes) != 1 {
		t.Fatal("external edit after exit not detected")
	}
}
