package application_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aipokalyptik/iterauthor/internal/application"
	"github.com/aipokalyptik/iterauthor/internal/model"
	"github.com/aipokalyptik/iterauthor/internal/project"
)

func fixture(t *testing.T, client model.Client) *application.Service {
	t.Helper()
	p, err := project.Create(filepath.Join(t.TempDir(), "story"), "Test", true)
	if err != nil {
		t.Fatal(err)
	}
	s := application.New(p, client, true)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := s.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})
	return s
}
func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func idle(t *testing.T, s *application.Service) {
	t.Helper()
	deadline := time.NewTimer(4 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for s.View().Busy {
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("worker did not finish within four seconds")
		}
	}
}

func TestUnattendedQueueCommitsAndFeedsFollowingPassage(t *testing.T) {
	for _, stalledObserver := range []bool{false, true} {
		t.Run(map[bool]string{false: "no interface", true: "unresponsive interface"}[stalledObserver], func(t *testing.T) {
			s := fixture(t, model.Demo{})
			if stalledObserver {
				_, unsubscribe := s.Subscribe()
				defer unsubscribe()
			}
			check(t, s.Generate(application.Selection{Scope: "story"}, false))
			idle(t, s)
			for _, id := range []string{"arrival", "visit"} {
				text, err := s.Read(id, "prose")
				check(t, err)
				if !strings.Contains(text, "DEMO") || s.Status(id) != "Available" {
					t.Fatalf("%s was not committed without a UI", id)
				}
			}
			runs, err := s.Runs()
			check(t, err)
			if len(runs) != 2 {
				t.Fatalf("got %d runs", len(runs))
			}
			for _, r := range runs {
				if r.Target == "visit" && !strings.Contains(r.Context, "synthetic passage") {
					t.Fatal("next leaf did not receive previously committed prose")
				}
			}
			path, err := s.Export()
			check(t, err)
			b, err := os.ReadFile(path)
			check(t, err)
			if strings.Count(string(b), "synthetic passage") != 2 {
				t.Fatal("export did not include both results")
			}
		})
	}
}

func TestQueueUsesOneCallBudgetAndPreservesAuthoredText(t *testing.T) {
	s := fixture(t, model.Demo{})
	check(t, s.SaveText("arrival", "prose", "My own prose", ""))
	v := s.View()
	// With selectors off, one passing draft consumes four calls: writing,
	// consistency plus its tool continuation, and style. One more call permits
	// the second draft, but cannot complete its reviews.
	off := false
	v.Config.Nodes[v.Config.Root].AutoKnowledge = &off
	v.Config.Nodes[v.Config.Root].AutoOutline = &off
	v.Config.Limits.Calls = 5
	check(t, s.SaveConfig(v.Config, "Queue budget", v.Config.Root, v.ConfigVersion))
	if err := s.Generate(application.Selection{Scope: "story"}, true); !errors.Is(err, application.ErrChanges) {
		t.Fatalf("pending changes bypassed: %v", err)
	}
	check(t, s.DecideAll("keep"))
	check(t, s.Generate(application.Selection{Scope: "selected", IDs: []string{"arrival", "arrival", "visit"}}, true))
	idle(t, s)
	runs, err := s.Runs()
	check(t, err)
	calls := 0
	for _, r := range runs {
		calls += r.Calls
	}
	if calls != 5 || len(runs) != 2 {
		t.Fatalf("queue exceeded/repeated budget: %d calls, %d runs", calls, len(runs))
	}
	text, err := s.Read("arrival", "prose")
	check(t, err)
	if text != "My own prose" || s.Status("arrival") != "Authored" {
		t.Fatal("authored text overwritten")
	}
	if s.Status("visit") == "Available" {
		t.Fatal("unreviewed second passage activated")
	}
	if len(s.View().Queue) != 0 || !strings.Contains(s.View().Progress, "budget exhausted") {
		t.Fatalf("queue did not report exhaustion: %+v", s.View())
	}
}

// The held client acknowledges cancellation but deliberately delays returning.
// This distinguishes cancellation requested from safe-to-edit/close.
type heldClient struct{ started, canceled, release chan struct{} }

