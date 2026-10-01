package engine_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
)

// programmeYAML builds a Programme manifest with extra spec-level lines
// appended, the way projectYAML does for projects.
func programmeYAML(id, extra string) string {
	return fmt.Sprintf("apiVersion: cartograph/v1\nkind: Programme\nmetadata:\n  id: %s\n  name: %s\n"+
		"spec:\n  name: %s\n  aim: {change: Better outcomes}\n  leadTeam: t1\n%s", id, id, id, extra)
}

// dependsOn is the risk list carrying one declared edge. An edge validates
// only once its far end exists, so a cycle is written in two passes: bodies
// first, edges second.
func dependsOn(kind, id, needBy string) string {
	risk := "  risks:\n    - description: Cannot start without it\n      type: dependency\n" +
		fmt.Sprintf("      depends:\n        direction: needs\n        on: {kind: %s, id: %s}\n", kind, id)
	if needBy != "" {
		risk += "        needBy: " + needBy + "\n"
	}
	return risk
}

// A cycle is the thing no single manifest can show: each end looks fine on
// its own, and only the graph says they are waiting on each other.
func TestDependencyGraphAndCycles(t *testing.T) {
	e := seededEngine(t)
	ctx := context.Background()

	for _, id := range []string{"alpha", "beta", "gamma"} {
		mustCommit(t, e, "Programme", id, "local", programmeYAML(id, ""))
	}
	mustCommit(t, e, "Programme", "alpha", "local", programmeYAML("alpha", dependsOn("Programme", "beta", "")))
	mustCommit(t, e, "Programme", "beta", "local", programmeYAML("beta", dependsOn("Programme", "alpha", "")))

	edges, err := e.DependencyGraph(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Two declared edges and no third: gamma depends on nothing, and a
	// derived reverse edge is not a second edge.
	if len(edges) != 2 {
		t.Fatalf("expected two edges, got %d: %+v", len(edges), edges)
	}
	for _, edge := range edges {
		if edge.FromKind != "Programme" || edge.ToKind != "Programme" {
			t.Fatalf("unexpected edge: %+v", edge)
		}
	}

	cycles, err := e.DependencyCycles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Once, not once per node in it.
	if len(cycles) != 1 {
		t.Fatalf("expected one cycle, got %d: %+v", len(cycles), cycles)
	}
	joined := strings.Join(cycles[0], " ")
	for _, want := range []string{"Programme/alpha", "Programme/beta"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected %s in the cycle %q", want, joined)
		}
	}
}

