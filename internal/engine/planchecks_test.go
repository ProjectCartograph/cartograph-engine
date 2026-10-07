package engine_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
)

// A target set when an event happens is complete until the month it is
// expected by passes; then it is flagged (TAXONOMY.md D47).
func TestATargetSetWhenAnEventHappens(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	ctx := context.Background()
	kr := func(expected string) string {
		return projectYAML("pending", "  objectives:\n    - id: o1\n      objective: Fewer sugary drinks\n      keyResults:\n"+
			"        - {id: kr1, metric: students drinking sugary drinks daily, direction: decrease, kind: percent,"+
			" baseline: {value: 49.4, date: \"2017-01\"},"+
			" target: {setWhen: {form: when, event: {on: {external: the survey baseline report}, happens: accepted}, expectedBy: \""+expected+"\"}}}\n")
	}
	mustCommit(t, e, "Project", "pending", "p1", kr("2099-12"))
	checks, err := e.ProjectChecks(ctx, "pending", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range checks.Items {
		if strings.Contains(c.ID, "target") && c.State != "ok" {
			t.Errorf("a pending target before its month: %+v", c)
		}
	}
	mustCommit(t, e, "Project", "pending", "p1", kr("2001-01"))
	checks, err = e.ProjectChecks(ctx, "pending", false)
	if err != nil {
		t.Fatal(err)
	}
	late := false
	for _, c := range checks.Items {
		late = late || (strings.Contains(c.ID, "target") && c.State != "ok")
	}
	if !late {
		t.Error("a pending target past its month is not flagged")
	}
}

// Milestones carry the schedule: each says when it falls, a loop is
// refused, and an unfunded cost needs the condition that decides it
// (TAXONOMY.md D48, D51, D52).
func TestMilestonesCostsAndSignOff(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	ctx := context.Background()
	base := "  milestones:\n" +
		"    - {id: m1, name: Guidelines issued, timing: {form: date, date: \"2025-12-31\"}}\n" +
		"    - {id: m2, name: Operators sensitised, timing: {form: after, event: {on: {local: milestones, id: m1}}, lagMonths: 2}}\n" +
		"    - {id: m3, name: Product list, timing: {form: window, notBefore: \"2027-04\", notAfter: \"2027-07\"}}\n" +
		"  costs:\n    - {id: c1, category: Survey field resources, status: beingCosted, condition: k1}\n" +
		"  conditions:\n    - {id: k1, action: Fund or defer the survey field resources, owner: {local: resources, id: sponsor}, due: {form: date, date: \"2026-10\"}}\n" +
		"  signOffs:\n    - {id: s1, stage: definition, label: Approved by, role: {local: resources, id: sponsor}}\n"
	mustCommit(t, e, "Project", "plan", "p1", projectYAML("plan", base))
	checks, err := e.ProjectChecks(ctx, "plan", false)
	if err != nil {
		t.Fatal(err)
	}
	got := checksByID(checks.Items)
	for _, id := range []string{"milestones-timing", "milestones-loop", "costs-funded", "conditions-due", "signoffs-present", "timeline-start-phases"} {
		if got[id].State != "ok" {
			t.Errorf("%s: %+v", id, got[id])
		}
	}

	// m1 waiting on m2 closes a loop.
	looped := strings.Replace(base, `timing: {form: date, date: "2025-12-31"}`, `timing: {form: after, event: {on: {local: milestones, id: m2}}}`, 1)
	looped = strings.Replace(looped, "condition: k1", "", 1)
	mustCommit(t, e, "Project", "plan", "p1", projectYAML("plan", looped))
	checks, err = e.ProjectChecks(ctx, "plan", false)
	if err != nil {
		t.Fatal(err)
	}
	got = checksByID(checks.Items)
	if got["milestones-loop"].State != "block" || !strings.Contains(got["milestones-loop"].Message, "Guidelines issued waits on Operators sensitised") {
		t.Errorf("loop: %+v", got["milestones-loop"])
	}
	if got["costs-funded"].State != "warn" {
		t.Errorf("an unfunded line with no condition: %+v", got["costs-funded"])
	}
}

// The engine records who entered an event, and keeps it (TAXONOMY.md D52).
func TestAnEventRecordsWhoEnteredIt(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	ev := func(by string) string {
		return projectYAML("log", "  events:\n    - {id: e1, on: {external: Policy issued}, happened: issued, date: \"2025-12-31\""+by+"}\n")
	}
	mustCommit(t, e, "Project", "log", "ada@example.org", ev(", recordedBy: someone-else"))
	mustCommit(t, e, "Project", "log", "bob@example.org", ev(""))
	v, err := e.Get(context.Background(), "Project", "log")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(v.YAML), "recordedBy: ada@example.org") {
		t.Fatalf("recorder not kept:\n%s", v.YAML)
	}
	_ = engine.Version{}
}

