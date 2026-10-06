package engine_test

import (
	"context"
	"testing"
)

// Attainable catches a target that cannot be reached as written: one that
// moves against the measure's direction, or is dated before today's figure.
func TestAttainableCatchesTargetsThatCannotBeReached(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	ctx := context.Background()
	commit := func(id, krs string) {
		t.Helper()
		y := "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: " + id + "\n  name: " + id + "\nspec:\n  level: objective\n  parent: g1\n  objective: Raise the rate\n  keyResults:\n" + krs
		if _, err := e.Commit(ctx, "Goal", id, []byte(y), "p1", "seed"); err != nil {
			t.Fatal(err)
		}
	}
	commit("wrong-way", "    - id: kr-1\n      metric: Rate\n      direction: increase\n      kind: percent\n      baseline: {value: 60, date: \"2025-07\"}\n      target: {value: 50, date: \"2027-07\"}\n")
	commit("backwards", "    - id: kr-1\n      metric: Rate\n      direction: increase\n      kind: percent\n      baseline: {value: 60, date: \"2025-07\"}\n      target: {value: 70, date: \"2024-07\"}\n")
	commit("fine", "    - id: kr-1\n      metric: Rate\n      direction: increase\n      kind: percent\n      baseline: {value: 60, date: \"2025-07\"}\n      target: {value: 70, date: \"2027-07\"}\n")
	for id, want := range map[string]string{"wrong-way": "warn", "backwards": "warn", "fine": "ok"} {
		checks, err := e.GoalChecks(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		wantCheck(t, checks, "smart-attainable", want)
	}
}

// A horizon is inherited from the aim above; a child's own horizon must sit
// inside it, and Time-bound reads targets against it.
func TestHorizonInheritedAndChecked(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	ctx := context.Background()
	commit := func(id, y string) {
		t.Helper()
		if _, err := e.Commit(ctx, "Goal", id, []byte(y), "p1", "seed"); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
	}
	commit("gh", "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: gh\n  name: Goal H\nspec:\n  level: goal\n  objective: Raise attainment\n  horizon: {start: \"2025\", end: \"2030\"}\n")
	commit("oh", "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: oh\n  name: Objective H\nspec:\n  level: objective\n  parent: gh\n  objective: Target support\n  keyResults:\n    - id: kr-1\n      metric: Rate\n      direction: increase\n      kind: percent\n      baseline: {value: 10, date: \"2025-07\"}\n      target: {value: 90, date: \"2032-07\"}\n")
	commit("ox", "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: ox\n  name: Objective X\nspec:\n  level: objective\n  parent: gh\n  objective: Target support\n  horizon: {start: \"2026-01\", end: \"2031-12\"}\n")

	checks, err := e.GoalChecks(ctx, "oh")
	if err != nil {
		t.Fatal(err)
	}
	// Inherited 2025 to 2030; a target in 2032 falls outside it.
	wantCheck(t, checks, "horizon", "ok")
	c := wantCheck(t, checks, "smart-time-bound", "warn")
	if c.Message != "Time-bound: 1 target falls outside the horizon, 2025 to 2030." {
		t.Fatalf("time-bound message = %q", c.Message)
	}
	checks, err = e.GoalChecks(ctx, "ox")
	if err != nil {
		t.Fatal(err)
	}
	wantCheck(t, checks, "horizon", "warn")
	wantCheck(t, checks, "owner", "warn")

	tree, err := e.GoalTree(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range tree.Nodes {
		for _, o := range g.Children {
			if o.ID == "oh" && (o.Horizon == nil || !o.Horizon.Inherited || o.Horizon.End != "2030-12") {
				t.Fatalf("oh horizon = %+v, want inherited 2025-01 to 2030-12", o.Horizon)
			}
		}
	}
}
