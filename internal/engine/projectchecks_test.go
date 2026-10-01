package engine_test

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
)

// I3a (2026-09-17): GET /manifests/Project/{id}/checks. Every rule is
// table-tested here with at least one passing and one failing fixture, per
// the card's own requirement. fullProjectYAML below builds a project that
// is green on every check the card lists (Blocking == 0, every emitted item
// ok or an intentionally-left warn); individual tests then start from a
// variant with exactly one thing missing.

// fullProjectYAML returns a Project manifest, complete enough to pass
// every blocking check, referencing the seededEngine fixtures (t1, p1,
// d1, c1, g1, k1, bg1, ex1).
func fullProjectYAML(id string) string {
	return "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: " + id + "\n  name: Full Project\nspec:\n" +
		"  team: t1\n" +
		"  mandate:\n    - kind: decision\n      title: Approved to proceed\n" +
		"  summary:\n" +
		"    problems:\n      - problem: {situation: Quality issues surface too late}\n" +
		"        change: {what: Every order is checked early}\n" +
		"    scopeIn: [Checking every order]\n" +
		"    scopeOut: [Checking orders already sold]\n" +
		"    beneficiaries:\n      - group: bg1\n" +
		"  funding:\n    - amount: 1000\n      currency: USD\n      status: approved\n" +
		"  alignment:\n    goals: [g1-f]\n" +
		"  objectives:\n" +
		"    - objective: Every order is checked\n" +
		"      keyResults:\n" +
		"        - id: kr-1\n" +
		"          metric: Orders checked\n" +
		"          direction: increase\n" +
		"          kind: count\n" +
		"          unit: orders\n" +
		"          baseline: {value: 0, date: \"2025-09\"}\n" +
		"          target: {value: 100, date: \"2026-06\"}\n" +
		"          source: d1\n" +
		"  kpis:\n    - {kpi: k1, reason: The standard is what the rate counts.}\n" +
		"  deliverables:\n    - id: dv-1\n      name: Training pack\n      acceptance: [{by: {external: Quality reviewer}, outcome: signs it off}]\n" +
		"  successCriteria:\n" +
		"    - id: sc-1\n      statement: Coverage is close to complete\n      metric: business\n      standard: 85 percent\n      source: d1\n      cycle: c1\n      owner: {external: Quality reviewer}\n      confirmedBy: {external: Quality reviewer}\n      when: atClosing\n" +
		"    - id: sc-2\n      statement: The outcome is met\n      metric: business\n      standard: 90 percent\n      source: d1\n      cycle: c1\n      owner: {external: Quality reviewer}\n      confirmedBy: {external: Quality reviewer}\n      when: atLanding\n" +
		"  operation: \"new\"\n" +
		"  data:\n" +
		"    consumes:\n      - source: d1\n        purpose: Identify records\n        handoff: apiOrFeed\n" +
		"    produces:\n      - output: recordsInExistingSource\n        sink: d1\n        purpose: Record results\n        personalData: sensitive\n" +
		"  timeline:\n    start: \"2025-09\"\n    phases:\n      - name: Pilot\n        months: 6\n" +
		"  risks:\n    - id: r-1\n      description: Something might go wrong\n      type: risk\n      mitigation: Mitigate it\n      escalate: {flag: true, reason: Needs board attention}\n" +
		"  compliance:\n    - item: Data protection review\n      status: inProgress\n" +
		"  resources:\n" +
		"    - {role: sponsor}\n" +
		"    - {role: manager}\n" +
		"    - {role: teamMember}\n" +
		"    - {role: serviceOwner}\n" +
		"    - {role: teamMember}\n"
}

func mustCommit(t *testing.T, e *engine.Engine, kind, id, actor, y string) {
	t.Helper()
	if _, err := e.Commit(context.Background(), kind, id, []byte(y), actor, "seed"); err != nil {
		t.Fatalf("commit %s/%s: %v", kind, id, err)
	}
}

// baseSpecYAML is the minimal valid Project spec body (the two required
// summary fields), with extra appended verbatim.
const baseSpecYAML = "  team: t1\n  summary:\n    problems:\n      - problem: {situation: A gap}\n        change: {what: No more gap}\n"

// projectYAML builds a full Project manifest for id from baseSpecYAML plus
// extra spec-level YAML lines.
func projectYAML(id, extra string) string {
	return "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: " + id + "\n  name: P\nspec:\n" + baseSpecYAML + extra
}

func checksByID(items []engine.ProjectCheckItem) map[string]engine.ProjectCheckItem {
	out := make(map[string]engine.ProjectCheckItem, len(items))
	for _, it := range items {
		out[it.ID] = it
	}
	return out
}

