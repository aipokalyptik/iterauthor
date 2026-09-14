package project

import (
	"path/filepath"
	"reflect"
	"testing"
)

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func currentFingerprint(t *testing.T, s *Store) string {
	t.Helper()
	f, err := s.Fingerprint()
	must(t, err)
	return f
}

func seedCandidate(t *testing.T, s *Store) Run {
	t.Helper()
	pass := &Review{Pass: true}
	r := Run{ID: NewID(), Target: "visit", Kind: "prose", Status: "Available", Fingerprint: currentFingerprint(t, s),
		Candidates: []Candidate{{Text: "A saved draft.", Consistency: pass, Style: pass}}}
	must(t, s.SaveRun(r))
	return r
}

func TestConfigurationChangesDistinguishWritingFromOperations(t *testing.T) {
	for _, tc := range []struct {
		name    string
		writing bool
		edit    func(*Config)
	}{
		{"connection address", false, func(c *Config) { m := c.Models["base"]; m.URL = "http://new-host:1234/v1"; c.Models["base"] = m }},
		{"connection label", false, func(c *Config) { m := c.Models["base"]; m.Name = "Renamed"; c.Models["base"] = m }},
		{"credential environment", false, func(c *Config) { m := c.Models["base"]; m.KeyEnv = "TEST_NEW_KEY"; c.Models["base"] = m }},
		{"token protocol", false, func(c *Config) { m := c.Models["base"]; m.TokenField = "max_completion_tokens"; c.Models["base"] = m }},
		{"unused connection", false, func(c *Config) { c.Models["unused"] = Model{Name: "Unused", Model: "different-model"} }},
		{"limits", false, func(c *Config) {
			c.Limits = Limits{Drafts: 2, Calls: 10, OutputTokens: 1024, ContextChars: 32000, Minutes: 10}
		}},
		{"automatic scheduling", false, func(c *Config) { c.AutoGenerate = true }},
		{"same effective default", false, func(c *Config) { c.Defaults = map[string]string{"prose": "base"} }},
		{"same effective override", false, func(c *Config) { c.Nodes["visit"].Models = map[string]string{"prose": "base"} }},
		{"same effective context toggle", false, func(c *Config) { on := true; c.Nodes["visit"].AutoKnowledge = &on }},
		{"active model identifier", true, func(c *Config) { m := c.Models["base"]; m.Model = "different-model"; c.Models["base"] = m }},
		{"base assignment", true, func(c *Config) { c.Models["new"] = Model{Tools: true}; c.BaseModel = "new" }},
		{"feature assignment", true, func(c *Config) { c.Models["writer"] = Model{}; c.Defaults = map[string]string{"prose": "writer"} }},
		{"local assignment", true, func(c *Config) {
			c.Models["writer"] = Model{}
			c.Nodes["visit"].Models = map[string]string{"prose": "writer"}
		}},
		{"prompt", true, func(c *Config) { c.Nodes["visit"].Prompts = map[string]string{"prose": "Keep the secret hidden."} }},
		{"knowledge selection", true, func(c *Config) { off := false; c.Nodes["story"].AutoKnowledge = &off }},
		{"outline selection", true, func(c *Config) { off := false; c.Nodes["story"].AutoOutline = &off }},
		{"context attachment", true, func(c *Config) { c.Nodes["arrival"].Attachments = []Attachment{{ID: "cup"}} }},
		{"style inheritance", true, func(c *Config) { c.Nodes["visit"].ReplaceStyle = true }},
		{"outline title", true, func(c *Config) { c.Nodes["visit"].Title = "A confrontation" }},
		{"knowledge metadata", true, func(c *Config) { c.Knowledge["cup"].Title = "A stolen cup" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := fixture(t)
			r := seedCandidate(t, s)
			before := currentFingerprint(t, s)
			c := s.Config.Clone()
			tc.edit(&c)
			must(t, s.SaveConfig(c, tc.name, "story"))
			if (len(s.State.Changes) == 1) != tc.writing || (before != currentFingerprint(t, s)) != tc.writing {
				t.Fatalf("wrong review/freshness classification: changes=%+v", s.State.Changes)
			}
			if tc.writing {
				if err := s.UseCandidate(r, 0, false); err == nil {
					t.Fatal("creative change allowed automatic use of an old candidate")
				}
			} else {
				if len(s.State.Decisions) != 1 || s.State.Decisions[0].Decision != "future-only" {
					t.Fatal("operational change was not recorded as future-only")
				}
				must(t, s.UseCandidate(r, 0, false))
			}
		})
	}
}

