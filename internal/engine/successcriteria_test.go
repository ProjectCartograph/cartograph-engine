package engine_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
)

// Project gains successCriteria (third I0 delta) and deliverables (fourth).
//
// The criterion's own shape was rewritten on 2026-09-27 against the
// success-criteria model: the outcome, the metric and the standard that
// settles it, where that measurement is read from and how often, and the
// roles that track and confirm it. `category`, `verification`,
// `threshold`, `signoff` and `derivedFrom` are all gone; `source` and
// `cycle` are references into the DataSource and ReportingCycle
// registers, so the method and the frequency are named once and reused.
func TestProjectSuccessCriteriaRules(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	base := "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: proj2\n  name: Project Two\nspec:\n  team: t1\n  summary:\n    problems:\n      - problem: {situation: A gap}\n        change: {what: No more gap}\n"

	runSchemaCases(t, e, []schemaCase{
		{
			name: "a measured criterion, with its method and frequency", kind: "Project",
			yaml: base + "  successCriteria:\n    - id: sc-1\n      statement: Coverage is close to complete\n      metric: business\n      standard: 85 percent\n      source: d1\n      cycle: c1\n      owner: {external: Quality reviewer}\n      confirmedBy: {external: Sponsor}\n      when: atLanding\n",
		},
		{
			// Compliance is satisfied rather than measured, so it carries
			// no standard, source or cycle and is still valid without them.
			name: "a compliance criterion needs no measurement", kind: "Project",
			yaml: base + "  successCriteria:\n    - id: sc-1\n      statement: The review is closed with no open actions\n      metric: compliance\n      confirmedBy: {external: Sponsor}\n      when: atClosing\n",
		},
		{
			name: "bad enum metric", kind: "Project", wantProblem: true,
			yaml: base + "  successCriteria:\n    - id: sc-1\n      statement: Coverage is close to complete\n      metric: vibes\n      confirmedBy: {external: Sponsor}\n      when: atClosing\n",
		},
		{
			name: "missing metric", kind: "Project", wantProblem: true,
			yaml: base + "  successCriteria:\n    - id: sc-1\n      statement: Coverage is close to complete\n      confirmedBy: {external: Sponsor}\n      when: atClosing\n",
		},
		{
			// Who ultimately determines whether the project succeeded may
			// not be left open at handoff: success-confirmer blocks it. A
			// check never blocks a save, so a criterion a document states
			// is written before its confirmer is decided.
			name: "a criterion saved before its confirmer is decided", kind: "Project",
			yaml: base + "  successCriteria:\n    - id: sc-1\n      statement: Coverage is close to complete\n      metric: business\n      when: atClosing\n",
		},
		{
			name: "bad enum when", kind: "Project", wantProblem: true,
			yaml: base + "  successCriteria:\n    - id: sc-1\n      statement: Coverage is close to complete\n      metric: business\n      confirmedBy: {external: Sponsor}\n      when: eventually\n",
		},
		{
			name: "missing when", kind: "Project", wantProblem: true,
			yaml: base + "  successCriteria:\n    - id: sc-1\n      statement: Coverage is close to complete\n      metric: business\n      confirmedBy: {external: Sponsor}\n",
		},
		{
			name: "dangling source ref", kind: "Project", wantProblem: true, wantSubstr: "does not exist",
			yaml: base + "  successCriteria:\n    - id: sc-1\n      statement: Coverage is close to complete\n      metric: business\n      source: does-not-exist\n      confirmedBy: {external: Sponsor}\n      when: atLanding\n",
		},
		{
			name: "dangling cycle ref", kind: "Project", wantProblem: true, wantSubstr: "does not exist",
			yaml: base + "  successCriteria:\n    - id: sc-1\n      statement: Coverage is close to complete\n      metric: business\n      cycle: does-not-exist\n      confirmedBy: {external: Sponsor}\n      when: atLanding\n",
		},
		{
			name: "the old shape is refused outright", kind: "Project", wantProblem: true, wantSubstr: "additional properties",
			yaml: base + "  successCriteria:\n    - id: sc-1\n      category: outcome\n      statement: Coverage is close to complete\n      verification: Compare against the register\n      signoff: accountable\n      when: atClosing\n",
		},
		{
			name: "duplicate success criteria ids", kind: "Project", wantProblem: true, wantSubstr: "duplicates",
			yaml: base + "  successCriteria:\n    - id: sc-1\n      statement: Coverage is close to complete\n      metric: business\n      confirmedBy: {external: Sponsor}\n      when: atClosing\n    - id: sc-1\n      statement: The tool is easy to use\n      metric: customer\n      confirmedBy: {external: Lead}\n      when: atClosing\n",
		},
	})
}