func TestProjectChecksAllGreen(t *testing.T) {
	e := seededEngine(t)
	ctx := context.Background()
	mustCommit(t, e, "Project", "proj-green", "p1", fullProjectYAML("proj-green"))

	checks, err := e.ProjectChecks(ctx, "proj-green", false)
	if err != nil {
		t.Fatal(err)
	}
	if checks.Blocking != 0 {
		t.Fatalf("expected zero blocking items, got %d: %+v", checks.Blocking, checks.Items)
	}
	if checks.DerivedEnd != "2026-03" {
		t.Fatalf("expected derivedEnd 2026-03, got %q", checks.DerivedEnd)
	}
	byID := checksByID(checks.Items)
	for _, id := range []string{
		"goals-aligned", "aim-mandate", "goals-functional-level", "goals-objective", "goals-key-results-count",
		"goals-key-results-baseline", "goals-key-results-target", "goals-key-results-source",
		"measures-kpis", "aim-problem-change", "scope-in",
		"resources-funding", "deliverables-count", "deliverables-acceptance", "deliverables-verifier",
		"beneficiaries-named", "timeline-start-phases", "resources-sponsor-lead",
		"resources-mapped", "data-personal-data", "risks-mitigation",
		"risks-escalated", "closing-criteria", "landing-operation", "landing-criteria", "landing-owner",
	} {
		item, ok := byID[id]
		if !ok {
			t.Fatalf("expected check %q to be present, items: %+v", id, checks.Items)
		}
		if item.State == "block" {
			t.Fatalf("check %q unexpectedly blocking: %+v", id, item)
		}
	}
	if byID["data-personal-data"].State != "ok" {
		t.Fatalf("expected data-personal-data ok, got %+v", byID["data-personal-data"])
	}
	if _, present := byID["data-handoff"]; present {
		t.Fatalf("expected no data-handoff item when nothing is manual re-entry, got %+v", byID["data-handoff"])
	}
	if _, present := byID["data-custodian"]; present {
		t.Fatalf("expected no data-custodian item at all, got %+v", byID["data-custodian"])
	}
	if byID["resources-mapped"].Message != "0 of 5 roles named from the resource catalogue." {
		t.Fatalf("expected resources-mapped to count fullProjectYAML's 5 free-text roles, got %+v", byID["resources-mapped"])
	}
	// fullProjectYAML aligns to g1-f, a functional goal (seededEngine): should be ok (I3.4a).
	if byID["goals-functional-level"].State != "ok" {
		t.Fatalf("expected goals-functional-level ok (g1-f is functional), got %+v", byID["goals-functional-level"])
	}
}

func TestProjectChecksGoalsSection(t *testing.T) {
	e := seededEngine(t)
	ctx := context.Background()
	mustCommit(t, e, "Project", "proj-no-goal", "p1", "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: proj-no-goal\n  name: P\nspec:\n  team: t1\n  summary:\n    problems:\n      - problem: {situation: A gap}\n        change: {what: No more gap}\n")

	checks, err := e.ProjectChecks(ctx, "proj-no-goal", false)
	if err != nil {
		t.Fatal(err)
	}
	byID := checksByID(checks.Items)
	if byID["goals-aligned"].State != "block" {
		t.Fatalf("expected goals-aligned block, got %+v", byID["goals-aligned"])
	}
	if byID["aim-mandate"].State != "warn" {
		t.Fatalf("expected aim-mandate warn, got %+v", byID["aim-mandate"])
	}

	mustCommit(t, e, "Project", "proj-goal", "p1", "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: proj-goal\n  name: P\nspec:\n  team: t1\n  summary:\n    problems:\n      - problem: {situation: A gap}\n        change: {what: No more gap}\n  alignment:\n    goals: [g1-f]\n  mandate:\n    - kind: decision\n      title: Approved\n")
	checks, err = e.ProjectChecks(ctx, "proj-goal", false)
	if err != nil {
		t.Fatal(err)
	}
	byID = checksByID(checks.Items)
	if byID["goals-aligned"].State != "ok" || byID["aim-mandate"].State != "ok" {
		t.Fatalf("expected both ok, got %+v", checks.Items)
	}
}

// TestProjectChecksGoalsStrategicLevel covers card §2's own rule directly:
// aligning only to a pillar warns; aligning to at least one strategic goal
// (even alongside a pillar) is ok.
func TestProjectChecksGoalsStrategicLevel(t *testing.T) {
	e := seededEngine(t)
	ctx := context.Background()
	// g1-f (seededEngine) is already a functional goal. Create a non-functional goal too.
	mustCommit(t, e, "Goal", "g-strat-bad", "p1", "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: g-strat-bad\n  name: Strategic Goal (bad alignment)\nspec:\n  level: objective\n  parent: g1\n  objective: A strategic aim\n")

	mustCommit(t, e, "Project", "proj-functional-only", "p1", "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: proj-functional-only\n  name: P\nspec:\n  team: t1\n  summary:\n    problems:\n      - problem: {situation: A gap}\n        change: {what: No more gap}\n  alignment:\n    goals: [g1-f]\n")
	checks, err := e.ProjectChecks(ctx, "proj-functional-only", false)
	if err != nil {
		t.Fatal(err)
	}
	if got := checksByID(checks.Items)["goals-functional-level"].State; got != "ok" {
		t.Fatalf("expected ok aligning to a functional goal, got %s", got)
	}

	mustCommit(t, e, "Project", "proj-non-functional", "p1", "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: proj-non-functional\n  name: P\nspec:\n  team: t1\n  summary:\n    problems:\n      - problem: {situation: A gap}\n        change: {what: No more gap}\n  alignment:\n    goals: [g1, g-strat-bad]\n")
	checks, err = e.ProjectChecks(ctx, "proj-non-functional", false)
	if err != nil {
		t.Fatal(err)
	}
	if got := checksByID(checks.Items)["goals-functional-level"].State; got != "block" {
		t.Fatalf("expected block aligning to non-functional goals, got %s", got)
	}
}