func TestPlanningNeedsReviewOnlyWhenProseExists(t *testing.T) {
	for _, kind := range []string{"none", "outline", "test", "edit", "empty-prose", "candidate", "invalidated", "authored"} {
		t.Run(kind, func(t *testing.T) {
			s := fixture(t)
			switch kind {
			case "candidate", "invalidated":
				r := seedCandidate(t, s)
				if kind == "invalidated" {
					s.State.InvalidRuns[r.ID] = true
				}
			case "authored":
				must(t, s.SaveText("visit", "prose", "My own prose.", ""))
				s.State.Changes = nil
			case "none":
			default:
				r := Run{ID: NewID(), Kind: kind, Target: "visit", Text: "A cached result"}
				if kind == "empty-prose" {
					r.Kind = "prose"
					r.Status = "Failed"
				}
				must(t, s.SaveRun(r))
			}
			before, err := s.Read("visit", "outline")
			must(t, err)
			must(t, s.SaveText("visit", "outline", "A new plan.", before))
			wantReview := kind == "candidate" || kind == "authored"
			if (len(s.State.Changes) == 1) != wantReview {
				t.Fatalf("wrong review requirement: %+v", s.State)
			}
			if !wantReview && (len(s.State.Decisions) != 1 || s.State.Decisions[0].Decision != "no-prose") {
				t.Fatal("planning change missing from history")
			}
		})
	}
}

func TestReopenResolvesOldSetupPromptsWithoutChangingProject(t *testing.T) {
	s := fixture(t)
	before := s.Config.Clone()
	manuscript := s.Manuscript()
	for _, description := range []string{"Changed model connection", "Changed model connection", "Changed feature models"} {
		s.addChange("story", description)
	}
	want := append([]Change(nil), s.State.Changes...)
	for i := range want {
		want[i].Decision = "no-prose"
	}
	must(t, s.SaveState())
	s.Close()
	for range 2 {
		next, err := Open(s.Dir)
		must(t, err)
		if len(next.State.Changes) != 0 || !reflect.DeepEqual(next.State.Decisions, want) || !reflect.DeepEqual(next.Config, before) {
			t.Fatal("reopen failed to resolve old prompts without altering history/configuration")
		}
		if next.Manuscript() != manuscript {
			t.Fatal("reopen created prose")
		}
		next.Close()
	}
}

func TestLegacyFingerprintSurvivesUpgradeAndOperationalEdits(t *testing.T) {
	s := fixture(t)
	legacy := fileHashes(s.Hashes)
	r := seedCandidate(t, s)
	r.Fingerprint = fingerprint(legacy)
	must(t, s.SaveRun(r))
	must(t, WriteJSON(filepath.Join(s.Dir, ".twriter", "inputs.json"), legacy))
	s.Close()
	next, err := Open(s.Dir)
	must(t, err)
	defer next.Close()
	c := next.Config.Clone()
	c.Limits.Calls++
	must(t, next.SaveConfig(c, "Call budget", "story"))
	if currentFingerprint(t, next) != r.Fingerprint || len(next.State.Changes) != 0 {
		t.Fatal("upgrade or operational edit made a legacy candidate stale")
	}
	must(t, next.UseCandidate(r, 0, false))
}

func TestExternalOperationalEditsStillRequireReload(t *testing.T) {
	s := fixture(t)
	seedCandidate(t, s)
	before := currentFingerprint(t, s)
	c := s.Config.Clone()
	c.Limits.Calls++
	must(t, WriteJSON(filepath.Join(s.Dir, "project.json"), c))
	if err := s.CheckUnchanged(); err == nil {
		t.Fatal("external operational edit bypassed reload protection")
	}
	must(t, s.Reload())
	if currentFingerprint(t, s) != before || len(s.State.Changes) != 0 || len(s.State.Decisions) != 1 {
		t.Fatal("external operational edit changed generation freshness or requested review")
	}
	c.Nodes["visit"].Title = "New scene title"
	must(t, WriteJSON(filepath.Join(s.Dir, "project.json"), c))
	must(t, s.Reload())
	if currentFingerprint(t, s) == before || len(s.State.Changes) != 1 {
		t.Fatal("external writing edit did not request review")
	}
}

func TestUnchangedConfigurationSaveDoesNotCreateDecisions(t *testing.T) {
	s := fixture(t)
	seedCandidate(t, s)
	must(t, s.SaveConfig(s.Config.Clone(), "No change", "story"))
	if len(s.State.Changes) != 0 || len(s.State.Decisions) != 0 {
		t.Fatal("no-op save created a decision")
	}
}
