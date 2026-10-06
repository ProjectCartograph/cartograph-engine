package engine_test

import (
	"context"
	"errors"
	"testing"

	codecyaml "github.com/ProjectCartograph/cartograph-engine/v2/internal/codec/yaml"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/layout/force"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store/memory"
)

// The workspace graph is every element and every reference between them,
// read from the index: a KPI reaches the goals it measures, a goal knows
// its level, and nothing that is not an element is in it. The engine
// places it, the same way every time, and measures it from a focus, so
// every interface draws and lights it alike.
func TestTheWorkspaceGraph(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	if _, err := newTestEngine(t).Graph(ctx, nil); !errors.Is(err, engine.ErrNoLayout) {
		t.Fatalf("a graph with no layout: %v", err)
	}
	e, err := engine.New(memory.NewManifestStore(), memory.NewOperationalStore(), engine.WithCodec(codecyaml.New()), engine.WithLayout(force.New()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.ImportDir(ctx, exampleDir(t), "alice-nkemah", "seed"); err != nil {
		t.Fatal(err)
	}
	kpi := engine.Ref{Kind: "KPI", ID: "depot-availability"}
	g, err := e.Graph(ctx, &kpi)
	if err != nil {
		t.Fatal(err)
	}
	again, err := e.Graph(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	nodes := map[engine.Ref]engine.GraphNode{}
	for _, n := range g.Nodes {
		if n.Kind == "Settings" || n.Kind == "KPIReadings" {
			t.Fatalf("%s/%s is not an element of the workspace", n.Kind, n.ID)
		}
		nodes[engine.Ref{Kind: n.Kind, ID: n.ID}] = n
	}
	if n := nodes[engine.Ref{Kind: "Goal", ID: "depots-stay-open-through-the-season"}]; n.Level == "" || n.Name == "" {
		t.Fatalf("a goal carries its name and level, got %+v", n)
	}
	measures := false
	for _, edge := range g.Edges {
		if _, ok := nodes[edge.From]; !ok {
			t.Fatalf("edge from %v, which is not a node", edge.From)
		}
		if _, ok := nodes[edge.To]; !ok {
			t.Fatalf("edge to %v, which is not a node", edge.To)
		}
		if edge.From == (engine.Ref{Kind: "KPI", ID: "depot-availability"}) && edge.To == (engine.Ref{Kind: "Goal", ID: "depots-stay-open-through-the-season"}) {
			measures = true
		}
	}
	if !measures {
		t.Fatal("the KPI does not reach the goal it measures")
	}
	for i, n := range g.Nodes {
		if again.Nodes[i].X != n.X || again.Nodes[i].Y != n.Y {
			t.Fatalf("%s/%s placed at %v,%v, then %v,%v", n.Kind, n.ID, n.X, n.Y, again.Nodes[i].X, again.Nodes[i].Y)
		}
		if again.Nodes[i].Distance != -1 {
			t.Fatalf("a distance with no focus: %+v", again.Nodes[i])
		}
	}
	if d := nodes[kpi].Distance; d != 0 {
		t.Fatalf("the focus is %d from itself", d)
	}
	if d := nodes[engine.Ref{Kind: "Goal", ID: "depots-stay-open-through-the-season"}].Distance; d != 1 {
		t.Fatalf("the goal the KPI measures is %d away, want 1", d)
	}
}
