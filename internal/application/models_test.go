package application_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/aipokalyptik/iterauthor/internal/application"
	"github.com/aipokalyptik/iterauthor/internal/model"
	"github.com/aipokalyptik/iterauthor/internal/project"
)

type heldSetup struct {
	model.Demo
	started, canceled, release chan struct{}
}

func (c *heldSetup) Discover(context.Context, model.Endpoint) (model.Catalog, error) {
	return model.Catalog{}, nil
}
func (c *heldSetup) Probe(ctx context.Context, m project.Model) (model.ProbeResult, error) {
	close(c.started)
	<-ctx.Done()
	close(c.canceled)
	<-c.release
	return model.ProbeResult{Text: true, Model: m}, nil
}
func TestConnectionProbeUsesCoreCancellationInterlock(t *testing.T) {
	p, err := project.Create(filepath.Join(t.TempDir(), "story"), "Test", true)
	check(t, err)
	client := &heldSetup{started: make(chan struct{}), canceled: make(chan struct{}), release: make(chan struct{})}
	core := application.New(p, client, false)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		check(t, core.Shutdown(ctx))
	})
	finished := make(chan error, 1)
	go func() {
		_, err := core.ProbeModel(context.Background(), project.Model{Model: "writer"})
		finished <- err
	}()
	select {
	case <-client.started:
	case <-time.After(time.Second):
		t.Fatal("probe did not start")
	}
	core.Cancel()
	select {
	case <-client.canceled:
	case <-time.After(time.Second):
		t.Fatal("probe not canceled")
	}
	busy := core.View().Busy
	saveErr := core.SaveText("arrival", "notes", "should not save", "")
	close(client.release)
	if !busy || !errors.Is(saveErr, application.ErrBusy) {
		t.Fatal("probe unlocked source editing before completion")
	}
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled probe reported success: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("probe did not finish")
	}
	if core.View().Busy {
		t.Fatal("probe did not release interlock")
	}
	check(t, core.SaveText("arrival", "notes", "safe now", ""))
}
