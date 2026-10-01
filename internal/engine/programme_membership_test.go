package engine_test

import (
	"context"
	"strings"
	"testing"
)

// Belonging to a programme is a declaration, and sharing a goal corroborates
// it rather than constituting it. The rule warned from 2026-09-28; it blocked
// until then, and the reasoning for the change is in TAXONOMY.md.
//
// The rule had no test at all while it was the one thing that could stop a
// definition being handed off.
func TestProjectProgrammeMembership(t *testing.T) {
	e := seededEngine(t)
	ctx := context.Background()

	programme := func(id string, goals string) string {
		return "apiVersion: cartograph/v1\nkind: Programme\nmetadata:\n  id: " + id + "\n  name: " + id +
			"\nspec:\n  name: " + id + "\n  aim: {change: Make something better}\n  leadTeam: t1\n  goals: [" + goals + "]\n"
	}
	mustCommit(t, e, "Programme", "shares-a-goal", "local", programme("shares-a-goal", "g1-f"))
	mustCommit(t, e, "Programme", "shares-nothing", "local", programme("shares-nothing", ""))
	mustCommit(t, e, "Programme", "shares-nothing-either", "local", programme("shares-nothing-either", ""))

	project := func(id string, programmes string) string {
		return "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: " + id + "\n  name: P\nspec:\n  team: t1\n" +
			"  summary:\n    problems:\n      - problem: {situation: A gap}\n        change: {what: No more gap}\n" +
			"  alignment:\n    goals: [g1-f]\n    programmes: [" + programmes + "]\n"
	}
	check := func(t *testing.T, id, programmes string) (state, message string, present bool) {
		t.Helper()
		mustCommit(t, e, "Project", id, "local", project(id, programmes))
		checks, err := e.ProjectChecks(ctx, id, false)
		if err != nil {
			t.Fatal(err)
		}
		item, ok := checksByID(checks.Items)["goals-programme-membership"]
		return item.State, item.Message, ok
	}

	t.Run("a programme it shares a goal with passes", func(t *testing.T) {
		state, msg, ok := check(t, "proj-pm-ok", "shares-a-goal")
		if !ok || state != "ok" {
			t.Fatalf("expected ok, got %q %q (present=%v)", state, msg, ok)
		}
	})

	// The substance of the change. An enabling component can belong to a
	// programme and serve no goal the programme lists, so this is worth
	// saying and not worth refusing.
	t.Run("a programme it shares nothing with warns, and does not block", func(t *testing.T) {
		state, msg, _ := check(t, "proj-pm-warn", "shares-nothing")
		if state != "warn" {
			t.Fatalf("expected warn, got %q (%s)", state, msg)
		}
		if !strings.Contains(msg, "shares-nothing") {
			t.Fatalf("expected the programme named, got %q", msg)
		}
	})

	t.Run("several unproven programmes are counted", func(t *testing.T) {
		state, msg, _ := check(t, "proj-pm-many", "shares-nothing, shares-nothing-either")
		if state != "warn" || !strings.Contains(msg, "2 programmes") {
			t.Fatalf("expected a warning counting 2, got %q %q", state, msg)
		}
	})

	// A project needs no programme at all, so naming none is not a state
	// worth reporting either way.
	t.Run("naming no programme reports nothing", func(t *testing.T) {
		_, _, present := check(t, "proj-pm-none", "")
		if present {
			t.Fatal("expected no membership check when the project names no programme")
		}
	})

	// Nothing about membership may stop a handoff now.
	t.Run("an unproven programme leaves nothing blocking", func(t *testing.T) {
		mustCommit(t, e, "Project", "proj-pm-block", "local", project("proj-pm-block", "shares-nothing"))
		checks, err := e.ProjectChecks(ctx, "proj-pm-block", false)
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range checks.Items {
			if it.ID == "goals-programme-membership" && it.State == "block" {
				t.Fatalf("membership must not block: %+v", it)
			}
		}
	})
}
