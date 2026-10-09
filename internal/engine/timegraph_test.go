package engine_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
)

// A project's waits across kinds (TAXONOMY.md D47, D48): its milestones
// in a chain, a deliverable after one, a dependency on another project,
// and a KPI's target set when a milestone is reached; the chain that
// decides the last date, what no longer fits, and what a risk could move.
func TestAProjectsWaitsAcrossKinds(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	supplier := "  milestones:\n    - {id: live, name: Scales live, timing: {form: date, date: \"2027-06\"}}\n"
	waiter := "  milestones:\n" +
		"    - {id: start, name: Pilot starts, timing: {form: date, date: \"2026-10\"}}\n" +
		"    - {id: review, name: Pilot review, timing: {form: after, event: {on: {local: milestones, id: start}}, lagMonths: 2}}\n" +
		"  deliverables:\n" +
		"    - {id: report, name: Pilot report, due: {form: after, event: {on: {local: milestones, id: review}}, lagMonths: 1, risks: [r-late]}}\n" +
		"  risks:\n" +
		"    - {id: r-late, description: Graders are not trained in time, type: risk}\n" +
		"    - id: r-scales\n      description: Scales from the supplier project\n      type: dependency\n" +
		"      depends: {direction: needs, on: {kind: Project, id: supplier}, needed: {form: after, event: {on: {local: milestones, id: start}}, lagMonths: 1}}\n"
	mustCommit(t, e, "DataSource", "depot-logs", "local", "apiVersion: cartograph/v1\nkind: DataSource\nmetadata:\n  id: depot-logs\n  name: Depot logs\nspec:\n  name: Depot logs\n  category: paperRecord\n  team: t1\n")
	mustCommit(t, e, "Project", "supplier", "local", projectYAML("supplier", supplier))
	mustCommit(t, e, "Project", "waiter", "local", projectYAML("waiter", waiter))
	kpi := "apiVersion: cartograph/v1\nkind: KPI\nmetadata:\n  id: graded\n  name: Crates graded\nspec:\n  definition: Crates graded.\n  unit: count\n  direction: increase\n  sources: [depot-logs]\n" +
		"  target:\n    setWhen: {form: when, event: {on: {kind: Project, id: waiter}, item: review}, expectedBy: \"2027-02\"}\n"
	if _, err := e.Commit(context.Background(), "KPI", "graded", []byte(kpi), "local", "seed"); err != nil {
		var invalid *engine.ValidationError
		if errors.As(err, &invalid) {
			t.Fatalf("%v: %+v", err, invalid.Problems)
		}
		t.Fatal(err)
	}

	g, err := e.Waits(context.Background(), "waiter")
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]engine.TimeNode{}
	index := map[string]int{}
	for i, n := range g.Nodes {
		by[n.Kind+" "+n.Name] = n
		index[n.Kind+" "+n.Name] = i
	}
	want := map[string]string{
		"milestone Pilot starts": "2026-10", "milestone Pilot review": "2026-12", "deliverable Pilot report": "2027-01",
		"dependency Scales from the supplier project": "2026-11", "ready P": "2027-06", "target Crates graded": "2027-02",
	}
	for name, month := range want {
		if n, ok := by[name]; !ok || n.Month != month {
			t.Errorf("%s: %+v, want month %s (nodes %v)", name, n, month, g.Nodes)
		}
	}
	if !by["dependency Scales from the supplier project"].Conflict {
		t.Error("needed in November, ready in June: a conflict")
	}
	if r := by["deliverable Pilot report"].Risks; len(r) != 1 || r[0] != "Graders are not trained in time" {
		t.Errorf("the report's timing names the training risk: %v", r)
	}
	if f := by["target Crates graded"].Follows; f != "Pilot review" {
		t.Errorf("the target is set when the pilot review happens: %q", f)
	}
	edge := func(from, to string) bool {
		for _, ed := range g.Edges {
			if ed.From == index[from] && ed.To == index[to] {
				return true
			}
		}
		return false
	}
	for _, pair := range [][2]string{
		{"milestone Pilot starts", "milestone Pilot review"}, {"milestone Pilot review", "deliverable Pilot report"},
		{"ready P", "dependency Scales from the supplier project"}, {"milestone Pilot review", "target Crates graded"},
	} {
		if !edge(pair[0], pair[1]) {
			t.Errorf("no edge %s to %s: %+v", pair[0], pair[1], g.Edges)
		}
	}
	if !by["ready P"].Critical || by["milestone Pilot starts"].Critical {
		t.Errorf("the chain that decides the last date ends at the supplier being ready: %+v", g.Nodes)
	}
}