func TestProjectChecksAimSection(t *testing.T) {
	e := seededEngine(t)
	ctx := context.Background()

	mustCommit(t, e, "Project", "proj-aim", "p1", projectYAML("proj-aim", ""))
	checks, err := e.ProjectChecks(ctx, "proj-aim", false)
	if err != nil {
		t.Fatal(err)
	}
	if got := checksByID(checks.Items)["goals-objective"].State; got != "block" {
		t.Fatalf("expected goals-objective block with no objectives, got %s", got)
	}

	tooMany := "  objectives:\n    - objective: Do the thing\n      keyResults:\n" +
		"        - {id: kr-1, metric: A, direction: increase, kind: count, unit: x}\n" +
		"        - {id: kr-2, metric: B, direction: increase, kind: count, unit: x}\n" +
		"        - {id: kr-3, metric: C, direction: increase, kind: count, unit: x}\n" +
		"        - {id: kr-4, metric: D, direction: increase, kind: count, unit: x}\n"
	mustCommit(t, e, "Project", "proj-aim-toomany", "p1", projectYAML("proj-aim-toomany", tooMany))
	checks, err = e.ProjectChecks(ctx, "proj-aim-toomany", false)
	if err != nil {
		t.Fatal(err)
	}
	byID := checksByID(checks.Items)
	if byID["goals-key-results-count"].State != "block" {
		t.Fatalf("expected aim-key-results-count block for 4 key results, got %+v", byID["goals-key-results-count"])
	}
	for _, id := range []string{"goals-key-results-baseline", "goals-key-results-target", "goals-key-results-source"} {
		if byID[id].State != "block" {
			t.Fatalf("expected %s block (nothing filled in), got %+v", id, byID[id])
		}
	}
	// A KPI is named by the project, never by a key result (2026-09-26),
	// so nothing about KPIs is asked of a key result any more.
	if _, ok := byID["goals-kpi-relation"]; ok {
		t.Fatalf("goals-kpi-relation should be gone, got %+v", byID["goals-kpi-relation"])
	}

	complete := "  objectives:\n    - objective: Do the thing\n      keyResults:\n" +
		"        - id: kr-1\n          metric: A\n          direction: increase\n          kind: count\n          unit: x\n" +
		"          baseline: {value: 0, date: \"2025-09\"}\n          target: {value: 10, date: \"2026-01\"}\n          source: d1\n" +
		""
	mustCommit(t, e, "Project", "proj-aim-complete", "p1", projectYAML("proj-aim-complete", complete))
	checks, err = e.ProjectChecks(ctx, "proj-aim-complete", false)
	if err != nil {
		t.Fatal(err)
	}
	byID = checksByID(checks.Items)
	for _, id := range []string{
		"goals-objective", "goals-key-results-count", "goals-key-results-baseline",
		"goals-key-results-target", "goals-key-results-source",
	} {
		if byID[id].State != "ok" {
			t.Fatalf("expected %s ok, got %+v", id, byID[id])
		}
	}
}

func TestProjectChecksScopeSection(t *testing.T) {
	e := seededEngine(t)
	ctx := context.Background()

	mustCommit(t, e, "Project", "proj-scope-bad", "p1", "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: proj-scope-bad\n  name: P\nspec:\n  team: t1\n  summary:\n    problems:\n      - problem: {situation: A gap}\n        change: {what: No more gap}\n")
	checks, err := e.ProjectChecks(ctx, "proj-scope-bad", false)
	if err != nil {
		t.Fatal(err)
	}
	byID := checksByID(checks.Items)
	if byID["aim-problem-change"].State != "ok" {
		t.Fatalf("problem and change are both stated, expected ok, got %+v", byID["aim-problem-change"])
	}
	if byID["scope-in"].State != "warn" {
		t.Fatalf("expected scope-in warn, got %+v", byID["scope-in"])
	}
	if byID["resources-funding"].State != "warn" {
		t.Fatalf("expected resources-funding warn, got %+v", byID["resources-funding"])
	}

	full := "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: proj-scope-good\n  name: P\nspec:\n  team: t1\n  summary:\n    problems:\n      - problem: {situation: A gap}\n        change: {what: No more gap}\n    scopeIn: [Doing the thing]\n  funding:\n    - {amount: 100, currency: USD, status: approved}\n"
	mustCommit(t, e, "Project", "proj-scope-good", "p1", full)
	checks, err = e.ProjectChecks(ctx, "proj-scope-good", false)
	if err != nil {
		t.Fatal(err)
	}
	byID = checksByID(checks.Items)
	if byID["scope-in"].State != "ok" || byID["resources-funding"].State != "ok" {
		t.Fatalf("expected both ok, got %+v", checks.Items)
	}
}

