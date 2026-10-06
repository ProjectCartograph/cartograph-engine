package engine_test

import (
	"strings"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/identity"
)

// An agent defines several manifests in one change set, each naming the
// one before it. Their checks read one another's drafts, not the stored
// record, where none of them exists yet: a project aligned to a drafted
// outcome is aligned, a drafted programme has the drafted project as a
// component, and a drafted planned service is set up by it.
func TestChecksReadTheDraftsInTheirChangeSet(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	agent := actingAs(identity.Principal{Subject: "ada@example.org", Email: "ada@example.org", Name: "Ada", Agent: "Claude", Grant: "g1"})
	save := func(kind, id, text string) {
		t.Helper()
		if err := e.SaveInChangeSet(agent, "", kind, id, []byte(text)); err != nil {
			t.Fatalf("save %s/%s: %v", kind, id, err)
		}
	}
	save("Goal", "outcome-new", "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: outcome-new\n  name: Faults found at intake\nspec:\n  level: outcome\n  statement: Faults are found at intake\n")
	save("Programme", "programme-new", programmeYAML("programme-new", ""))
	save("Operation", "service-new", "apiVersion: cartograph/v1\nkind: Operation\nmetadata:\n  id: service-new\n  name: Checks\nspec:\n  purpose: Check deliveries\n  team: t1\n  status: planned\n")
	save("Segment", "rural", "apiVersion: cartograph/v1\nkind: Segment\nmetadata:\n  id: rural\n  name: Rural\nspec:\n  name: Rural\n  description: Depots outside the towns.\n")
	save("Gap", "gap-new", "apiVersion: cartograph/v1\nkind: Gap\nmetadata:\n  id: gap-new\n  name: Faults reach buyers\nspec:\n  outcomes: [outcome-new]\n  segments: [rural]\n")
	project := strings.Replace(projectYAML("project-new", "  alignment:\n    goals: [outcome-new]\n    programmes: [programme-new]\n  operation: service-new\n"),
		"        change: {what: No more gap}\n", "        change: {what: No more gap}\n        gaps: [{gap: gap-new, segments: [rural]}]\n", 1)
	save("Project", "project-new", project)

	cs, err := e.WorkingChangeSet(agent, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := e.InChangeSet(agent, cs.ID)
	if err != nil {
		t.Fatal(err)
	}
	state := func(kind, id, check string) engine.Check {
		t.Helper()
		checks, err := e.DraftChecks(ctx, kind, id)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range checks {
			if c.ID == check {
				return c
			}
		}
		t.Fatalf("%s/%s has no %s", kind, id, check)
		return engine.Check{}
	}
	if c := state("Project", "project-new", "goals-functional-level"); c.State != "ok" {
		t.Errorf("a project aligned to a drafted outcome: %+v", c)
	}
	if c := state("Programme", "programme-new", "components-present"); c.State != "ok" {
		t.Errorf("a drafted programme with a drafted member: %+v", c)
	}
	if c := state("Operation", "service-new", "service-status"); c.State != "ok" {
		t.Errorf("a drafted planned service a drafted project sets up: %+v", c)
	}
	if c := state("Gap", "gap-new", "gap-covered"); c.State != "ok" {
		t.Errorf("a drafted gap a drafted project works on: %+v", c)
	}
}

// A goal drafted beside the purpose is judged relevant against it, not
// told there is no vision or mission to judge it by.
func TestAGoalIsJudgedAgainstADraftedPurpose(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	agent := actingAs(identity.Principal{Subject: "ada@example.org", Email: "ada@example.org", Name: "Ada", Agent: "Claude", Grant: "g1"})
	for _, d := range []struct{ kind, id, text string }{
		{"Purpose", "default", "apiVersion: cartograph/v1\nkind: Purpose\nmetadata:\n  id: default\n  name: Purpose\nspec:\n  vision: Every member's produce reaches a buyer sound\n"},
		{"Goal", "goal-new", "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: goal-new\n  name: Sound produce\nspec:\n  level: goal\n  statement: Sound produce\n"},
	} {
		if err := e.SaveInChangeSet(agent, "", d.kind, d.id, []byte(d.text)); err != nil {
			t.Fatal(err)
		}
	}
	cs, _ := e.WorkingChangeSet(agent, "")
	ctx, err := e.InChangeSet(agent, cs.ID)
	if err != nil {
		t.Fatal(err)
	}
	checks, err := e.DraftChecks(ctx, "Goal", "goal-new")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range checks {
		if c.ID == "smart-relevant" && strings.Contains(c.Message, "no vision or mission") {
			t.Fatalf("the drafted purpose was not read: %+v", c)
		}
	}
}
