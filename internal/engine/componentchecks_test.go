package engine_test

import (
	"context"
	"testing"
)

// TAXONOMY.md D15: a project can be a component of one other project. The
// parent holds the results framework; the component holds its own lead,
// deliverables and key results, and shares the parent's goals, programmes,
// sponsor and budget.

func TestComponentTakesItsParentsGoalsSponsorAndBudget(t *testing.T) {
	e := seededEngine(t)
	ctx := context.Background()
	mustCommit(t, e, "Project", "parent", "p1", projectYAML("parent",
		"  alignment:\n    goals: [g1-f]\n"+
			"  resources:\n    - {id: sp, role: sponsor, resource: r1}\n    - {role: manager}\n"))
	mustCommit(t, e, "Project", "part", "p1", projectYAML("part",
		"  alignment:\n    partOf: parent\n"+
			"  resources:\n    - {role: manager}\n"))

	checks, err := e.ProjectChecks(ctx, "part", false)
	if err != nil {
		t.Fatal(err)
	}
	byID := checksByID(checks.Items)
	for _, id := range []string{"goals-aligned", "resources-sponsor-lead", "resources-funding", "components-one-sponsor"} {
		if byID[id].State != "ok" {
			t.Errorf("%s: a component inherits this from its parent, got %+v", id, byID[id])
		}
	}
}

func TestComponentWithItsOwnSponsorIsAProgrammeInTheMaking(t *testing.T) {
	e := seededEngine(t)
	ctx := context.Background()
	mustCommit(t, e, "Resource", "r2", "local", "apiVersion: cartograph/v1\nkind: Resource\nmetadata:\n  id: r2\n  name: Resource Two\nspec:\n  name: Resource Two\n  category: personRole\n")
	mustCommit(t, e, "Project", "parent", "p1", projectYAML("parent",
		"  resources:\n    - {id: sp, role: sponsor, resource: r1}\n    - {role: manager}\n"))
	mustCommit(t, e, "Project", "part", "p1", projectYAML("part",
		"  alignment:\n    partOf: parent\n"+
			"  resources:\n    - {id: sp, role: sponsor, resource: r2}\n    - {role: manager}\n"))

	checks, err := e.ProjectChecks(ctx, "part", false)
	if err != nil {
		t.Fatal(err)
	}
	got := checksByID(checks.Items)["components-one-sponsor"]
	if got.State != "warn" {
		t.Fatalf("a different sponsor should be advised, got %+v", got)
	}
	if checks.Blocking != 0 && got.State == "block" {
		t.Fatal("the sponsor test advises; it must never block")
	}
}

func TestComponentNamesNoProgrammesAndGoesOneLevelDeep(t *testing.T) {
	e := seededEngine(t)
	ctx := context.Background()
	mustCommit(t, e, "Programme", "prog", "p1", "apiVersion: cartograph/v1\nkind: Programme\nmetadata:\n  id: prog\n  name: Prog\nspec:\n  name: Prog\n  leadTeam: t1\n  aim: {change: Things change}\n  goals: [g1-f]\n")
	mustCommit(t, e, "Project", "top", "p1", projectYAML("top", ""))
	mustCommit(t, e, "Project", "middle", "p1", projectYAML("middle", "  alignment:\n    partOf: top\n"))
	mustCommit(t, e, "Project", "bottom", "p1", projectYAML("bottom",
		"  alignment:\n    partOf: middle\n    programmes: [prog]\n"))

	checks, err := e.ProjectChecks(ctx, "bottom", false)
	if err != nil {
		t.Fatal(err)
	}
	byID := checksByID(checks.Items)
	if byID["components-one-level"].State != "warn" {
		t.Errorf("a component of a component should be advised, got %+v", byID["components-one-level"])
	}
	if byID["components-programmes"].State != "warn" {
		t.Errorf("a component naming programmes should be advised, got %+v", byID["components-programmes"])
	}
}

func TestParentWithComponentsNeedsSomethingToAddUpTo(t *testing.T) {
	e := seededEngine(t)
	ctx := context.Background()
	mustCommit(t, e, "Project", "parent", "p1", projectYAML("parent", ""))
	mustCommit(t, e, "Project", "part", "p1", projectYAML("part", "  alignment:\n    partOf: parent\n"))

	checks, err := e.ProjectChecks(ctx, "parent", false)
	if err != nil {
		t.Fatal(err)
	}
	if got := checksByID(checks.Items)["components-add-up"]; got.State != "warn" {
		t.Fatalf("a parent with components and no key result should be advised, got %+v", got)
	}

	checks, err = e.ProjectChecks(ctx, "part", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, present := checksByID(checks.Items)["components-add-up"]; present {
		t.Fatal("a component with no components of its own gets no add-up check")
	}
}

// A component is accepted into its parent and lands with it, under the
// parent's mandate, moving the parent's KPIs: none of the three is asked
// of it again.
func TestComponentLandsWithItsParent(t *testing.T) {
	e := seededEngine(t)
	ctx := context.Background()
	mustCommit(t, e, "Project", "parent", "p1", projectYAML("parent", ""))
	mustCommit(t, e, "Project", "part", "p1", projectYAML("part", "  alignment:\n    partOf: parent\n"))
	checks, err := e.ProjectChecks(ctx, "part", false)
	if err != nil {
		t.Fatal(err)
	}
	byID := checksByID(checks.Items)
	for _, id := range []string{"aim-mandate", "landing-criteria", "measures-kpis"} {
		if byID[id].State != "ok" {
			t.Errorf("%s: a component inherits this from its parent, got %+v", id, byID[id])
		}
	}
}
