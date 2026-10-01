package engine_test

import (
	"context"
	"testing"
)

// TAXONOMY.md D18: the check that would have caught the TTNLA answer
// sheets before they were printed.
func TestTurnaroundReadFromHandCapturedSourceIsAdvised(t *testing.T) {
	e := seededEngine(t)
	ctx := context.Background()
	source := func(capture string) string {
		y := "apiVersion: cartograph/v1\nkind: DataSource\nmetadata:\n  id: scripts\n  name: Answer scripts\nspec:\n  name: Answer scripts\n  category: paperRecord\n  team: t1\n"
		if capture != "" {
			y += "  capture: " + capture + "\n"
		}
		return y
	}
	project := projectYAML("assess", "  objectives:\n    - objective: Results reach offices sooner\n      keyResults:\n"+
		"        - {id: kr-1, metric: days from sitting to results, direction: decrease, kind: duration, unit: days,"+
		" baseline: {value: 120, date: \"2025-09\"}, target: {value: 30, date: \"2026-06\"}, source: scripts}\n")

	cases := []struct {
		capture, id, want string
	}{
		{"byHand", "data-capture-turnaround", "warn"},
		{"", "data-capture-turnaround", "warn"},
		{"notDecided", "data-capture-decided", "warn"},
		{"scannedForms", "data-capture-decided", "ok"},
	}
	for _, tc := range cases {
		mustCommit(t, e, "DataSource", "scripts", "p1", source(tc.capture))
		mustCommit(t, e, "Project", "assess", "p1", project)
		checks, err := e.ProjectChecks(ctx, "assess", false)
		if err != nil {
			t.Fatal(err)
		}
		got := checksByID(checks.Items)[tc.id]
		if got.State != tc.want {
			t.Errorf("capture %q: %s = %+v, want %s", tc.capture, tc.id, got, tc.want)
		}
	}
}

// The same target, once a component is the work that changes how records
// get in, is no longer advised: the component carries it.
func TestComponentThatChangesCaptureClearsTheTurnaroundAdvice(t *testing.T) {
	e := seededEngine(t)
	ctx := context.Background()
	mustCommit(t, e, "DataSource", "scripts", "p1", "apiVersion: cartograph/v1\nkind: DataSource\nmetadata:\n  id: scripts\n  name: Answer scripts\nspec:\n  name: Answer scripts\n  category: paperRecord\n  team: t1\n  capture: byHand\n")
	mustCommit(t, e, "Project", "assess", "p1", projectYAML("assess", "  objectives:\n    - objective: Results reach offices sooner\n      keyResults:\n"+
		"        - {id: kr-1, metric: days from sitting to results, direction: decrease, kind: duration, unit: days,"+
		" baseline: {value: 120, date: \"2025-09\"}, target: {value: 30, date: \"2026-06\"}, source: scripts}\n"))
	mustCommit(t, e, "Project", "omr", "p1", projectYAML("omr", "  alignment:\n    partOf: assess\n"+
		"  data:\n    produces:\n      - {output: recordsInExistingSource, sink: scripts, purpose: Scored answers, personalData: personal}\n"))

	checks, err := e.ProjectChecks(ctx, "assess", false)
	if err != nil {
		t.Fatal(err)
	}
	if got := checksByID(checks.Items)["data-capture-turnaround"]; got.State != "ok" {
		t.Fatalf("a component that writes into the source should carry the turnaround, got %+v", got)
	}
}
