package engine_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
)

// work is a project of the given months listing its components, each a
// kind and an id.
func work(id string, months string, components ...string) string {
	extra := "  timeline:\n    start: \"2026-01\"\n    phases:\n      - {name: Build, months: " + months + "}\n"
	if len(components) > 0 {
		extra += "  components:\n"
		for i := 0; i < len(components); i += 2 {
			extra += "    - {kind: " + components[i] + ", id: " + components[i+1] + "}\n"
		}
	}
	return projectYAML(id, extra)
}

// A project or programme lists what it depends on; the engine reads the
// whole graph: who depends on what, at any depth, the critical path by
// duration, the most depended on, and every loop (TAXONOMY.md D46).
func TestComponentsResolveTheWholeGraph(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	ctx := context.Background()
	mustCommit(t, e, "Project", "platform", "p1", work("platform", "4"))
	mustCommit(t, e, "Project", "pb", "p1", work("pb", "3", "Project", "platform"))
	mustCommit(t, e, "Project", "pa", "p1", work("pa", "6", "Project", "pb", "Project", "platform"))
	mustCommit(t, e, "Project", "pc", "p1", work("pc", "2", "Project", "platform"))
	mustCommit(t, e, "Programme", "prog", "local", programmeYAML("prog", "  components:\n    - {kind: Project, id: pc, why: It needs the registers}\n"))

	g, err := e.Components(ctx)
	if err != nil {
		t.Fatal(err)
	}
	node := func(id string) engine.ComponentNode {
		t.Helper()
		for _, n := range g.Nodes {
			if n.ID == id {
				return n
			}
		}
		t.Fatalf("%s is not in the graph", id)
		return engine.ComponentNode{}
	}
	// Everything depends on the platform, directly or through another.
	if p := node("platform"); p.Dependents != 4 || !p.MostDependedOn {
		t.Errorf("the shared platform: %+v", p)
	}
	if b := node("pb"); b.MostDependedOn {
		t.Errorf("b is depended on once: %+v", b)
	}
	// Every project starts in January 2026, so they run side by side: the
	// chain that runs longest on the calendar is pa's six months, through pb
	// to the platform.
	var path []string
	for _, r := range g.CriticalPath {
		path = append(path, r.ID)
	}
	if strings.Join(path, ">") != "pa>pb>platform" || g.CriticalMonths != 6 {
		t.Errorf("critical path %v over %d months, want pa>pb>platform over 6", path, g.CriticalMonths)
	}
	if len(g.Loops) != 0 {
		t.Errorf("no loop yet: %v", g.Loops)
	}

	// The platform may not take a, which already depends on it.
	cands, err := e.LinkCandidates(ctx, engine.LinkProjectComponent, "platform", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cands {
		switch c.ID {
		case "pa", "platform":
			if c.Allowed || c.Reason == "" {
				t.Errorf("%s offered to the platform: %+v", c.ID, c)
			}
		}
	}
	from, err := e.LinkCandidates(ctx, engine.LinkProjectComponent, "pa", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range from {
		if c.ID == "pb" && !c.Linked {
			t.Errorf("a lists b: %+v", c)
		}
		if c.ID == "prog" && (!c.Allowed || c.Kind != "Programme") {
			t.Errorf("a programme can be a component: %+v", c)
		}
	}

	// Saving a loop is refused, the loop named in order.
	if _, err := e.Commit(ctx, "Project", "platform", []byte(work("platform", "4", "Project", "pa")), "p1", "loop"); err == nil ||
		!strings.Contains(err.Error(), "this would make a loop: P depends on P") {
		t.Fatalf("a loop saved: %v", err)
	}

	// One that comes in through an older declaration (pc named the
	// platform its parent, so the platform depends on pc) is found, and
	// stops the handoff of each project on it.
	mustCommit(t, e, "Project", "pc", "p1", projectYAML("pc", "  alignment:\n    partOf: platform\n"+
		"  timeline:\n    start: \"2026-01\"\n    phases:\n      - {name: Build, months: 2}\n  components:\n    - {kind: Project, id: platform}\n"))
	g, err = e.Components(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Loops) != 1 || !node("pc").InLoop || !node("platform").InLoop || node("pa").InLoop {
		t.Fatalf("loops %v", g.Loops)
	}
	checks, err := e.ProjectChecks(ctx, "pc", false)
	if err != nil {
		t.Fatal(err)
	}
	got := checksByID(checks.Items)["components-loop"]
	if got.State != "block" || got.Message != "Depends on itself: P depends on P depends on P. Remove one of these components to break the loop." {
		t.Errorf("loop check: %+v", got)
	}
	checks, err = e.ProjectChecks(ctx, "pb", false)
	if err != nil {
		t.Fatal(err)
	}
	if got := checksByID(checks.Items)["components-loop"]; got.State != "ok" {
		t.Errorf("pb is off the loop: %+v", got)
	}
}

// An older definition that names its parent is read the other way round.
func TestAnOlderPartOfReadsAsAComponent(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	mustCommit(t, e, "Project", "parent", "p1", projectYAML("parent", ""))
	mustCommit(t, e, "Project", "child", "p1", projectYAML("child", "  alignment:\n    partOf: parent\n"))
	g, err := e.Components(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Edges) != 1 || g.Edges[0].From.ID != "parent" || g.Edges[0].To.ID != "child" || !g.Edges[0].Legacy {
		t.Fatalf("edges %+v", g.Edges)
	}
}