// The schedule places each milestone from its timing: an after takes what
// it follows plus its lag, and the chain to the last date is marked.
func TestTheScheduleFollowsWhatEachMilestoneWaitsOn(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	mustCommit(t, e, "Project", "sched", "p1", projectYAML("sched", "  milestones:\n"+
		"    - {id: m1, name: Issued, timing: {form: date, date: \"2025-12-31\"}}\n"+
		"    - {id: m2, name: Sensitised, timing: {form: after, event: {on: {local: milestones, id: m1}}, lagMonths: 2}}\n"+
		"    - {id: m3, name: Trained, timing: {form: after, event: {on: {local: milestones, id: m2}}, lagMonths: 4}}\n"+
		"    - {id: m4, name: Survey, timing: {form: when, event: {on: {external: ethics approval}}, expectedBy: \"2026-06\"}}\n"))
	items, err := e.Schedule(context.Background(), "sched")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]engine.ScheduleItem{}
	for _, it := range items {
		got[it.ID] = it
	}
	if got["m2"].Month != "2026-02" || got["m3"].Month != "2026-06" {
		t.Fatalf("placed %+v", items)
	}
	if !got["m1"].Critical || !got["m2"].Critical || !got["m3"].Critical || got["m4"].Critical {
		t.Errorf("critical chain %+v", items)
	}
	if !got["m4"].Pending || got["m4"].Month != "2026-06" {
		t.Errorf("pending %+v", got["m4"])
	}
}

// A target set when an event happens is held to the aim's horizon at the
// month it is expected by, like any dated target.
func TestAPendingTargetIsInsideTheHorizonByItsExpectedMonth(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	ctx := context.Background()
	mustCommit(t, e, "Goal", "o-pending", "local", "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: o-pending\n  name: Operators apply the rules\nspec:\n  level: goal\n  objective: Operators apply the rules.\n  horizon: {start: \"2026-09\", end: \"2027-07\"}\n"+
		"  keyResults:\n    - {id: kr1, metric: operators trained, kind: percent, direction: increase, baseline: {value: 0, date: \"2026-08\"},"+
		" target: {setWhen: {form: when, event: {on: {external: calendar confirmed}}, expectedBy: \"2026-11\"}}}\n")
	checks, err := e.GoalChecks(ctx, "o-pending")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range checks {
		if c.ID == "smart-time-bound" && c.State != "ok" {
			t.Errorf("a pending target inside the horizon: %+v", c)
		}
	}
}

// A milestone may wait on another project's milestone: the schedule
// places it from there, and the component's span counts from where it
// falls rather than reading as no time at all. Two projects waiting on
// each other leave the loop unplaced instead of placing forever.
func TestAMilestoneWaitsOnAnotherProjects(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	// The survey first, so the app can name it; then the survey's baseline
	// waits on the app.
	mustCommit(t, e, "Project", "survey", "p1", projectYAML("survey", "  milestones:\n"+
		"    - {id: s1, name: Approved, timing: {form: date, date: \"2026-08-20\"}}\n"))
	mustCommit(t, e, "Project", "app", "p1", projectYAML("app", "  milestones:\n"+
		"    - {id: a1, name: Digitised, timing: {form: after, event: {on: {kind: Project, id: survey}, item: s1}, lagMonths: 1}}\n"+
		"    - {id: a2, name: Field-ready, timing: {form: after, event: {on: {local: milestones, id: a1}}, lagMonths: 2}}\n"))
	mustCommit(t, e, "Project", "survey", "p2", projectYAML("survey", "  milestones:\n"+
		"    - {id: s1, name: Approved, timing: {form: date, date: \"2026-08-20\"}}\n"+
		"    - {id: s2, name: Baseline, timing: {form: after, event: {on: {kind: Project, id: app}, item: a2}}}\n"+
		"  components:\n    - {kind: Project, id: app}\n"))
	items, err := e.Schedule(context.Background(), "app")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Month != "2026-09" || items[1].Month != "2026-11" {
		t.Fatalf("placed %+v", items)
	}
	survey, err := e.Schedule(context.Background(), "survey")
	if err != nil || survey[1].Month != "2026-11" {
		t.Fatalf("the survey's baseline does not wait on the app: %+v (%v)", survey, err)
	}
	g, err := e.Components(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range g.Nodes {
		if n.Ref.ID == "app" && n.Months != 3 {
			t.Errorf("the app runs %d months, want 3 (September to November)", n.Months)
		}
	}

	mustCommit(t, e, "Project", "loop2", "p1", projectYAML("loop2", "  milestones:\n"+
		"    - {id: k1, name: Second, timing: {form: date, date: \"2026-01\"}}\n"))
	mustCommit(t, e, "Project", "loop", "p1", projectYAML("loop", "  milestones:\n"+
		"    - {id: l1, name: First, timing: {form: after, event: {on: {kind: Project, id: loop2}, item: k1}}}\n"))
	mustCommit(t, e, "Project", "loop2", "p2", projectYAML("loop2", "  milestones:\n"+
		"    - {id: k1, name: Second, timing: {form: after, event: {on: {kind: Project, id: loop}, item: l1}}}\n"))
	looped, err := e.Schedule(context.Background(), "loop")
	if err != nil || len(looped) != 1 || !looped[0].Unplaced {
		t.Fatalf("a loop between projects: %+v (%v)", looped, err)
	}
}