func TestProjectChecksDeliverablesSection(t *testing.T) {
	e := seededEngine(t)
	ctx := context.Background()

	mustCommit(t, e, "Project", "proj-dv", "p1", projectYAML("proj-dv", ""))
	checks, err := e.ProjectChecks(ctx, "proj-dv", false)
	if err != nil {
		t.Fatal(err)
	}
	if got := checksByID(checks.Items)["deliverables-count"].State; got != "block" {
		t.Fatalf("expected deliverables-count block, got %s", got)
	}

	noAcceptance := "  deliverables:\n    - {id: dv-1, name: Training pack}\n"
	mustCommit(t, e, "Project", "proj-dv-noacc", "p1", projectYAML("proj-dv-noacc", noAcceptance))
	checks, err = e.ProjectChecks(ctx, "proj-dv-noacc", false)
	if err != nil {
		t.Fatal(err)
	}
	byID := checksByID(checks.Items)
	if byID["deliverables-count"].State != "ok" {
		t.Fatalf("expected deliverables-count ok, got %+v", byID["deliverables-count"])
	}
	if byID["deliverables-acceptance"].State != "warn" {
		t.Fatalf("expected deliverables-acceptance warn, got %+v", byID["deliverables-acceptance"])
	}

	withAcceptance := "  deliverables:\n    - {id: dv-1, name: Training pack, acceptance: [{by: {external: Quality reviewer}, outcome: signs it off}]}\n"
	mustCommit(t, e, "Project", "proj-dv-acc", "p1", projectYAML("proj-dv-acc", withAcceptance))
	checks, err = e.ProjectChecks(ctx, "proj-dv-acc", false)
	if err != nil {
		t.Fatal(err)
	}
	if got := checksByID(checks.Items)["deliverables-acceptance"].State; got != "ok" {
		t.Fatalf("expected deliverables-acceptance ok, got %s", got)
	}
}

// Beneficiaries are qualitative: the one thing worth checking is whether
// any group is named, and naming one settles the section.
func TestProjectChecksBeneficiariesSection(t *testing.T) {
	e := seededEngine(t)
	ctx := context.Background()

	mustCommit(t, e, "Project", "proj-ben", "p1", projectYAML("proj-ben", ""))
	checks, err := e.ProjectChecks(ctx, "proj-ben", false)
	if err != nil {
		t.Fatal(err)
	}
	byID := checksByID(checks.Items)
	if got := byID["beneficiaries-named"].State; got != "warn" {
		t.Fatalf("expected beneficiaries-named warn, got %s", got)
	}
	// Nothing counts beneficiaries any more, here or anywhere.
	if _, ok := byID["beneficiaries-basis"]; ok {
		t.Fatalf("beneficiaries-basis should be gone, got %+v", checks.Items)
	}

	named := "    beneficiaries:\n      - {group: bg1}\n      - {group: bg2}\n"
	mustCommit(t, e, "Project", "proj-ben-named", "p1", projectYAML("proj-ben-named", named))
	checks, err = e.ProjectChecks(ctx, "proj-ben-named", false)
	if err != nil {
		t.Fatal(err)
	}
	byID = checksByID(checks.Items)
	if byID["beneficiaries-named"].State != "ok" {
		t.Fatalf("named groups should be ok, got %+v", checks.Items)
	}
	if byID["beneficiaries-named"].Message != "2 beneficiary groups." {
		t.Fatalf("expected the group count in the message, got %q", byID["beneficiaries-named"].Message)
	}
}

func TestProjectChecksTimelineSection(t *testing.T) {
	e := seededEngine(t)
	ctx := context.Background()

	mustCommit(t, e, "Project", "proj-tl-missing", "p1", "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: proj-tl-missing\n  name: P\nspec:\n  team: t1\n  summary:\n    problems:\n      - problem: {situation: A gap}\n        change: {what: No more gap}\n")
	checks, err := e.ProjectChecks(ctx, "proj-tl-missing", false)
	if err != nil {
		t.Fatal(err)
	}
	if got := checksByID(checks.Items)["timeline-start-phases"].State; got != "block" {
		t.Fatalf("expected timeline-start-phases block, got %s", got)
	}

	mustCommit(t, e, "Project", "proj-tl-set", "p1", "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: proj-tl-set\n  name: P\nspec:\n  team: t1\n  summary:\n    problems:\n      - problem: {situation: A gap}\n        change: {what: No more gap}\n  timeline:\n    start: \"2025-01\"\n    phases:\n      - {name: Pilot, months: 4}\n")
	checks, err = e.ProjectChecks(ctx, "proj-tl-set", false)
	if err != nil {
		t.Fatal(err)
	}
	byID := checksByID(checks.Items)
	if byID["timeline-start-phases"].State != "ok" {
		t.Fatalf("expected timeline-start-phases ok, got %+v", byID["timeline-start-phases"])
	}
	if checks.DerivedEnd != "2025-05" {
		t.Fatalf("expected derivedEnd 2025-05, got %q", checks.DerivedEnd)
	}
}

