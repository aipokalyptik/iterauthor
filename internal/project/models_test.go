package project

import (
	"context"
	"testing"
)

func TestInferenceInheritanceAndUnlimited(t *testing.T) {
	cap := 8000
	zero := 0
	c := Config{Limits: DefaultLimits(), Models: map[string]Model{"writer": {OutputTokens: &cap, Reasoning: "low"}}, Inference: map[string]Inference{"prose": {Reasoning: "medium"}}, Nodes: map[string]*Node{"root": {ID: "root", Inference: map[string]Inference{"prose": {OutputTokens: &zero, Reasoning: "high"}}}, "leaf": {ID: "leaf", Parent: "root", Inference: map[string]Inference{"prose": {Reasoning: "default"}}}}}
	got := c.InferenceFor("leaf", "prose", "writer")
	if *got.OutputTokens != 0 || got.Reasoning != "default" {
		t.Fatalf("inherited settings: %+v", got)
	}
	got = c.InferenceFor("leaf", "style", "writer")
	if *got.OutputTokens != cap || got.Reasoning != "low" {
		t.Fatalf("model defaults: %+v", got)
	}
	ctx, cancel := WorkContext(context.Background(), 0)
	defer cancel()
	if _, ok := ctx.Deadline(); ok {
		t.Fatal("unlimited time has a deadline")
	}
	cancel()
	if ctx.Err() == nil {
		t.Fatal("unlimited work is not cancelable")
	}
}

func TestLegacyConnectionsPreserveAssignments(t *testing.T) {
	c := Config{Models: map[string]Model{"base": {Name: "Base", URL: "http://local/v1", Model: "alpha", KeyEnv: "FIRST"}, "other": {Name: "Other", URL: "http://local/v1", Model: "beta", KeyEnv: "FIRST"}, "account": {Name: "Account", URL: "http://local/v1", Model: "alpha", KeyEnv: "SECOND"}}, BaseModel: "base"}
	migrated := c.WithConnections()
	if len(migrated.Connections) != 2 || migrated.Models["base"].Connection != migrated.Models["other"].Connection || migrated.Models["base"].Connection == migrated.Models["account"].Connection || migrated.BaseModel != "base" {
		t.Fatalf("bad legacy migration: %+v", migrated)
	}
	if c.Models["base"].Connection != "" {
		t.Fatal("view migration mutated source config")
	}
	again := migrated.WithConnections()
	if len(again.Connections) != 2 {
		t.Fatal("migration not idempotent")
	}
}
