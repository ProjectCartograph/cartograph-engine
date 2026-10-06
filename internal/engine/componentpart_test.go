package engine_test

import (
	"context"
	"testing"
)

// A component holds only what is its own (TAXONOMY.md D15): its schedule,
// success criteria, service and service owner are its parent's, so none
// of them is asked of it.
func TestAComponentIsAskedOnlyForItsOwnPart(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	ctx := context.Background()
	mustCommit(t, e, "Project", "parent", "p1", projectYAML("parent", ""))
	part := "  alignment:\n    partOf: parent\n"
	if err := e.PutWorking(ctx, "Project", "part", []byte(projectYAML("part", part))); err != nil {
		t.Fatal(err)
	}
	checks, err := e.ProjectChecks(ctx, "part", false)
	if err != nil {
		t.Fatal(err)
	}
	by := checksByID(checks.Items)
	for _, id := range []string{"timeline-start-phases", "success-criteria", "landing-operation", "landing-owner", "landing-criteria"} {
		if c := by[id]; c.State != "ok" {
			t.Errorf("%s asked of a component: %+v", id, c)
		}
	}
	if _, asked := by["landing-service-funding"]; asked {
		t.Error("a component is asked who pays to run its parent's service")
	}
}

// A component's objective is measured by its parent's indicators, so it
// is not asked for key results of its own; one it gives is still checked.
func TestAComponentObjectiveNeedsNoKeyResults(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	ctx := context.Background()
	mustCommit(t, e, "Project", "parent", "p1", projectYAML("parent", ""))
	part := "  alignment:\n    partOf: parent\n  objectives:\n    - {id: o1, objective: Grade every lot at intake by one standard}\n"
	if err := e.PutWorking(ctx, "Project", "part", []byte(projectYAML("part", part))); err != nil {
		t.Fatal(err)
	}
	checks, err := e.ProjectChecks(ctx, "part", false)
	if err != nil {
		t.Fatal(err)
	}
	if c := checksByID(checks.Items)["goals-key-results-count"]; c.State != "ok" {
		t.Errorf("a component's objective asked for key results: %+v", c)
	}
}