// Resources is one step for everything the project needs around it: the
// roles it names, the funding envelope, and the stakeholders it must keep
// close. There is no per-section accountability: a RACI's rows are
// deliverables and decisions, and a deliverable's own acceptance criteria
// already name the role that verifies each one.
func TestProjectChecksResourcesSection(t *testing.T) {
	e := seededEngine(t)
	ctx := context.Background()
	mustCommit(t, e, "Project", "proj-resources", "p1", "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: proj-resources\n  name: P\nspec:\n  team: t1\n  summary:\n    problems:\n      - problem: {situation: A gap}\n        change: {what: No more gap}\n")

	checks, err := e.ProjectChecks(ctx, "proj-resources", false)
	if err != nil {
		t.Fatal(err)
	}
	byID := checksByID(checks.Items)
	if byID["resources-sponsor-lead"].State != "block" {
		t.Fatalf("expected resources-sponsor-lead block with no resources at all, got %+v", byID["resources-sponsor-lead"])
	}
	if _, present := byID["resources-accountable"]; present {
		t.Fatalf("per-section accountability is not a charter concern; the check should be gone, got %+v", byID["resources-accountable"])
	}
	if byID["resources-funding"].State != "warn" {
		t.Fatalf("expected resources-funding warn, got %+v", byID["resources-funding"])
	}
	if byID["resources-mapped"].State != "ok" || byID["resources-mapped"].Message != "0 of 0 roles named from the resource catalogue." {
		t.Fatalf("expected resources-mapped ok with no roles, got %+v", byID["resources-mapped"])
	}

	// A sponsor, a lead and an approved envelope: nothing here blocks any
	// more once the two roles the charter cannot do without are named.
	full := "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: proj-resources\n  name: P\nspec:\n  team: t1\n" +
		"  summary:\n    problems:\n      - problem: {situation: A gap}\n        change: {what: No more gap}\n" +
		"  funding:\n    - {amount: 100, currency: USD, status: approved}\n" +
		"  resources:\n    - {role: sponsor}\n    - {role: manager}\n    - {role: teamMember}\n"
	mustCommit(t, e, "Project", "proj-resources", "p1", full)
	checks, err = e.ProjectChecks(ctx, "proj-resources", false)
	if err != nil {
		t.Fatal(err)
	}
	byID = checksByID(checks.Items)
	for _, id := range []string{"resources-sponsor-lead", "resources-funding"} {
		if byID[id].State != "ok" {
			t.Fatalf("expected %s ok, got %+v", id, byID[id])
		}
	}
	if byID["resources-mapped"].Message != "0 of 3 roles named from the resource catalogue." {
		t.Fatalf("expected resources-mapped to count the 3 free-text roles, got %+v", byID["resources-mapped"])
	}

	// A role picked from the catalogue is counted as named. A stakeholder
	// carries no tier here at all now: how much power one holds is scored
	// on the StakeholderMap scoped to this project, not on the row.
	mapped := "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: proj-resources\n  name: P\nspec:\n  team: t1\n" +
		"  summary:\n    problems:\n      - problem: {situation: A gap}\n        change: {what: No more gap}\n" +
		"  funding:\n    - {amount: 100, currency: USD, status: approved}\n" +
		"  resources:\n    - {role: sponsor, resource: r1}\n    - {role: manager}\n    - {role: teamMember, resource: r1}\n"
	mustCommit(t, e, "Project", "proj-resources", "p1", mapped)
	checks, err = e.ProjectChecks(ctx, "proj-resources", false)
	if err != nil {
		t.Fatal(err)
	}
	byID = checksByID(checks.Items)
	if byID["resources-mapped"].Message != "2 of 3 roles named from the resource catalogue." {
		t.Fatalf("expected one mapped role to be counted, got %+v", byID["resources-mapped"])
	}
	// The project no longer answers how a stakeholder is placed: that is a
	// fact about the link, held on the StakeholderMap scoped to it, so no
	// check here can pass or fail on it.
	// A project answers nothing about stakeholders now: not whether any
	// are named, and not how they are placed. Both are the map's.
	for _, gone := range []string{"resources-stakeholder", "resources-stakeholder-tier"} {
		if _, present := byID[gone]; present {
			t.Fatalf("expected no %s on the project: stakeholders are the map's", gone)
		}
	}
}