func TestProjectDeliverables(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	base := "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: proj5\n  name: Project Five\nspec:\n  team: t1\n  summary:\n    problems:\n      - problem: {situation: A gap}\n        change: {what: No more gap}\n"

	runSchemaCases(t, e, []schemaCase{
		{
			name: "valid deliverables",
			kind: "Project",
			yaml: base + "  deliverables:\n    - id: dv-1\n      name: Training pack\n      description: Slides and a guide\n      acceptance: [{by: {external: Quality reviewer}, outcome: signs it off}]\n    - id: dv-2\n      name: Reporting view\n",
		},
		{
			name: "missing name", kind: "Project", wantProblem: true,
			yaml: base + "  deliverables:\n    - id: dv-1\n      description: Slides and a guide\n",
		},
		{
			name: "duplicate deliverable ids", kind: "Project", wantProblem: true, wantSubstr: "duplicates",
			yaml: base + "  deliverables:\n    - id: dv-1\n      name: Training pack\n    - id: dv-1\n      name: Reporting view\n",
		},
	})
}

// A number in an objective is never refused: whether the objective says a
// change or a figure to reach is the decision model's to judge, and it
// advises (docs/adr/0030). "Every 2-year-old reaches the milestone" names
// a group; a pattern could not tell it from a target.
func TestAnObjectiveWithANumberIsNotRefused(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	runSchemaCases(t, e, []schemaCase{
		{
			name: "a goal objective naming a group by number", kind: "Goal",
			yaml: "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: g6\n  name: Goal Six\nspec:\n  level: goal\n  objective: Every 2-year-old in the valley reaches the language milestone\n",
		},
		{
			name: "a project objective with a figure in it", kind: "Project",
			yaml: "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: proj3\n  name: Project Three\nspec:\n  team: t1\n  summary:\n    problems:\n      - problem: {situation: A gap}\n        change: {what: No more gap}\n  objectives:\n    - objective: Deliver 1200 orders this year\n",
		},
	})
}

// A criterion read once, at closing or at landing, needs its standard and
// source but no cycle; one judged after closing is read repeatedly and
// needs a cycle too. The check names what is missing.
func TestOnlyACriterionReadAfterClosingNeedsACycle(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	ctx := context.Background()
	criterion := func(when, extra string) string {
		return "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: once\n  name: Once\nspec:\n  team: t1\n  successCriteria:\n    - id: sc-1\n      statement: Operators show a measured knowledge gain\n      metric: team\n      standard: 20 points above the pre-training score\n      source: d1\n      confirmedBy: {external: Sponsor}\n      when: " + when + "\n" + extra
	}
	measured := func(y string) engine.ProjectCheckItem {
		t.Helper()
		if err := e.PutWorking(ctx, "Project", "once", []byte(y)); err != nil {
			t.Fatal(err)
		}
		checks, err := e.ProjectChecks(ctx, "once", false)
		if err != nil {
			t.Fatal(err)
		}
		return checksByID(checks.Items)["success-measured"]
	}
	if c := measured(criterion("atClosing", "")); c.State != "ok" {
		t.Fatalf("read once at closing, no cycle: %+v", c)
	}
	if c := measured(criterion("postClosingCycle", "")); c.State != "block" || !strings.Contains(c.Message, "1 needs a reporting cycle") {
		t.Fatalf("read after closing, no cycle: %+v", c)
	}
	if c := measured(criterion("postClosingCycle", "      cycle: c1\n")); c.State != "ok" {
		t.Fatalf("read after closing, with a cycle: %+v", c)
	}
	if c := measured(strings.Replace(criterion("atLanding", ""), "      source: d1\n", "", 1)); c.State != "block" || !strings.Contains(c.Message, "1 needs a data source") {
		t.Fatalf("no source: %+v", c)
	}
	// On time and on budget is read from the project's own schedule and
	// spend: no data source is asked for.
	efficiency := strings.Replace(strings.Replace(criterion("atClosing", ""), "      source: d1\n", "", 1), "metric: team", "metric: efficiency", 1)
	if c := measured(efficiency); c.State != "ok" {
		t.Fatalf("an efficiency criterion asked for a source: %+v", c)
	}
}
