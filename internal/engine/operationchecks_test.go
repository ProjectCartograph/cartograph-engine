package engine_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
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
	// The team runs it; the service owner is the role accountable for it,
	// and none is named yet.
	want := map[string]string{"service-purpose": "ok", "service-owner": "warn", "service-hours": "warn",
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

func TestAnOperationsServiceOwnerIsARole(t *testing.T) {
	e := seededEngine(t)
	ctx := context.Background()
	mustCommit(t, e, "Resource", "svc-lead", "p1", "apiVersion: cartograph/v1\nkind: Resource\nmetadata:\n  id: svc-lead\n  name: Service lead\nspec:\n  name: Service lead\n  category: personRole\n")
	mustCommit(t, e, "Operation", "svc", "p1", "apiVersion: cartograph/v1\nkind: Operation\nmetadata:\n  id: svc\n  name: Service\nspec:\n  name: Service\n  purpose: Keeps things running\n  team: t1\n  serviceOwner: svc-lead\n")
	checks, err := e.OperationChecks(ctx, "svc")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range checks {
		if c.ID == "service-owner" && c.State != "ok" {
			t.Fatalf("a named service owner: %+v", c)
		}
	}
}

// A service is planned before the project that sets it up names it, and
// running once in use (TAXONOMY.md D30): a planned one asks for that
// project; one without a status is running, as every older one is.
func TestAServiceIsPlannedUntilItRuns(t *testing.T) {
	e := seededEngine(t)
	ctx := context.Background()
	svc := func(id, status string) string {
		y := "apiVersion: cartograph/v1\nkind: Operation\nmetadata:\n  id: " + id + "\n  name: " + id + "\nspec:\n  purpose: Check deliveries\n  team: t1\n"
		if status != "" {
			y += "  status: " + status + "\n"
		}
		return y
	}
	state := func(id string) engine.ProgrammeCheck {
		t.Helper()
		checks, err := e.OperationChecks(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range checks {
			if c.ID == "service-status" {
				return c
			}
		}
		t.Fatalf("no service-status on %s", id)
		return engine.ProgrammeCheck{}
	}
	mustCommit(t, e, "Operation", "svc-old", "p1", svc("svc-old", ""))
	if c := state("svc-old"); c.State != "ok" || c.Message != "Running." {
		t.Fatalf("a service with no status: %+v", c)
	}
	mustCommit(t, e, "Operation", "svc-new", "p1", svc("svc-new", "planned"))
	if c := state("svc-new"); c.State != "warn" || !strings.Contains(c.Message, "no project sets it up") {
		t.Fatalf("a planned service nobody sets up: %+v", c)
	}
	mustCommit(t, e, "Project", "set-up", "p1", strings.Replace(projectYAML("set-up", "  operation: svc-new\n"), "  name: P\n", "  name: Set up checks\n", 1))
	if c := state("svc-new"); c.State != "ok" || !strings.Contains(c.Message, "Set up checks") {
		t.Fatalf("a planned service a project sets up: %+v", c)
	}
	checks, err := e.ProjectChecks(ctx, "set-up", false)
	if err != nil || checksByID(checks.Items)["landing-operation"].Message != "Lands in a planned service, which this project sets up." {
		t.Fatalf("the project's landing: %+v %v", checksByID(checks.Items)["landing-operation"], err)
	}
}