func TestProjectChecksRisksSection(t *testing.T) {
	e := seededEngine(t)
	ctx := context.Background()

	noMitigation := "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: proj-risks-nomit\n  name: P\nspec:\n  team: t1\n  summary:\n    problems:\n      - problem: {situation: A gap}\n        change: {what: No more gap}\n  risks:\n    - {description: Something might happen, type: risk}\n"
	mustCommit(t, e, "Project", "proj-risks-nomit", "p1", noMitigation)
	checks, err := e.ProjectChecks(ctx, "proj-risks-nomit", false)
	if err != nil {
		t.Fatal(err)
	}
	byID := checksByID(checks.Items)
	if byID["risks-mitigation"].State != "warn" {
		t.Fatalf("expected risks-mitigation warn, got %+v", byID["risks-mitigation"])
	}
	if byID["risks-escalated"].State != "ok" {
		t.Fatalf("risks-escalated is always ok (informational), got %+v", byID["risks-escalated"])
	}

	withMitigationAndEscalation := "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: proj-risks-good\n  name: P\nspec:\n  team: t1\n  summary:\n    problems:\n      - problem: {situation: A gap}\n        change: {what: No more gap}\n  risks:\n    - {description: Something might happen, type: risk, mitigation: Watch for it, escalate: {flag: true, reason: Needs attention}}\n"
	mustCommit(t, e, "Project", "proj-risks-good", "p1", withMitigationAndEscalation)
	checks, err = e.ProjectChecks(ctx, "proj-risks-good", false)
	if err != nil {
		t.Fatal(err)
	}
	byID = checksByID(checks.Items)
	if byID["risks-mitigation"].State != "ok" {
		t.Fatalf("expected risks-mitigation ok, got %+v", byID["risks-mitigation"])
	}
}

func TestProjectChecksClosingAndLandingSection(t *testing.T) {
	e := seededEngine(t)
	ctx := context.Background()

	mustCommit(t, e, "Project", "proj-cl-none", "p1", "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: proj-cl-none\n  name: P\nspec:\n  team: t1\n  summary:\n    problems:\n      - problem: {situation: A gap}\n        change: {what: No more gap}\n")
	checks, err := e.ProjectChecks(ctx, "proj-cl-none", false)
	if err != nil {
		t.Fatal(err)
	}
	byID := checksByID(checks.Items)
	if byID["closing-criteria"].State != "block" {
		t.Fatalf("expected closing-criteria block, got %+v", byID["closing-criteria"])
	}
	if byID["landing-operation"].State != "block" {
		t.Fatalf("expected landing-operation block, got %+v", byID["landing-operation"])
	}
	if byID["landing-criteria"].State != "block" {
		t.Fatalf("expected landing-criteria block, got %+v", byID["landing-criteria"])
	}
	if byID["landing-owner"].State != "warn" {
		t.Fatalf("expected landing-owner warn, got %+v", byID["landing-owner"])
	}
	if byID["landing-owner"].Fix.Section != "resources" || byID["landing-owner"].Fix.Phase != "initiation" {
		t.Fatalf("expected landing-owner's fix to point at initiation/resources, got %+v", byID["landing-owner"].Fix)
	}

	// closing-criteria reads the deliverables' own acceptance criteria
	// since 2026-09-27, not a second list written on the closing step.
	good := "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: proj-cl-good\n  name: P\nspec:\n  team: t1\n  summary:\n    problems:\n      - problem: {situation: A gap}\n        change: {what: No more gap}\n  operation: \"new\"\n" +
		"  deliverables:\n    - {id: dv-1, name: Training pack, acceptance: [{by: {external: Quality reviewer}, outcome: signs it off}]}\n" +
		"  successCriteria:\n" +
		"    - {id: sc-1, statement: A, metric: compliance, confirmedBy: {external: Sponsor}, when: atClosing}\n" +
		"    - {id: sc-2, statement: B, metric: business, confirmedBy: {external: Sponsor}, when: atLanding}\n" +
		"  resources:\n    - {role: serviceOwner}\n"
	mustCommit(t, e, "Project", "proj-cl-good", "p1", good)
	checks, err = e.ProjectChecks(ctx, "proj-cl-good", false)
	if err != nil {
		t.Fatal(err)
	}
	byID = checksByID(checks.Items)
	for _, id := range []string{"closing-criteria", "landing-operation", "landing-criteria", "landing-owner"} {
		if byID[id].State != "ok" {
			t.Fatalf("expected %s ok, got %+v", id, byID[id])
		}
	}

	// "postClosingCycle" also satisfies landing-criteria.
	cycling := "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: proj-cl-cycle\n  name: P\nspec:\n  team: t1\n  summary:\n    problems:\n      - problem: {situation: A gap}\n        change: {what: No more gap}\n  successCriteria:\n" +
		"    - {id: sc-1, statement: A, metric: business, confirmedBy: {external: Sponsor}, when: postClosingCycle}\n"
	mustCommit(t, e, "Project", "proj-cl-cycle", "p1", cycling)
	checks, err = e.ProjectChecks(ctx, "proj-cl-cycle", false)
	if err != nil {
		t.Fatal(err)
	}
	if got := checksByID(checks.Items)["landing-criteria"].State; got != "ok" {
		t.Fatalf("expected landing-criteria ok for each cycle after closing, got %s", got)
	}
}

func TestProjectChecksNotFound(t *testing.T) {
	e := seededEngine(t)
	if _, err := e.ProjectChecks(context.Background(), "does-not-exist", false); err == nil {
		t.Fatal("expected an error for a project that does not exist")
	}
}

