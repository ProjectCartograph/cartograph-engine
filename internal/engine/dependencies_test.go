package engine_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store/memory"
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

// writeBehind stores a version without the engine's validation, as an
// older engine allowed it: a loop was advice until the record became a
// directed acyclic graph (TAXONOMY.md D28), so a store may still hold one.
func writeBehind(t *testing.T, ms store.ManifestStore, kind, id, y string) {
	t.Helper()
	ctx := context.Background()
	versions, err := ms.ListVersions(ctx, kind, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := ms.PutVersion(ctx, store.Version{Kind: kind, ID: id, Number: len(versions) + 1, YAML: []byte(y), Actor: "local", On: time.Now()}); err != nil {
		t.Fatal(err)
	}
}

// A loop in the record is refused where it would close (TAXONOMY.md D28):
// the save that closes it names the loop, and the record stays acyclic.
func TestADependencyLoopIsRefused(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	ctx := context.Background()
	for _, id := range []string{"alpha", "beta"} {
		mustCommit(t, e, "Programme", id, "local", programmeYAML(id, ""))
	}
	mustCommit(t, e, "Programme", "alpha", "local", programmeYAML("alpha", dependsOn("Programme", "beta", "")))
	_, err := e.Commit(ctx, "Programme", "beta", []byte(programmeYAML("beta", dependsOn("Programme", "alpha", ""))), "local", "close the loop")
	var ve *engine.ValidationError
	if !errors.As(err, &ve) || len(ve.Problems) != 1 || ve.Problems[0].Path != "/spec/risks/0/depends/on/id" ||
		!strings.Contains(ve.Problems[0].Message, "beta → alpha → beta") {
		t.Fatalf("closing a loop: %v", err)
	}
	// A programme waiting on a project names what comes after it: the
	// project declares that the programme needs it instead.
	mustCommit(t, e, "Project", "p1", "local", projectYAML("p1", ""))
	_, err = e.Commit(ctx, "Programme", "beta", []byte(programmeYAML("beta", dependsOn("Project", "p1", ""))), "local", "downstream")
	if !errors.As(err, &ve) || !strings.Contains(ve.Problems[0].Message, "comes after it") {
		t.Fatalf("a programme naming a project: %v", err)
	}
}

// A cycle is the thing no single manifest can show: each end looks fine on
// its own, and only the graph says they are waiting on each other. A store
// written before loops were refused may hold one, and the check finds it.
func TestDependencyGraphAndCycles(t *testing.T) {
	t.Parallel()
	ms := memory.NewManifestStore()
	e := seededEngineOver(t, ms)
	ctx := context.Background()

	for _, id := range []string{"alpha", "beta", "gamma"} {
		mustCommit(t, e, "Programme", id, "local", programmeYAML(id, ""))
	}
	mustCommit(t, e, "Programme", "alpha", "local", programmeYAML("alpha", dependsOn("Programme", "beta", "")))
	writeBehind(t, ms, "Programme", "beta", programmeYAML("beta", dependsOn("Programme", "alpha", "")))
	// A record saved before loops were refused still saves as it stands:
	// only a reference being added is held to the order (VERSIONING.md).
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	ms := memory.NewManifestStore()
	e := seededEngineOver(t, ms)
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

	// Closing the loop from the other end is refused; a store written
	// before it was may hold one, and the check reads it as a cycle.
	closing := named("conn", "Connectivity", "  timeline:\n    start: \"2026-01\"\n    phases:\n      - {id: build, name: Build, months: 48}\n"+
		dependsOn("Project", "learn", ""))
	if _, err := e.Commit(ctx, "Project", "conn", []byte(closing), "local", "close the loop"); err == nil {
		t.Fatal("a save that closes a loop was accepted")
	}
	writeBehind(t, ms, "Project", "conn", closing)
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

// A dependency says when it is needed as a timing (TAXONOMY.md D47), and
// the other project's milestones say when it is ready (D48): needed before
// its last milestone is a conflict, with no phases on either side.
func TestADependencyNeededByATimingMeetsTheOtherProjectsMilestones(t *testing.T) {
	t.Parallel()
	needed := func(timing string) string {
		return "  risks:\n    - description: Cannot start without it\n      type: dependency\n" +
			"      depends:\n        direction: needs\n        on: {kind: Project, id: supplier}\n        needed: " + timing + "\n"
	}
	supplier := "  milestones:\n    - {id: live, name: Live, timing: {form: date, date: \"2027-06\"}}\n"
	own := "  milestones:\n    - {id: start, name: Start, timing: {form: date, date: \"2026-10\"}}\n"
	setup := func(t *testing.T, timing string) []engine.ScheduleConflict {
		t.Helper()
		e := seededEngine(t)
		mustCommit(t, e, "Project", "supplier", "local", projectYAML("supplier", supplier))
		mustCommit(t, e, "Project", "waiter", "local", projectYAML("waiter", own+needed(timing)))
		conflicts, err := e.DependencyScheduleConflicts(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return conflicts
	}
	if c := setup(t, `{form: date, date: "2027-01"}`); len(c) != 1 || c[0].NeedByOn != "2027-01" || c[0].ReadyOn != "2027-06" {
		t.Fatalf("needed in January, ready in June: %+v", c)
	}
	if c := setup(t, `{form: after, event: {on: {local: milestones, id: start}}, lagMonths: 3}`); len(c) != 1 || c[0].NeedByOn != "2027-01" {
		t.Fatalf("needed three months after its own start (October): %+v", c)
	}
	if c := setup(t, `{form: window, notAfter: "2027-09"}`); len(c) != 0 {
		t.Fatalf("needed by September, ready in June, is no conflict: %+v", c)
	}
}