func (c *heldClient) Complete(ctx context.Context, _ project.Model, _ []model.Message, _ []model.Tool, _ int) (model.Response, error) {
	close(c.started)
	<-ctx.Done()
	close(c.canceled)
	<-c.release
	return model.Response{}, ctx.Err()
}
func signal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("operation did not reach the expected state")
	}
}
func TestCancellationAndShutdownHoldTheWriterLock(t *testing.T) {
	c := &heldClient{make(chan struct{}), make(chan struct{}), make(chan struct{})}
	s := fixture(t, c)
	defer close(c.release)
	v := s.View()
	check(t, s.Generate(application.Selection{Scope: "story"}, false))
	signal(t, c.started)
	s.Cancel()
	signal(t, c.canceled)
	for name, command := range map[string]func() error{
		"edit":     func() error { return s.SaveText("visit", "outline", "replacement", "") },
		"metadata": func() error { return s.SaveConfig(v.Config, "edit", v.Config.Root, v.ConfigVersion) },
		"reload":   s.Reload,
		"switch":   func() error { return s.SwitchProject(filepath.Join(t.TempDir(), "new"), "Other", true) },
		"generate": func() error { return s.Generate(application.Selection{Scope: "story"}, true) },
	} {
		if err := command(); !errors.Is(err, application.ErrBusy) {
			t.Errorf("%s bypassed active worker: %v", name, err)
		}
	}
	if !s.View().Busy || !s.View().Canceling || len(s.View().Queue) != 0 {
		t.Fatal("incorrect canceling state")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	if err := s.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown did not wait for worker: %v", err)
	}
	if p, err := project.Open(v.Dir); err == nil {
		p.Close()
		t.Fatal("project lock released while worker could still write")
	}
	if err := s.BeginEditing(); !errors.Is(err, application.ErrClosed) {
		t.Fatalf("closing service accepted work: %v", err)
	}
}

func TestConversationCompletionPreservesDraftAndScopeWithoutUI(t *testing.T) {
	s := fixture(t, model.Demo{})
	first, err := s.NewConversation("visit", "edit", "")
	check(t, err)
	second, err := s.NewConversation("arrival", "advice", "")
	check(t, err)
	check(t, s.Send(first.ID, "Add a detail about the cup."))
	check(t, s.SaveDraft(first.ID, "My next thought"))
	idle(t, s)
	c, err := s.LoadConversation(first.ID)
	check(t, err)
	if len(c.Turns) != 2 || c.Target != "visit" || c.Draft != "My next thought" {
		t.Fatalf("conversation completion lost state: %+v", c)
	}
	other, err := s.LoadConversation(second.ID)
	check(t, err)
	if len(other.Turns) != 0 {
		t.Fatal("response went to a different conversation")
	}
	before, err := s.Read("visit", "outline")
	check(t, err)
	if strings.Contains(before, "DEMO") {
		t.Fatal("proposal applied before author acceptance")
	}
	check(t, s.ApplyEdits(c.Turns[1].Run))
	after, err := s.Read("visit", "outline")
	check(t, err)
	if !strings.Contains(after, "DEMO proposed outline") {
		t.Fatal("accepted proposal not applied")
	}
	if err := s.ApplyEdits(c.Turns[1].Run); err == nil {
		t.Fatal("stale proposal was reapplied")
	}
	versions, err := s.SourceHistory("visit")
	check(t, err)
	if len(versions) != 1 {
		t.Fatalf("expected source backup: %+v", versions)
	}
	old, err := s.ReadSourceVersion(versions[0].ID)
	check(t, err)
	if old != before {
		t.Fatal("backup did not preserve source")
	}
}

func TestSnapshotsAndStaleEditorsCannotMutateCoreState(t *testing.T) {
	s := fixture(t, model.Demo{})
	old := s.View()
	changed := s.View()
	changed.Config.Nodes["visit"].Title = "A different title"
	changed.State.LastManualModel = "invented"
	if s.View().Config.Nodes["visit"].Title == "A different title" || s.View().State.LastManualModel == "invented" {
		t.Fatal("View leaked mutable state")
	}
	check(t, s.SaveConfig(changed.Config, "Rename", "visit", changed.ConfigVersion))
	changed.Config.Nodes["visit"].Title = "Mutated after save"
	if s.View().Config.Nodes["visit"].Title != "A different title" {
		t.Fatal("SaveConfig retained caller's mutable map")
	}
	if err := s.SaveConfig(old.Config, "stale save", "visit", old.ConfigVersion); !errors.Is(err, application.ErrStale) {
		t.Fatalf("stale config accepted: %v", err)
	}
	text, err := s.Read("visit", "outline")
	check(t, err)
	check(t, s.SaveText("visit", "outline", "New source", text))
	if err := s.SaveText("visit", "outline", "Old editor", text); err == nil {
		t.Fatal("stale text accepted")
	}
	changes := s.View().State.Changes
	check(t, s.Decide(changes[0].ID, "keep", nil))
	check(t, s.Decide(changes[1].ID, "branch", nil))
	if err := s.Decide(changes[0].ID, "story", nil); err == nil {
		t.Fatal("stale decision applied to another change")
	}
	if s.Status("visit") != "Invalidated" || s.Status("arrival") != "Missing" {
		t.Fatal("branch invalidation escaped its scope")
	}
}

