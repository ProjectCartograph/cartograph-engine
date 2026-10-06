package engine_test

import (
	"context"
	"testing"
)

// An outcome that closes no gap is fixed on the outcome, where the gap is
// chosen; the objective above it points there rather than at itself.
func TestAGapCheckPointsAtTheOutcomeThatNeedsIt(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	ctx := context.Background()
	commit := func(id, y string) {
		t.Helper()
		if _, err := e.Commit(ctx, "Goal", id, []byte(y), "p1", "seed"); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
	}
	commit("oz", "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: oz\n  name: Objective Z\nspec:\n  level: objective\n  parent: g1\n  objective: Grade alike\n")
	commit("uz", "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: uz\n  name: Outcome Z\nspec:\n  level: outcome\n  parent: oz\n  objective: Every depot grades alike\n")

	checks, err := e.GoalChecks(ctx, "oz")
	if err != nil {
		t.Fatal(err)
	}
	c := wantCheck(t, checks, "outcomes-close-gaps", "warn")
	if c.Fix == nil || c.Fix.Section != "closesGap" || c.Fix.Goal != "uz" {
		t.Fatalf("the objective's fix = %+v, want the outcome's closesGap", c.Fix)
	}
	checks, err = e.GoalChecks(ctx, "uz")
	if err != nil {
		t.Fatal(err)
	}
	c = wantCheck(t, checks, "closes-gap", "warn")
	if c.Fix == nil || c.Fix.Section != "closesGap" || c.Fix.Goal != "" {
		t.Fatalf("the outcome's fix = %+v, want its own closesGap", c.Fix)
	}
}
