package engine

import (
	"context"
	"fmt"
	"sort"
)

// ComponentNode is one project or programme in the graph of components.
type ComponentNode struct {
	Ref
	Name string
	// Months is how long it takes itself: a project's timeline, summed; a
	// programme takes none of its own.
	Months int
	// Dependents is how many pieces of work depend on it, directly or
	// through others.
	Dependents int
	// MostDependedOn is whether more work depends on it than on any other,
	// and at least two pieces of work do.
	MostDependedOn bool
	// Critical is whether it lies on the critical path.
	Critical bool
	// InLoop is whether it lies on a loop of components.
	InLoop bool
}

// ComponentEdge is one piece of work depending on another (TAXONOMY.md
// D46). Legacy is true when it is read from an older declaration made on
// the component (a project's alignment.partOf or alignment.programmes, a
// programme's programmes).
type ComponentEdge struct {
	From, To Ref
	Why      string
	Legacy   bool
}

// ComponentGraph is the whole graph of components as ctx reads it, and
// what follows from it.
type ComponentGraph struct {
	Nodes []ComponentNode
	Edges []ComponentEdge
	// Loops are the chains of components that come back to where they
	// started, each in order, starting from its first piece of work by id.
	Loops [][]Ref
	// CriticalPath is the longest chain of components by duration, from the
	// work that depends to the work depended on.
	CriticalPath []Ref
	// CriticalMonths is the critical path's length.
	CriticalMonths int
}

// Components resolves the graph of components across every project and
// programme (TAXONOMY.md D46): who depends on whom, the loops, the
// critical path and how widely each is depended on. Read as ctx reads the
// workspace, a change set's drafts included.
func (e *Engine) Components(ctx context.Context) (ComponentGraph, error) {
	var g ComponentGraph
	at := map[Ref]int{}
	spanAt := map[Ref]func() (int, int, bool){}
	for _, kind := range []string{"Project", "Programme"} {
		docs, err := e.allInPlay(ctx, kind)
		if err != nil {
			return g, err
		}
		for _, d := range docs {
			r := Ref{Kind: kind, ID: d.id}
			at[r] = len(g.Nodes)
			sp := specOf(d.doc)
			g.Nodes = append(g.Nodes, ComponentNode{Ref: r, Name: nameOf(d.doc, d.id), Months: monthsOf(sp)})
			spanAt[r] = func() (int, int, bool) { return spanOf(sp) }
		}
		for _, d := range docs {
			from := Ref{Kind: kind, ID: d.id}
			spec := specOf(d.doc)
			if cs, ok := spec["components"].([]any); ok {
				for _, c := range cs {
					cm, _ := c.(map[string]any)
					k, _ := cm["kind"].(string)
					id, _ := cm["id"].(string)
					why, _ := cm["why"].(string)
					if k != "" && id != "" {
						g.Edges = append(g.Edges, ComponentEdge{From: from, To: Ref{Kind: k, ID: id}, Why: why})
					}
				}
			}
			// What an older definition declared on the component, read
			// the way round D46 keeps it.
			if kind == "Project" {
				alignment, _ := spec["alignment"].(map[string]any)
				if parent, _ := alignment["partOf"].(string); parent != "" {
					g.Edges = append(g.Edges, ComponentEdge{From: Ref{Kind: "Project", ID: parent}, To: from, Legacy: true})
				}
				for _, p := range stringsOf(alignment["programmes"]) {
					g.Edges = append(g.Edges, ComponentEdge{From: Ref{Kind: "Programme", ID: p}, To: from, Legacy: true})
				}
			} else {
				// A sub-programme named the programme it sits in.
				for _, p := range stringsOf(spec["programmes"]) {
					g.Edges = append(g.Edges, ComponentEdge{From: Ref{Kind: "Programme", ID: p}, To: from, Legacy: true})
				}
			}
		}
	}
	// One edge per pair: a component listed both ways counts once.
	seen := map[[2]Ref]bool{}
	edges := g.Edges[:0]
	for _, ed := range g.Edges {
		key := [2]Ref{ed.From, ed.To}
		if seen[key] {
			continue
		}
		if _, ok := at[ed.From]; !ok {
			continue
		}
		if _, ok := at[ed.To]; !ok {
			continue
		}
		seen[key] = true
		edges = append(edges, ed)
	}
	g.Edges = edges
	sort.Slice(g.Edges, func(i, j int) bool {
		if g.Edges[i].From != g.Edges[j].From {
			return refLess(g.Edges[i].From, g.Edges[j].From)
		}
		return refLess(g.Edges[i].To, g.Edges[j].To)
	})

	out := make([][]int, len(g.Nodes))
	for _, ed := range g.Edges {
		out[at[ed.From]] = append(out[at[ed.From]], at[ed.To])
	}

	// Loops: the strongly connected parts with more than one piece of
	// work, or one that lists itself (Tarjan).
	for _, scc := range stronglyConnected(out) {
		if len(scc) == 1 && !contains1(out[scc[0]], scc[0]) {
			continue
		}
		for _, n := range scc {
			g.Nodes[n].InLoop = true
		}
		g.Loops = append(g.Loops, loopThrough(out, scc, g.Nodes))
	}
	sort.Slice(g.Loops, func(i, j int) bool { return refLess(g.Loops[i][0], g.Loops[j][0]) })

	// How widely each is depended on: everything that reaches it.
	for i := range g.Nodes {
		g.Nodes[i].Dependents = 0
	}
	for i := range g.Nodes {
		for _, n := range reachable(out, i) {
			if n != i {
				g.Nodes[n].Dependents++
			}
		}
	}

	most := 0
	for _, n := range g.Nodes {
		most = max(most, n.Dependents)
	}
	for i := range g.Nodes {
		g.Nodes[i].MostDependedOn = most >= 2 && g.Nodes[i].Dependents == most
	}

	// The critical path: the chain that runs longest on the calendar, from
	// the earliest start on it to the latest finish, loops left out. Work
	// with no dates of its own (a programme) adds nothing.
	type span struct{ lo, hi int }
	spans := make([]*span, len(g.Nodes))
	for i := range g.Nodes {
		if lo, hi, ok := spanAt[g.Nodes[i].Ref](); ok {
			spans[i] = &span{lo, hi}
		}
	}
	bestLen, bestPath := 0, []int(nil)
	walked := 0
	var walk func(n int, path []int, lo, hi int, dated bool)
	walk = func(n int, path []int, lo, hi int, dated bool) {
		walked++
		if sp := spans[n]; sp != nil {
			if !dated || sp.lo < lo {
				lo = sp.lo
			}
			if !dated || sp.hi > hi {
				hi = sp.hi
			}
			dated = true
		}
		path = append(path, n)
		ends := true
		for _, c := range out[n] {
			if g.Nodes[c].InLoop || walked > 20000 {
				continue
			}
			ends = false
			walk(c, path, lo, hi, dated)
		}
		if !ends || len(path) < 2 || !dated {
			return
		}
		length := hi - lo + 1
		better := length > bestLen
		if length == bestLen && bestPath != nil {
			better = len(path) > len(bestPath) || (len(path) == len(bestPath) && refLess(g.Nodes[path[0]].Ref, g.Nodes[bestPath[0]].Ref))
		}
		if better {
			bestLen, bestPath = length, append([]int(nil), path...)
		}
	}
	for i := range g.Nodes {
		hasParent := false
		for _, ed := range g.Edges {
			if ed.To == g.Nodes[i].Ref && !g.Nodes[at[ed.From]].InLoop {
				hasParent = true
				break
			}
		}
		if !hasParent && !g.Nodes[i].InLoop && len(out[i]) > 0 {
			walk(i, nil, 0, 0, false)
		}
	}
	if bestPath != nil {
		g.CriticalMonths = bestLen
		for _, n := range bestPath {
			g.Nodes[n].Critical = true
			g.CriticalPath = append(g.CriticalPath, g.Nodes[n].Ref)
		}
	}
	return g, nil
}

