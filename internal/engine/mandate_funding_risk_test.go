package engine_test

import (
	"strings"
	"testing"
)

// I0.1 contract additions (2026-09-17): mandate on Project, Programme and
// Operation; funding on Project (at most one line per currency); risk
// type (required) and escalate (reason required when flag is true);
// personalData on DataUse.produces (required) and .consumes (optional);
// the new BeneficiaryGroup directory kind and Project.spec.summary's
// beneficiaries[] shape (a group reference and nothing else); and the maxLength caps added across
// Goal and Project free text.

func TestProjectMandate(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	base := "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: proj-mandate\n  name: Project Mandate\nspec:\n  team: t1\n  summary:\n    problems:\n      - problem: {situation: A gap}\n        change: {what: No more gap}\n"

	runSchemaCases(t, e, []schemaCase{
		{
			name: "valid mandate, full",
			kind: "Project",
			yaml: base + "  mandate:\n    - kind: decision\n      title: Approved to proceed\n      reference: DEC-1\n      date: \"2025-08\"\n      issuedBy: Board\n    - kind: policy\n      title: Standing policy\n",
		},
		{
			name: "valid mandate, date with day", kind: "Project",
			yaml: base + "  mandate:\n    - kind: contract\n      title: Supplier agreement\n      date: \"2025-08-15\"\n",
		},
		{
			name: "missing mandate title", kind: "Project", wantProblem: true,
			yaml: base + "  mandate:\n    - kind: decision\n      reference: DEC-1\n",
		},
		{
			name: "bad enum mandate kind", kind: "Project", wantProblem: true,
			yaml: base + "  mandate:\n    - kind: memo\n      title: Approved to proceed\n",
		},
		{
			name: "mandate title exceeds maxLength 120", kind: "Project", wantProblem: true, wantSubstr: "maxLength",
			yaml: base + "  mandate:\n    - kind: decision\n      title: " + strings.Repeat("x", 121) + "\n",
		},
		{
			name: "bad mandate date format", kind: "Project", wantProblem: true,
			yaml: base + "  mandate:\n    - kind: decision\n      title: Approved to proceed\n      date: \"2025/08\"\n",
		},
	})
}

func TestProgrammeAndOperationMandate(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	runSchemaCases(t, e, []schemaCase{
		{
			name: "valid programme mandate", kind: "Programme",
			yaml: "apiVersion: cartograph/v1\nkind: Programme\nmetadata:\n  id: prog-mandate\n  name: Programme Mandate\nspec:\n  name: Programme Mandate\n  aim: {change: Raise quality}\n  leadTeam: t1\n  mandate:\n    - kind: policy\n      title: Standing quality policy\n",
		},
		{
			name: "bad enum programme mandate kind", kind: "Programme", wantProblem: true,
			yaml: "apiVersion: cartograph/v1\nkind: Programme\nmetadata:\n  id: prog-mandate\n  name: Programme Mandate\nspec:\n  name: Programme Mandate\n  aim: {change: Raise quality}\n  leadTeam: t1\n  mandate:\n    - kind: memo\n      title: Standing quality policy\n",
		},
		{
			name: "valid operation mandate", kind: "Operation",
			yaml: "apiVersion: cartograph/v1\nkind: Operation\nmetadata:\n  id: op-mandate\n  name: Operation Mandate\nspec:\n  name: Operation Mandate\n  purpose: Keep the lights on\n  team: t1\n  mandate:\n    - kind: lawOrRegulation\n      title: Standing regulation\n",
		},
		{
			name: "missing operation mandate title", kind: "Operation", wantProblem: true,
			yaml: "apiVersion: cartograph/v1\nkind: Operation\nmetadata:\n  id: op-mandate\n  name: Operation Mandate\nspec:\n  name: Operation Mandate\n  purpose: Keep the lights on\n  team: t1\n  mandate:\n    - kind: request\n",
		},
	})
}