func TestDerivedEndComputation(t *testing.T) {
	e := seededEngine(t)
	ctx := context.Background()
	cases := []struct {
		name   string
		start  string
		months []int
		want   string
	}{
		{"single phase within year", "2025-01", []int{3}, "2025-04"},
		{"crosses a year boundary", "2025-09", []int{9}, "2026-06"},
		{"several phases sum", "2025-09", []int{3, 6}, "2026-06"},
		{"ends in december", "2025-01", []int{11}, "2025-12"},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			id := "proj-derived-" + string(rune('a'+i))
			phases := ""
			for _, m := range c.months {
				phases += "      - {name: Phase, months: " + strconv.Itoa(m) + "}\n"
			}
			y := "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: " + id + "\n  name: P\nspec:\n  team: t1\n  summary:\n    problems:\n      - problem: {situation: A gap}\n        change: {what: No more gap}\n  timeline:\n    start: \"" + c.start + "\"\n    phases:\n" + phases
			mustCommit(t, e, "Project", id, "p1", y)
			checks, err := e.ProjectChecks(ctx, id, false)
			if err != nil {
				t.Fatal(err)
			}
			if checks.DerivedEnd != c.want {
				t.Fatalf("start %s months %v: expected %s, got %s", c.start, c.months, c.want, checks.DerivedEnd)
			}
		})
	}
}

// A key result's baseline, target and source are three separate facts, and
// each is reported on its own. One check covering all three reported "a
// baseline, a target or a source is missing" at a key result whose
// baseline was right there, which is worse than saying nothing: the
// message pointed at the one part that was already done.
func TestProjectChecksNameTheMissingPartOfAKeyResult(t *testing.T) {
	e := seededEngine(t)
	ctx := context.Background()

	// A baseline of zero is a baseline. Only the source is missing here.
	spec := "  objectives:\n    - objective: Map the process\n      keyResults:\n" +
		"        - id: kr-1\n          metric: process maps published\n          direction: reach\n          kind: count\n          unit: process maps\n" +
		"          baseline: {value: 0, date: \"2025-09\"}\n          target: {value: 5, date: \"2027-01\"}\n"
	mustCommit(t, e, "Project", "proj-parts", "p1", projectYAML("proj-parts", spec))

	checks, err := e.ProjectChecks(ctx, "proj-parts", false)
	if err != nil {
		t.Fatal(err)
	}
	byID := checksByID(checks.Items)

	if got := byID["goals-key-results-baseline"]; got.State != "ok" {
		t.Fatalf("a baseline of zero is a baseline; expected ok, got %+v", got)
	}
	if got := byID["goals-key-results-target"]; got.State != "ok" {
		t.Fatalf("expected the target check ok, got %+v", got)
	}
	if got := byID["goals-key-results-source"]; got.State != "block" {
		t.Fatalf("expected the source check to block, got %+v", got)
	}
	for _, id := range []string{"goals-key-results-baseline", "goals-key-results-target"} {
		if strings.Contains(byID[id].Message, "source") {
			t.Fatalf("check %q should not mention the source: %q", id, byID[id].Message)
		}
	}
	if strings.Contains(byID["goals-key-results-source"].Message, "baseline") {
		t.Fatalf("the source check should not mention a baseline: %q", byID["goals-key-results-source"].Message)
	}

	// An admitted unknown counts as a baseline too.
	unknown := "  objectives:\n    - objective: Map the process\n      keyResults:\n" +
		"        - id: kr-1\n          metric: process maps published\n          direction: reach\n          kind: count\n          unit: process maps\n" +
		"          baseline: {unknownReason: the first count runs in March}\n          target: {value: 5, date: \"2027-01\"}\n          source: d1\n"
	mustCommit(t, e, "Project", "proj-unknown", "p1", projectYAML("proj-unknown", unknown))
	checks, err = e.ProjectChecks(ctx, "proj-unknown", false)
	if err != nil {
		t.Fatal(err)
	}
	if got := checksByID(checks.Items)["goals-key-results-baseline"]; got.State != "ok" {
		t.Fatalf("an admitted unknown is an answer; expected ok, got %+v", got)
	}
}

// A deliverable can need more than one signature, and a test nobody owns
// is a test nobody applies: each acceptance criterion names the role that
// verifies it, and the check says how many still do not.
func TestProjectChecksDeliverableVerifiers(t *testing.T) {
	e := seededEngine(t)
	ctx := context.Background()

	mixed := "  deliverables:\n" +
		"    - id: dv-1\n      name: Training pack\n      acceptance:\n" +
		"        - {by: {external: Quality reviewer}, outcome: signs it off}\n" +
		"        - {outcome: is published to every depot}\n" +
		"    - id: dv-2\n      name: Reporting view\n      acceptance:\n" +
		"        - {outcome: matches the underlying spreadsheet}\n"
	mustCommit(t, e, "Project", "proj-verify", "p1", projectYAML("proj-verify", mixed))

	checks, err := e.ProjectChecks(ctx, "proj-verify", false)
	if err != nil {
		t.Fatal(err)
	}
	byID := checksByID(checks.Items)
	if byID["deliverables-acceptance"].State != "ok" {
		t.Fatalf("both deliverables say what makes them accepted; expected ok, got %+v", byID["deliverables-acceptance"])
	}
	if got := byID["deliverables-verifier"]; got.State != "warn" || !strings.Contains(got.Message, "2 acceptance criteria") {
		t.Fatalf("expected a warn naming the two criteria with no verifier, got %+v", got)
	}

	named := "  deliverables:\n" +
		"    - id: dv-1\n      name: Training pack\n      acceptance:\n" +
		"        - {by: {external: Quality reviewer}, outcome: signs it off}\n" +
		"        - {by: {external: Depot lead}, outcome: confirms it can be run as written}\n"
	mustCommit(t, e, "Project", "proj-verified", "p1", projectYAML("proj-verified", named))
	checks, err = e.ProjectChecks(ctx, "proj-verified", false)
	if err != nil {
		t.Fatal(err)
	}
	if got := checksByID(checks.Items)["deliverables-verifier"]; got.State != "ok" {
		t.Fatalf("expected ok once every criterion names a verifier, got %+v", got)
	}
}