// spanOf is the months a project runs, as indexes from year zero: its
// milestones' first and last dates (TAXONOMY.md D48), or its start and the
// end of its last phase.
func spanOf(spec map[string]any) (lo, hi int, ok bool) {
	if ms, _ := spec["milestones"].([]any); len(ms) > 0 {
		first := true
		for _, it := range ms {
			m, _ := it.(map[string]any)
			t, _ := m["timing"].(map[string]any)
			for _, k := range []string{"date", "notBefore", "notAfter", "expectedBy"} {
				v, _ := t[k].(string)
				if len(v) < 7 {
					continue
				}
				x, good := yearMonthIndex(v[:7])
				if !good {
					continue
				}
				if first || x < lo {
					lo = x
				}
				if first || x > hi {
					hi = x
				}
				first = false
			}
		}
		return lo, hi, !first
	}
	timeline, _ := spec["timeline"].(map[string]any)
	start, _ := timeline["start"].(string)
	s, good := yearMonthIndex(start)
	if !good {
		return 0, 0, false
	}
	total := 0
	phases, _ := timeline["phases"].([]any)
	for _, p := range phases {
		pm, _ := p.(map[string]any)
		total += intOf(pm["months"])
	}
	if total == 0 {
		return 0, 0, false
	}
	return s, s + total - 1, true
}

// monthsOf is how many months a project runs, first to last, both counted.
func monthsOf(spec map[string]any) int {
	lo, hi, ok := spanOf(spec)
	if !ok {
		return 0
	}
	return hi - lo + 1
}

func refLess(a, b Ref) bool {
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	return a.ID < b.ID
}