func TestProjectFunding(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	base := "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: proj-funding\n  name: Project Funding\nspec:\n  team: t1\n  summary:\n    problems:\n      - problem: {situation: A gap}\n        change: {what: No more gap}\n"

	runSchemaCases(t, e, []schemaCase{
		{
			name: "valid, one line", kind: "Project",
			yaml: base + "  funding:\n    - amount: 1000\n      currency: USD\n      status: approved\n",
		},
		{
			name: "valid, three distinct currencies", kind: "Project",
			yaml: base + "  funding:\n    - amount: 1000\n      currency: USD\n      status: approved\n    - amount: 500\n      currency: EUR\n      status: requested\n    - amount: 200\n      currency: TTD\n      status: unfunded\n",
		},
		{
			name: "missing status", kind: "Project", wantProblem: true,
			yaml: base + "  funding:\n    - amount: 1000\n      currency: USD\n",
		},
		{
			name: "bad currency pattern", kind: "Project", wantProblem: true,
			yaml: base + "  funding:\n    - amount: 1000\n      currency: us\n      status: approved\n",
		},
		{
			name: "negative amount", kind: "Project", wantProblem: true,
			yaml: base + "  funding:\n    - amount: -1\n      currency: USD\n      status: approved\n",
		},
		{
			name: "more than three funding lines violates maxItems", kind: "Project", wantProblem: true,
			yaml: base + "  funding:\n    - amount: 1\n      currency: USD\n      status: approved\n    - amount: 1\n      currency: EUR\n      status: approved\n    - amount: 1\n      currency: TTD\n      status: approved\n    - amount: 1\n      currency: GBP\n      status: approved\n",
		},
		{
			name: "duplicate currency rejected", kind: "Project", wantProblem: true, wantSubstr: "currency",
			yaml: base + "  funding:\n    - amount: 1000\n      currency: USD\n      status: approved\n    - amount: 500\n      currency: USD\n      status: requested\n",
		},
	})
}

func TestProjectRiskTypeAndEscalate(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	base := "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: proj-risk\n  name: Project Risk\nspec:\n  team: t1\n  summary:\n    problems:\n      - problem: {situation: A gap}\n        change: {what: No more gap}\n"

	runSchemaCases(t, e, []schemaCase{
		{
			name: "valid, no escalate", kind: "Project",
			yaml: base + "  risks:\n    - description: Vendor delay\n      type: risk\n",
		},
		{
			name: "valid, escalate flag false, no reason needed", kind: "Project",
			yaml: base + "  risks:\n    - description: Vendor delay\n      type: risk\n      escalate: {flag: false}\n",
		},
		{
			name: "valid, escalate flag true with reason", kind: "Project",
			yaml: base + "  risks:\n    - description: Vendor delay\n      type: issue\n      escalate: {flag: true, reason: Already affecting the season's reporting}\n",
		},
		{
			name: "missing risk type", kind: "Project", wantProblem: true,
			yaml: base + "  risks:\n    - description: Vendor delay\n",
		},
		{
			name: "bad enum risk type", kind: "Project", wantProblem: true,
			yaml: base + "  risks:\n    - description: Vendor delay\n      type: hazard\n",
		},
		{
			name: "escalate flag true without reason", kind: "Project", wantProblem: true, wantSubstr: "reason is required",
			yaml: base + "  risks:\n    - description: Vendor delay\n      type: risk\n      escalate: {flag: true}\n",
		},
	})
}

func TestDataUsePersonalData(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	base := "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: proj-data\n  name: Project Data\nspec:\n  team: t1\n  summary:\n    problems:\n      - problem: {situation: A gap}\n        change: {what: No more gap}\n"

	runSchemaCases(t, e, []schemaCase{
		{
			name: "valid, produces personalData set, consumes personalData omitted", kind: "Project",
			yaml: base + "  data:\n    consumes:\n      - source: d1\n        purpose: reconciliation\n    produces:\n      - output: recordsInExistingSource\n        sink: d1\n        purpose: results\n        personalData: none\n",
		},
		{
			name: "produces missing personalData", kind: "Project", wantProblem: true,
			yaml: base + "  data:\n    produces:\n      - output: recordsInExistingSource\n        sink: d1\n        purpose: results\n",
		},
		{
			name: "produces bad enum personalData", kind: "Project", wantProblem: true,
			yaml: base + "  data:\n    produces:\n      - output: recordsInExistingSource\n        sink: d1\n        purpose: results\n        personalData: secret\n",
		},
		{
			name: "consumes bad enum personalData", kind: "Project", wantProblem: true,
			yaml: base + "  data:\n    consumes:\n      - source: d1\n        purpose: reconciliation\n        personalData: secret\n",
		},
	})
}