// A project may answer more than one problem, each felt by its own
// beneficiary groups. The aim check counts
// the pairs that are whole, refuses a pair with only one half written,
// and warns about a problem stated about nobody.
func TestProjectChecksAimProblems(t *testing.T) {
	e := seededEngine(t)
	ctx := context.Background()

	two := "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: proj-aim-two\n  name: P\nspec:\n  team: t1\n" +
		"  summary:\n    problems:\n" +
		"      - problem: {situation: A gap}\n        change: {what: No more gap}\n        groups: [bg1]\n" +
		"      - problem: {situation: Another gap}\n        change: {what: No more of that either}\n        groups: [bg1]\n"
	mustCommit(t, e, "Project", "proj-aim-two", "p1", two)
	checks, err := e.ProjectChecks(ctx, "proj-aim-two", false)
	if err != nil {
		t.Fatal(err)
	}
	byID := checksByID(checks.Items)
	if byID["aim-problem-change"].State != "ok" {
		t.Fatalf("two whole pairs, expected ok, got %+v", byID["aim-problem-change"])
	}
	if !strings.Contains(byID["aim-problem-change"].Message, "2 problems") {
		t.Fatalf("expected the count in the message, got %q", byID["aim-problem-change"].Message)
	}
	if byID["aim-problem-groups"].State != "ok" {
		t.Fatalf("both problems name a group, expected ok, got %+v", byID["aim-problem-groups"])
	}

	// A pair with only one half is worse than no pair at all: it reads as
	// finished and is not.
	half := "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: proj-aim-half\n  name: P\nspec:\n  team: t1\n" +
		"  summary:\n    problems:\n" +
		"      - problem: {situation: A gap}\n        change: {what: No more gap}\n" +
		"      - problem: {situation: Another gap}\n        change: {what: \" \"}\n"
	mustCommit(t, e, "Project", "proj-aim-half", "p1", half)
	checks, err = e.ProjectChecks(ctx, "proj-aim-half", false)
	if err != nil {
		t.Fatal(err)
	}
	byID = checksByID(checks.Items)
	if byID["aim-problem-change"].State != "block" {
		t.Fatalf("expected block for a half-written pair, got %+v", byID["aim-problem-change"])
	}
	if byID["aim-problem-groups"].State != "warn" {
		t.Fatalf("expected a warn for problems naming no group, got %+v", byID["aim-problem-groups"])
	}
}

// Data that lands nowhere is data nobody can find again, so every output
// names the register, system or store that holds it afterwards.
func TestProjectChecksDataSink(t *testing.T) {
	e := seededEngine(t)
	ctx := context.Background()

	mustCommit(t, e, "Project", "proj-sink-ok", "p1", projectYAML("proj-sink-ok",
		"  data:\n    produces:\n      - output: documentsOrFiles\n        sink: d1\n        purpose: Evidence\n        personalData: none\n"))
	checks, err := e.ProjectChecks(ctx, "proj-sink-ok", false)
	if err != nil {
		t.Fatal(err)
	}
	if got := checksByID(checks.Items)["data-sink"]; got.State != "ok" {
		t.Fatalf("expected data-sink ok, got %+v", got)
	}

	// A committed manifest can never reach the check without a sink: the
	// schema refuses both a missing one and an empty one, so the check
	// above is the draft-time guard and this is the contract-time one.
	for _, y := range []string{
		projectYAML("proj-sink-none", "  data:\n    produces:\n      - output: documentsOrFiles\n        purpose: Evidence\n        personalData: none\n"),
		projectYAML("proj-sink-empty", "  data:\n    produces:\n      - output: documentsOrFiles\n        sink: \"\"\n        purpose: Evidence\n        personalData: none\n"),
	} {
		if _, err := e.Commit(ctx, "Project", "proj-sink-refused", []byte(y), "p1", "seed"); err == nil {
			t.Fatal("expected a produced output with nowhere to land to be refused")
		}
	}

	// The shape the section used to store is refused outright, so nothing
	// can quietly keep writing a produced "source".
	old := projectYAML("proj-sink-old", "  data:\n    produces:\n      - source: d1\n        purpose: Evidence\n        personalData: none\n")
	if _, err := e.Commit(ctx, "Project", "proj-sink-old", []byte(old), "p1", "seed"); err == nil {
		t.Fatal("expected the old produces shape to be refused")
	}
}