func contains1(list []int, n int) bool {
	for _, x := range list {
		if x == n {
			return true
		}
	}
	return false
}

// stronglyConnected is Tarjan's strongly connected components of a graph
// given as adjacency lists.
func stronglyConnected(out [][]int) [][]int {
	index, low := make([]int, len(out)), make([]int, len(out))
	onStack := make([]bool, len(out))
	for i := range index {
		index[i] = -1
	}
	var stack []int
	var sccs [][]int
	next := 0
	var visit func(v int)
	visit = func(v int) {
		index[v], low[v] = next, next
		next++
		stack = append(stack, v)
		onStack[v] = true
		for _, w := range out[v] {
			if index[w] < 0 {
				visit(w)
				low[v] = min(low[v], low[w])
			} else if onStack[w] {
				low[v] = min(low[v], index[w])
			}
		}
		if low[v] == index[v] {
			var scc []int
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				scc = append(scc, w)
				if w == v {
					break
				}
			}
			sccs = append(sccs, scc)
		}
	}
	for v := range out {
		if index[v] < 0 {
			visit(v)
		}
	}
	return sccs
}

// loopThrough is one loop inside a strongly connected part, in order,
// starting from its first piece of work by id and ending where it began.
func loopThrough(out [][]int, scc []int, nodes []ComponentNode) []Ref {
	in := map[int]bool{}
	first := scc[0]
	for _, n := range scc {
		in[n] = true
		if refLess(nodes[n].Ref, nodes[first].Ref) {
			first = n
		}
	}
	// Walk inside the part until the first node comes round again.
	path := []int{first}
	visited := map[int]bool{first: true}
	var walk func(n int) bool
	walk = func(n int) bool {
		for _, w := range out[n] {
			if !in[w] {
				continue
			}
			if w == first {
				path = append(path, first)
				return true
			}
			if visited[w] {
				continue
			}
			visited[w] = true
			path = append(path, w)
			if walk(w) {
				return true
			}
			path = path[:len(path)-1]
		}
		return false
	}
	walk(first)
	refs := make([]Ref, len(path))
	for i, n := range path {
		refs[i] = nodes[n].Ref
	}
	return refs
}

// reachable is every node reachable from n, n included.
func reachable(out [][]int, n int) []int {
	seen := map[int]bool{n: true}
	stack := []int{n}
	var all []int
	for len(stack) > 0 {
		v := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		all = append(all, v)
		for _, w := range out[v] {
			if !seen[w] {
				seen[w] = true
				stack = append(stack, w)
			}
		}
	}
	return all
}

// componentCandidates is every project and programme self could depend
// on, each marked whether it may be added and why not: itself, or one
// that already depends on self, which would close a loop.
func (e *Engine) componentCandidates(ctx context.Context, self Ref) ([]LinkCandidate, error) {
	g, err := e.Components(ctx)
	if err != nil {
		return nil, err
	}
	known := false
	out := map[Ref][]Ref{}
	for _, ed := range g.Edges {
		out[ed.From] = append(out[ed.From], ed.To)
	}
	for _, n := range g.Nodes {
		known = known || n.Ref == self
	}
	if !known {
		return nil, fmt.Errorf("%w: %s/%s", ErrNotFound, self.Kind, self.ID)
	}
	// What already depends on self, at any depth: adding any of them
	// would bring the chain back round.
	above := map[Ref]bool{}
	for _, n := range g.Nodes {
		if n.Ref != self && reachesRef(out, n.Ref, self) {
			above[n.Ref] = true
		}
	}
	mine := map[Ref]bool{}
	for _, to := range out[self] {
		mine[to] = true
	}
	res := make([]LinkCandidate, 0, len(g.Nodes))
	for _, n := range g.Nodes {
		c := LinkCandidate{Kind: n.Kind, ID: n.ID, Name: n.Name}
		switch {
		case n.Ref == self:
			c.Reason = "It cannot depend on itself."
		case mine[n.Ref]:
			c.Linked = true
		case above[n.Ref]:
			c.Reason = "It already depends on this one, so this would make a loop."
		default:
			c.Allowed = true
		}
		res = append(res, c)
	}
	return res, nil
}

// reachesRef reports whether to can be reached from from.
func reachesRef(out map[Ref][]Ref, from, to Ref) bool {
	seen := map[Ref]bool{from: true}
	stack := []Ref{from}
	for len(stack) > 0 {
		v := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, w := range out[v] {
			if w == to {
				return true
			}
			if !seen[w] {
				seen[w] = true
				stack = append(stack, w)
			}
		}
	}
	return false
}

// yearMonthIndex counts months from year zero, for "YYYY-MM".
func yearMonthIndex(m string) (int, bool) {
	y, mo, ok := splitYearMonth(m)
	return y*12 + mo - 1, ok
}