func TestOperationDataUsePersonalData(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	runSchemaCases(t, e, []schemaCase{
		{
			name: "operation produces missing personalData", kind: "Operation", wantProblem: true,
			yaml: "apiVersion: cartograph/v1\nkind: Operation\nmetadata:\n  id: op-data\n  name: Operation Data\nspec:\n  name: Operation Data\n  purpose: Keep the lights on\n  team: t1\n  data:\n    produces:\n      - output: recordsInExistingSource\n        sink: d1\n        purpose: results\n",
		},
		{
			name: "operation produces with personalData is valid", kind: "Operation",
			yaml: "apiVersion: cartograph/v1\nkind: Operation\nmetadata:\n  id: op-data\n  name: Operation Data\nspec:\n  name: Operation Data\n  purpose: Keep the lights on\n  team: t1\n  data:\n    produces:\n      - output: recordsInExistingSource\n        sink: d1\n        purpose: results\n        personalData: personal\n",
		},
	})
}

// A beneficiary line names a group and nothing else: beneficiaries are
// qualitative (DESIGN_RULES.md), so the count fields the first
// draft of the kind carried are refused outright rather than ignored.
func TestProjectBeneficiaries(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	base := "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: proj-beneficiaries\n  name: Project Beneficiaries\nspec:\n  team: t1\n  summary:\n    problems:\n      - problem: {situation: A gap}\n        change: {what: No more gap}\n"

	runSchemaCases(t, e, []schemaCase{
		{
			name: "valid, a group and nothing else", kind: "Project",
			yaml: base + "    beneficiaries:\n      - group: bg1\n",
		},
		{
			name: "valid, several groups", kind: "Project",
			yaml: base + "    beneficiaries:\n      - group: bg1\n      - group: bg2\n",
		},
		{
			name: "missing group", kind: "Project", wantProblem: true,
			yaml: base + "    beneficiaries:\n      - {}\n",
		},
		{
			name: "dangling group ref", kind: "Project", wantProblem: true, wantSubstr: "does not exist",
			yaml: base + "    beneficiaries:\n      - group: does-not-exist\n",
		},
		{
			name: "a count is refused", kind: "Project", wantProblem: true, wantSubstr: "count",
			yaml: base + "    beneficiaries:\n      - group: bg1\n        count: 400\n",
		},
		{
			name: "a countBasis is refused", kind: "Project", wantProblem: true, wantSubstr: "countBasis",
			yaml: base + "    beneficiaries:\n      - group: bg1\n        countBasis: exact\n",
		},
	})
}