// An edge declared from the other end is the same edge. Nothing should see
// two, and a needed-by pair should not read as a cycle.
func TestDependencyNeededByIsOneEdge(t *testing.T) {
	e := seededEngine(t)
	ctx := context.Background()

	mustCommit(t, e, "Programme", "supplier", "local", programmeYAML("supplier", ""))
	mustCommit(t, e, "Programme", "waiter", "local", programmeYAML("waiter", ""))
	mustCommit(t, e, "Programme", "supplier", "local", programmeYAML("supplier",
		"  risks:\n    - description: The other one is waiting on us\n      type: dependency\n"+
			"      depends:\n        direction: neededBy\n        on: {kind: Programme, id: waiter}\n"))

	edges, err := e.DependencyGraph(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 1 {
		t.Fatalf("expected one edge, got %d: %+v", len(edges), edges)
	}
	// Turned around: the waiter needs the supplier, however it was written.
	if edges[0].FromID != "waiter" || edges[0].ToID != "supplier" {
		t.Fatalf("expected waiter -> supplier, got %+v", edges[0])
	}
	cycles, err := e.DependencyCycles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(cycles) != 0 {
		t.Fatalf("a single edge is not a cycle: %+v", cycles)
	}
}

// The conflict the edge exists for: A needs B by a phase that ends before B
// delivers anything. It needs both ends to schedule, so it is a project
// answer — a programme schedules nothing.
func TestDependencyScheduleConflicts(t *testing.T) {
	timeline := func(phases string) string {
		return "  timeline:\n    start: \"2026-01\"\n    phases:\n" + phases
	}
	oneYear := timeline("      - {id: build, name: Build, months: 12}\n")
	// Design ends 2026-04; rollout runs to 2027-01.
	twoPhase := timeline("      - {id: design, name: Design, months: 3}\n      - {id: rollout, name: Rollout, months: 9}\n")

	setup := func(t *testing.T, needBy string) *engine.Engine {
		t.Helper()
		e := seededEngine(t)
		mustCommit(t, e, "Project", "supplier", "local", projectYAML("supplier", oneYear))
		mustCommit(t, e, "Project", "waiter", "local", projectYAML("waiter", twoPhase))
		mustCommit(t, e, "Project", "waiter", "local",
			projectYAML("waiter", twoPhase+dependsOn("Project", "supplier", needBy)))
		return e
	}

	t.Run("needed before the other end can deliver", func(t *testing.T) {
		conflicts, err := setup(t, "design").DependencyScheduleConflicts(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(conflicts) != 1 {
			t.Fatalf("expected one conflict, got %d: %+v", len(conflicts), conflicts)
		}
		c := conflicts[0]
		if c.NeedByOn != "2026-04" || c.ReadyOn != "2027-01" {
			t.Fatalf("expected needed by 2026-04, ready 2027-01, got %+v", c)
		}
		if c.Edge.FromID != "waiter" || c.Edge.ToID != "supplier" {
			t.Fatalf("expected waiter -> supplier, got %+v", c.Edge)
		}
	})

	t.Run("needed after it lands is not a conflict", func(t *testing.T) {
		// Rollout ends 2027-01, which is when the supplier is ready.
		conflicts, err := setup(t, "rollout").DependencyScheduleConflicts(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(conflicts) != 0 {
			t.Fatalf("expected no conflict, got %+v", conflicts)
		}
	})

	t.Run("no phase named means nothing to compare", func(t *testing.T) {
		conflicts, err := setup(t, "").DependencyScheduleConflicts(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(conflicts) != 0 {
			t.Fatalf("an unscheduled dependency is not a conflict: %+v", conflicts)
		}
	})
}

// The two graph facts surfaced where a person reads them. Worth asserting
// on the message as well as the state: the whole point of both checks is
// the sentence, and a state alone tells nobody which project is late.
func TestDependencyChecksOnTheProject(t *testing.T) {
	e := seededEngine(t)
	ctx := context.Background()
	named := func(id, name, extra string) string {
		return strings.Replace(projectYAML(id, extra), "  name: P\n", "  name: "+name+"\n", 1)
	}

	// Connectivity runs four years; the rollout needs it by its first phase.
	mustCommit(t, e, "Project", "conn", "local",
		named("conn", "Connectivity", "  timeline:\n    start: \"2026-01\"\n    phases:\n      - {id: build, name: Build, months: 48}\n"))
	mustCommit(t, e, "Project", "learn", "local",
		named("learn", "Digital Learning", "  timeline:\n    start: \"2026-01\"\n    phases:\n      - {id: pilot, name: Pilot, months: 6}\n"))
	mustCommit(t, e, "Project", "learn", "local",
		named("learn", "Digital Learning", "  timeline:\n    start: \"2026-01\"\n    phases:\n      - {id: pilot, name: Pilot, months: 6}\n"+
			dependsOn("Project", "conn", "pilot")))

	checks, err := e.ProjectChecks(ctx, "learn", false)
	if err != nil {
		t.Fatal(err)
	}
	byID := checksByID(checks.Items)

	sched := byID["risks-dependency-schedule"]
	if sched.State != "warn" {
		t.Fatalf("expected a schedule warning, got %+v", sched)
	}
	// The name of the other end, both dates, and a capital letter.
	for _, want := range []string{"Connectivity", "2026-07", "2030-01"} {
		if !strings.Contains(sched.Message, want) {
			t.Fatalf("expected %q in %q", want, sched.Message)
		}
	}
	if sched.Message[:1] != strings.ToUpper(sched.Message[:1]) {
		t.Fatalf("a check message is a sentence: %q", sched.Message)
	}
	// One edge is not a loop, and the passing line should say so rather
	// than be absent.
	if got := byID["risks-dependency-cycle"].State; got != "ok" {
		t.Fatalf("expected the cycle check to pass, got %q", got)
	}
	// Nothing about the graph is reported for a project holding no edge.
	other, err := e.ProjectChecks(ctx, "conn", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"risks-dependency-cycle", "risks-dependency-schedule"} {
		if _, ok := checksByID(other.Items)[id]; ok {
			t.Fatalf("%s reported on a project that waits on nothing", id)
		}
	}

	// Now close the loop from the other end and read it back as a cycle.
	mustCommit(t, e, "Project", "conn", "local",
		named("conn", "Connectivity", "  timeline:\n    start: \"2026-01\"\n    phases:\n      - {id: build, name: Build, months: 48}\n"+
			dependsOn("Project", "learn", "")))
	checks, err = e.ProjectChecks(ctx, "learn", false)
	if err != nil {
		t.Fatal(err)
	}
	cycle := checksByID(checks.Items)["risks-dependency-cycle"]
	if cycle.State != "warn" {
		t.Fatalf("expected a cycle warning, got %+v", cycle)
	}
	// Read from the project the reader is on, and closed.
	if !strings.HasPrefix(cycle.Message, "A loop: Digital Learning → Connectivity → Digital Learning") {
		t.Fatalf("unexpected cycle message: %q", cycle.Message)
	}
	// Neither graph check may ever refuse a save: both can appear because
	// somebody edited a different file.
	for _, id := range []string{"risks-dependency-cycle", "risks-dependency-schedule"} {
		if got := checksByID(checks.Items)[id].State; got == "block" {
			t.Fatalf("%s must not block", id)
		}
	}
}
