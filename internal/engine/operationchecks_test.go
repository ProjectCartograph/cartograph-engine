package engine_test

import (
	"context"
	"testing"
)

func TestOperationChecksAdviseStepByStep(t *testing.T) {
	e := seededEngine(t)
	ctx := context.Background()
	mustCommit(t, e, "Operation", "svc", "p1", "apiVersion: cartograph/v1\nkind: Operation\nmetadata:\n  id: svc\n  name: Service\nspec:\n  name: Service\n  purpose: Keeps things running\n  team: t1\n")
	checks, err := e.OperationChecks(ctx, "svc")
	if err != nil {
		t.Fatal(err)
	}
	state := map[string]string{}
	for _, c := range checks {
		state[c.ID] = c.State
		if c.State == "block" {
			t.Fatalf("an operation's checks advise only, got a block: %+v", c)
		}
	}
	want := map[string]string{"service-purpose": "ok", "service-owner": "ok", "service-hours": "warn",
		"measures-kpis": "warn"}
	// A programme is optional, so an operation outside one gets no line.
	if s, ok := state["alignment-programmes"]; ok {
		t.Errorf("alignment-programmes = %q, want no check", s)
	}
	for id, s := range want {
		if state[id] != s {
			t.Errorf("%s = %q, want %q", id, state[id], s)
		}
	}
}