func TestI01MaxLengths(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	projectBase := "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: proj-caps\n  name: Project Caps\nspec:\n  team: t1\n"
	minimalSummary := "  summary:\n    problems:\n      - problem: {situation: A gap}\n        change: {what: No more gap}\n"

	over := func(n int) string { return strings.Repeat("x", n+1) }

	runSchemaCases(t, e, []schemaCase{
		{
			name: "goal objective exceeds maxLength 120", kind: "Goal", wantProblem: true, wantSubstr: "maxLength",
			yaml: "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: g-caps\n  name: Goal Caps\nspec:\n  level: goal\n  objective: " + over(120) + "\n",
		},
		{
			name: "project objective exceeds maxLength 250", kind: "Project", wantProblem: true, wantSubstr: "maxLength",
			yaml: projectBase + minimalSummary + "  objectives:\n    - objective: " + over(250) + "\n",
		},
		{
			name: "summary.problems[].problem.situation exceeds maxLength 600", kind: "Project", wantProblem: true, wantSubstr: "maxLength",
			yaml: projectBase + "  summary:\n    problems:\n      - problem: {situation: " + over(600) + "}\n        change: {what: No more gap}\n",
		},
		{
			name: "summary.problems[].change.what exceeds maxLength 600", kind: "Project", wantProblem: true, wantSubstr: "maxLength",
			yaml: projectBase + "  summary:\n    problems:\n      - problem: {situation: A gap}\n        change: {what: " + over(600) + "}\n",
		},
		{
			name: "scopeIn item exceeds maxLength 60", kind: "Project", wantProblem: true, wantSubstr: "maxLength",
			yaml: projectBase + minimalSummary + "    scopeIn:\n      - " + over(60) + "\n",
		},
		{
			name: "scopeOut item exceeds maxLength 60", kind: "Project", wantProblem: true, wantSubstr: "maxLength",
			yaml: projectBase + minimalSummary + "    scopeOut:\n      - " + over(60) + "\n",
		},
		{
			name: "deliverable name exceeds maxLength 60", kind: "Project", wantProblem: true, wantSubstr: "maxLength",
			yaml: projectBase + minimalSummary + "  deliverables:\n    - id: dv-1\n      name: " + over(60) + "\n",
		},
		{
			name: "deliverable description exceeds maxLength 240", kind: "Project", wantProblem: true, wantSubstr: "maxLength",
			yaml: projectBase + minimalSummary + "  deliverables:\n    - id: dv-1\n      name: Training pack\n      description: " + over(240) + "\n",
		},
		{
			name: "deliverable acceptance outcome exceeds maxLength 160", kind: "Project", wantProblem: true, wantSubstr: "maxLength",
			yaml: projectBase + minimalSummary + "  deliverables:\n    - id: dv-1\n      name: Training pack\n      acceptance: [{outcome: " + over(160) + "}]\n",
		},
		{
			name: "project kpi reason exceeds maxLength 160", kind: "Project", wantProblem: true, wantSubstr: "maxLength",
			yaml: projectBase + minimalSummary + "  kpis:\n    - kpi: k1\n      reason: " + over(160) + "\n",
		},
		{
			name: "risk description exceeds maxLength 160", kind: "Project", wantProblem: true, wantSubstr: "maxLength",
			yaml: projectBase + minimalSummary + "  risks:\n    - description: " + over(160) + "\n      type: risk\n",
		},
		{
			name: "risk mitigation exceeds maxLength 160", kind: "Project", wantProblem: true, wantSubstr: "maxLength",
			yaml: projectBase + minimalSummary + "  risks:\n    - description: Vendor delay\n      type: risk\n      mitigation: " + over(160) + "\n",
		},
		{
			name: "successCriteria statement exceeds maxLength 160", kind: "Project", wantProblem: true, wantSubstr: "maxLength",
			yaml: projectBase + minimalSummary + "  successCriteria:\n    - id: sc-1\n      metric: business\n      confirmedBy: {external: Sponsor}\n      when: atClosing\n      statement: " + over(160) + "\n",
		},
		{
			name: "successCriteria standard exceeds maxLength 60", kind: "Project", wantProblem: true, wantSubstr: "maxLength",
			yaml: projectBase + minimalSummary + "  successCriteria:\n    - id: sc-1\n      statement: Coverage is close to complete\n      metric: business\n      confirmedBy: {external: Sponsor}\n      when: atClosing\n      standard: " + over(60) + "\n",
		},
		{
			// confirmedBy is a reference now, so its length rule lives on
			// the external form, which is the only variant that carries
			// free text: the other two carry a Slug.
			name: "successCriteria confirmedBy external exceeds maxLength 80", kind: "Project", wantProblem: true, wantSubstr: "maxLength",
			yaml: projectBase + minimalSummary + "  successCriteria:\n    - id: sc-1\n      statement: Coverage is close to complete\n      metric: business\n      when: atClosing\n      confirmedBy: {external: " + over(80) + "}\n",
		},
	})
}