func TestCoreRejectsInvalidProposalBeforeAnyWrites(t *testing.T) {
	for _, field := range []string{"notes", "outline"} {
		t.Run(field, func(t *testing.T) {
			p, err := project.Create(filepath.Join(t.TempDir(), "story"), "Test", true)
			check(t, err)
			before, err := p.Read("visit", "outline")
			check(t, err)
			snapshot, err := p.Snapshot()
			check(t, err)
			bad := project.Edit{ID: "visit", Field: field, Text: "must not write"}
			if field == "outline" {
				bad.ID = "arrival"
			}
			run := project.Run{ID: project.NewID(), Target: "visit", Kind: "edit", Fingerprint: snapshot.Fingerprint,
				Edits: []project.Edit{{ID: "visit", Field: "outline", Text: "first edit"}, bad}}
			check(t, p.SaveRun(run))
			s := application.New(p, model.Demo{}, true)
			defer func() { check(t, s.Shutdown(context.Background())) }()
			if err := s.ApplyEdits(run.ID); err == nil {
				t.Fatal("unsafe proposal accepted")
			}
			after, err := s.Read("visit", "outline")
			check(t, err)
			if after != before {
				t.Fatal("wrote the first edit before validating all proposals")
			}
		})
	}
}

func TestEditingModeAutoGenerationAndModelCapabilities(t *testing.T) {
	s := fixture(t, model.Demo{})
	v := s.View()
	v.Config.Models["writer"] = project.Model{Name: "Writer", Tools: false}
	v.Config.Defaults = map[string]string{"prose": "writer"}
	v.Config.AutoGenerate = true
	check(t, s.SaveConfig(v.Config, "Enable automatic generation", v.Config.Root, v.ConfigVersion))
	v = s.View()
	v.Config.Defaults["consistency"] = "writer"
	if err := s.SaveConfig(v.Config, "Invalid reviewer", v.Config.Root, v.ConfigVersion); err == nil {
		t.Fatal("tool-free consistency model accepted")
	}
	if _, err := s.NewConversation("visit", "edit", "writer"); err == nil {
		t.Fatal("tool-free interactive editor accepted")
	}
	if err := s.FinishEditing(); !errors.Is(err, application.ErrChanges) {
		t.Fatalf("pending changes ignored: %v", err)
	}
	check(t, s.DecideAll("keep"))
	check(t, s.FinishEditing())
	idle(t, s)
	if s.View().Editing || s.Status("visit") != "Available" {
		t.Fatal("finish editing did not run automatic queue")
	}
}

func TestOutlineImportAndInvalidationUsePersistedRunIdentity(t *testing.T) {
	s := fixture(t, model.Demo{})
	v := s.View()
	v.Config.Models["outliner"] = project.Model{Name: "Text-only outliner", Tools: false}
	check(t, s.SaveConfig(v.Config, "Add outliner", v.Config.Root, v.ConfigVersion))
	check(t, s.Start(application.Job{Kind: "outline", Target: "visit", Model: "outliner", Prompt: "Add useful detail"}))
	idle(t, s)
	id := s.View().LastRun
	run, err := s.LoadRun(id)
	check(t, err)
	usedOverride := false
	for _, trace := range run.Trace {
		if trace.Stage == "outline" && trace.Model == "outliner" {
			usedOverride = true
		}
	}
	if !usedOverride {
		t.Fatal("outline operation ignored its explicit model choice")
	}
	run.Text = "Caller-modified proposal"
	check(t, s.ImportOutline(id))
	text, err := s.Read("visit", "outline")
	check(t, err)
	if !strings.Contains(text, "DEMO outline proposal") || strings.Contains(text, run.Text) {
		t.Fatal("import trusted client data instead of saved proposal")
	}
	check(t, s.InvalidateRun(id))
	if err := s.ImportOutline(id); err == nil {
		t.Fatal("invalidated outline imported")
	}
}

func TestProjectSwitchAndSubscriptionLifecycle(t *testing.T) {
	s := fixture(t, model.Demo{})
	v := s.View()
	if err := s.SwitchProject(filepath.Join(t.TempDir(), "missing"), "", false); err == nil {
		t.Fatal("missing project opened")
	}
	if s.View().Dir != v.Dir {
		t.Fatal("failed switch discarded current project")
	}
	check(t, s.SwitchProject(filepath.Join(t.TempDir(), "next"), "Next story", true))
	p, err := project.Open(v.Dir)
	check(t, err)
	p.Close()
	if s.View().Config.Title != "Next story" {
		t.Fatal("project did not switch")
	}
	ch, unsubscribe := s.Subscribe()
	unsubscribe()
	unsubscribe()
	for range ch {
	}
	ch, _ = s.Subscribe()
	check(t, s.Shutdown(context.Background()))
	for range ch {
	}
	if _, err := s.Read("story", "outline"); !errors.Is(err, application.ErrClosed) {
		t.Fatalf("closed project read: %v", err)
	}
}
