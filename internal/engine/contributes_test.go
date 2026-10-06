package engine_test

import (
	"context"
	"testing"
)

// TAXONOMY.md D24: an outcome says what else it leads to, and why; a gap
// names the outcome that would close it.
func TestOutcomeContributesToAnotherObjectiveWithAReason(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	ctx := context.Background()
	mustCommit(t, e, "Goal", "g2-s", "local", "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: g2-s\n  name: Second objective\nspec:\n  level: objective\n  parent: g1\n  objective: Improve something else\n")
	outcome := func(links string) string {
		return "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: o1\n  name: An outcome\nspec:\n  level: outcome\n  parent: g1-s\n  objective: Something is true\n" + links
	}
	state := func() string {
		checks, err := e.GoalChecks(ctx, "o1")
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range checks {
			if c.ID == "contributes-to" {
				return c.State
			}
		}
		return ""
	}
	mustCommit(t, e, "Goal", "o1", "local", outcome("  contributesTo:\n    - {goal: g2-s, because: The same evidence serves both}\n"))
	if s := state(); s != "ok" {
		t.Fatalf("a reasoned link to another objective should pass, got %q", s)
	}
	mustCommit(t, e, "Goal", "o1", "local", outcome("  contributesTo:\n    - {goal: g2-s}\n"))
	if s := state(); s != "warn" {
		t.Fatalf("a link with no reason should be advised, got %q", s)
	}
	mustCommit(t, e, "Goal", "o1", "local", outcome("  contributesTo:\n    - {goal: g1-s, because: It is filed there}\n"))
	if s := state(); s != "warn" {
		t.Fatalf("a link to its own parent adds nothing and should be advised, got %q", s)
	}
}

func TestGapNamesTheOutcomeThatWouldCloseIt(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	ctx := context.Background()
	for _, tc := range []struct{ spec, want string }{
		{"  current: Things are short\n", "warn"},
		{"  current: Things are short\n  outcomes: [g1-f]\n", "ok"},
	} {
		mustCommit(t, e, "Gap", "gp", "local", "apiVersion: cartograph/v1\nkind: Gap\nmetadata:\n  id: gp\n  name: A gap\nspec:\n"+tc.spec)
		checks, err := e.GapChecks(ctx, "gp")
		if err != nil {
			t.Fatal(err)
		}
		got := ""
		for _, c := range checks {
			if c.ID == "gap-outcome" {
				got = c.State
			}
		}
		if got != tc.want {
			t.Errorf("%q: gap-outcome = %q, want %q", tc.spec, got, tc.want)
		}
	}
}
