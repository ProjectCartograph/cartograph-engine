package engine

import (
	"context"
	"errors"
	"sort"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/kinds"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/layout"
)

// ErrNoLayout is a graph asked of an engine given no layout.
var ErrNoLayout = errors.New("no graph layout is configured")

// WithLayout sets how the workspace graph is placed for drawing. Without
// one, Graph answers ErrNoLayout.
func WithLayout(l layout.Layout) Option {
	return func(e *Engine) { e.layout = l }
}

// GraphNode is one manifest in the workspace graph.
type GraphNode struct {
	Kind, ID, Name string
	// Level is a goal's level (goal, objective, outcome); empty otherwise.
	Level string
	// Stage is the stage of the order of work it is written in; empty
	// for a register. Layer is its band in the graph: 0 for the
	// registers, then one per stage, so every edge runs to a lower layer
	// or along its own (TAXONOMY.md D28).
	Stage string
	Layer int
	// X and Y are where the layout put it.
	X, Y float64
	// Distance is how many edges, either way, it is from the focus asked
	// for; -1 when nothing joins them or no focus was asked for.
	Distance int
}

// GraphEdge is one reference: From names To somewhere in its spec, so To
// comes before From in the order of work.
type GraphEdge struct {
	From, To Ref
}

// Graph is every manifest and every reference between them.
type Graph struct {
	Nodes []GraphNode
	Edges []GraphEdge
}

// notInGraph are kinds that are not elements of the workspace: the
// deployment's settings, and the readings that are a KPI's data rather
// than something it is connected to.
var notInGraph = map[string]bool{"Settings": true, "KPIReadings": true, "PortfolioDecisions": true}

// Graph returns every manifest and every reference between them, from
// the reference index the store already keeps: a list and one reverse
// lookup per kind, never a manifest read. An edge whose ends are not
// both in the graph is left out. Every node carries where the layout
// placed it, and, with a focus, how far it is from that one; the nodes
// come grouped by kind in the order of work and by id, so the same
// workspace is always placed alike.
func (e *Engine) Graph(ctx context.Context, focus *Ref) (Graph, error) {
	if e.layout == nil {
		return Graph{}, ErrNoLayout
	}
	var g Graph
	present := map[Ref]bool{}
	levels := map[string]string{}
	if tree, err := e.GoalTree(ctx); err == nil {
		var walk func([]*GoalNode)
		walk = func(ns []*GoalNode) {
			for _, n := range ns {
				levels[n.ID] = n.Level
				walk(n.Children)
			}
		}
		walk(tree.Nodes)
	}
	for _, kind := range graphKinds() {
		summaries, err := e.manifests.ListSummaries(ctx, kind, "", nil)
		if err != nil {
			return Graph{}, err
		}
		for _, s := range summaries {
			n := GraphNode{Kind: kind, ID: s.ID, Name: s.Name}
			if kind == "Goal" {
				n.Level = levels[s.ID]
			}
			n.Layer = layerOf(kind, n.Level)
			if st, ok := stageOf(kind, n.Level); ok {
				n.Stage = st.Key
			}
			g.Nodes = append(g.Nodes, n)
			present[Ref{Kind: kind, ID: s.ID}] = true
		}
	}
	seen := map[GraphEdge]bool{}
	for _, kind := range graphKinds() {
		var ids []string
		for _, n := range g.Nodes {
			if n.Kind == kind {
				ids = append(ids, n.ID)
			}
		}
		by, err := referencingKind(ctx, e.manifests, kind, ids)
		if err != nil {
			return Graph{}, err
		}
		for id, froms := range by {
			to := Ref{Kind: kind, ID: id}
			if !present[to] {
				continue
			}
			for _, f := range froms {
				edge := GraphEdge{From: Ref{Kind: f.Kind, ID: f.ID}, To: to}
				if !present[edge.From] || edge.From == to || seen[edge] {
					continue
				}
				seen[edge] = true
				g.Edges = append(g.Edges, edge)
			}
		}
	}
	e.place(&g, focus)
	sort.Slice(g.Edges, func(i, j int) bool {
		a, b := g.Edges[i], g.Edges[j]
		if a.From != b.From {
			return a.From.Kind+"/"+a.From.ID < b.From.Kind+"/"+b.From.ID
		}
		return a.To.Kind+"/"+a.To.ID < b.To.Kind+"/"+b.To.ID
	})
	return g, nil
}

// graphKinds is every kind in the graph, in the order of work, then any
// kind the order does not name yet.
func graphKinds() []string {
	out := []string{}
	named := map[string]bool{}
	for _, k := range orderedKinds() {
		if _, ok := kinds.ByName(k); ok && !notInGraph[k] {
			out = append(out, k)
			named[k] = true
		}
	}
	for _, k := range kinds.Names() {
		if !named[k] && !notInGraph[k] {
			out = append(out, k)
		}
	}
	return out
}

// layerOf is a node's band: the registers share the first, the roots
// everything may name, and each stage has its own below.
func layerOf(kind, level string) int {
	r := rank(kind, level)
	if r < len(registers) {
		return 0
	}
	return r - len(registers) + 1
}

// place asks the layout where each node goes, a layer to a group, and
// measures every node's distance from the focus.
func (e *Engine) place(g *Graph, focus *Ref) {
	index := map[Ref]int{}
	nodes := make([]layout.Node, len(g.Nodes))
	for i, n := range g.Nodes {
		index[Ref{Kind: n.Kind, ID: n.ID}] = i
		nodes[i] = layout.Node{Group: n.Layer}
	}
	edges := make([]layout.Edge, 0, len(g.Edges))
	near := make([][]int, len(g.Nodes))
	for _, ed := range g.Edges {
		s, t := index[ed.From], index[ed.To]
		edges = append(edges, layout.Edge{S: s, T: t})
		near[s] = append(near[s], t)
		near[t] = append(near[t], s)
	}
	for i, p := range e.layout.Place(nodes, edges) {
		g.Nodes[i].X, g.Nodes[i].Y = p.X, p.Y
		g.Nodes[i].Distance = -1
	}
	if focus == nil {
		return
	}
	start, ok := index[Ref{Kind: focus.Kind, ID: focus.ID}]
	if !ok {
		return
	}
	g.Nodes[start].Distance = 0
	frontier := []int{start}
	for d := 1; len(frontier) > 0; d++ {
		var next []int
		for _, i := range frontier {
			for _, j := range near[i] {
				if g.Nodes[j].Distance < 0 {
					g.Nodes[j].Distance = d
					next = append(next, j)
				}
			}
		}
		frontier = next
	}
}
