package engine_test

import "testing"

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
			// Who ultimately determines whether the project succeeded is
			// the one question a criterion may not leave open.
			name: "missing confirmedBy", kind: "Project", wantProblem: true,
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

func TestObjectiveMustBeQualitative(t *testing.T) {
	e := seededEngine(t)
	runSchemaCases(t, e, []schemaCase{
		{
			name: "goal objective with a digit is rejected", kind: "Goal", wantProblem: true, wantSubstr: "qualitative",
			yaml: "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: g6\n  name: Goal Six\nspec:\n  level: goal\n  objective: Reach 75 percent attainment by next year\n",
		},
		{
			name: "goal objective without a digit is fine", kind: "Goal",
			yaml: "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: g6\n  name: Goal Six\nspec:\n  level: goal\n  objective: Raise quality across every depot\n",
		},
		{
			name: "project objective with a digit is rejected", kind: "Project", wantProblem: true, wantSubstr: "qualitative",
			yaml: "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: proj3\n  name: Project Three\nspec:\n  team: t1\n  summary:\n    problems:\n      - problem: {situation: A gap}\n        change: {what: No more gap}\n  objectives:\n    - objective: Deliver 1200 orders this year\n",
		},
	})
}
