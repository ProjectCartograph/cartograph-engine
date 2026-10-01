package engine

import (
	"context"
	"sort"
	"strings"
)

// The dependency graph across a whole vault.
//
// Every edge has the same shape wherever it is declared — a risk typed
// dependency, carrying a direction and a reference to the far end — so one
// walk compiles the lot. Projects and programmes both hold them; a
// programme's are the ones worth drawing, since the Strategic Plan states
// linkages between its programmes twenty-six times.
//
// What the edges make checkable is worth more than a picture of them. A
// cycle and a schedule conflict are both facts a person cannot see by
// looking at one definition, and both can be stated in a sentence.

// DependencyEdge is one resolved link: who waits, on what, and by when.
type DependencyEdge struct {
	FromKind string `json:"fromKind"`
	FromID   string `json:"fromId"`
	FromName string `json:"fromName"`
	ToKind   string `json:"toKind"`
	ToID     string `json:"toId"`
	ToName   string `json:"toName"`
	NeedBy   string `json:"needBy,omitempty"`
	Why      string `json:"why,omitempty"`
}

// dependencyKinds are the kinds that hold a risk list, and so may hold an
// edge. A new one joins by having risks, not by being listed somewhere.
var dependencyKinds = []string{"Project", "Programme"}

// dependencyDocs loads every manifest that may hold an edge, once. Both
// walks below need both ends of every edge — one to name them, one to
// schedule them — so neither can work from a single kind at a time.
func (e *Engine) dependencyDocs(ctx context.Context) (map[string]map[string]map[string]any, error) {
	l := &lookup{ctx: ctx, store: e.manifests, codec: e.codec}
	out := map[string]map[string]map[string]any{}
	for _, kind := range dependencyKinds {
		docs, err := l.Documents(kind)
		if err != nil {
			return nil, err
		}
		out[kind] = docs
	}
	return out, nil
}

// DependencyGraph reads every edge in the vault, normalised so they all
// point the same way: from the thing that waits, to the thing waited on.
// A "neededBy" edge is stored on the far end, so it is turned around here
// rather than leaving two conventions in one graph.
func (e *Engine) DependencyGraph(ctx context.Context) ([]DependencyEdge, error) {
	docs, err := e.dependencyDocs(ctx)
	if err != nil {
		return nil, err
	}
	// nameOf reads a far end's own name rather than printing its id. An
	// edge's two ends may be different kinds, so this needs every kind
	// loaded before any edge is built.
	nameOf := func(kind, id string) string {
		if doc, ok := docs[kind][id]; ok {
			return docName(doc, id)
		}
		return id
	}

	var out []DependencyEdge
	for _, kind := range dependencyKinds {
		for id, doc := range docs[kind] {
			spec, _ := doc["spec"].(map[string]any)
			if spec == nil {
				continue
			}
			risks, _ := spec["risks"].([]any)
			for _, r := range risks {
				rm, ok := r.(map[string]any)
				if !ok {
					continue
				}
				dep, ok := rm["depends"].(map[string]any)
				if !ok {
					continue
				}
				on, ok := parseRef(dep["on"])
				if !ok || on.Kind == "" || on.ID == "" {
					// An external target has no node in this vault, and a
					// local one points inside a single manifest. Neither is
					// an edge between two pieces of work.
					continue
				}
				why, _ := rm["description"].(string)
				needBy, _ := dep["needBy"].(string)
				edge := DependencyEdge{
					FromKind: kind, FromID: id, FromName: docName(doc, id),
					ToKind: on.Kind, ToID: on.ID, ToName: nameOf(on.Kind, on.ID),
					NeedBy: needBy, Why: why,
				}
				if d, _ := dep["direction"].(string); d == "neededBy" {
					// Declared from the other end: the far end is the one
					// that waits. needBy is dropped with the turn, because
					// it names a phase of this manifest's timeline and this
					// manifest is no longer the one waiting.
					edge = DependencyEdge{
						FromKind: on.Kind, FromID: on.ID, FromName: nameOf(on.Kind, on.ID),
						ToKind: kind, ToID: id, ToName: docName(doc, id),
						Why: why,
					}
				}
				out = append(out, edge)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].FromID != out[j].FromID {
			return out[i].FromID < out[j].FromID
		}
		return out[i].ToID < out[j].ToID
	})
	return out, nil
}

// DependencyCycles returns every cycle in the graph, each as the ids it
// runs through, starting from the lowest for a stable reading. Two pieces
// of work each waiting on the other is a fact neither definition shows.
func (e *Engine) DependencyCycles(ctx context.Context) ([][]string, error) {
	edges, err := e.DependencyGraph(ctx)
	if err != nil {
		return nil, err
	}
	next := map[string][]string{}
	for _, edge := range edges {
		from, to := edge.FromKind+"/"+edge.FromID, edge.ToKind+"/"+edge.ToID
		next[from] = append(next[from], to)
	}

	var cycles [][]string
	seen := map[string]bool{}
	var stack []string
	onStack := map[string]bool{}
	var walk func(node string)
	walk = func(node string) {
		seen[node] = true
		onStack[node] = true
		stack = append(stack, node)
		for _, to := range next[node] {
			if onStack[to] {
				// Found one: the cycle is the stack from that node on.
				at := 0
				for i, n := range stack {
					if n == to {
						at = i
						break
					}
				}
				cycle := append([]string{}, stack[at:]...)
				if !haveCycle(cycles, cycle) {
					cycles = append(cycles, cycle)
				}
				continue
			}
			if !seen[to] {
				walk(to)
			}
		}
		stack = stack[:len(stack)-1]
		onStack[node] = false
	}
	nodes := make([]string, 0, len(next))
	for n := range next {
		nodes = append(nodes, n)
	}
	sort.Strings(nodes)
	for _, n := range nodes {
		if !seen[n] {
			walk(n)
		}
	}
	return cycles, nil
}

// haveCycle reports whether an equivalent cycle is already recorded. The
// same loop is found once per node it passes through, so it is matched on
// its members rather than on where the walk happened to enter it.
func haveCycle(have [][]string, cycle []string) bool {
	key := func(c []string) string {
		s := append([]string{}, c...)
		sort.Strings(s)
		return strings.Join(s, "|")
	}
	want := key(cycle)
	for _, c := range have {
		if key(c) == want {
			return true
		}
	}
	return false
}

// ScheduleConflict is a dependency that cannot land in time: the work
// waiting needs it by a phase that ends before the work it waits on
// finishes.
type ScheduleConflict struct {
	Edge     DependencyEdge `json:"edge"`
	NeedByOn string         `json:"needByOn"`
	ReadyOn  string         `json:"readyOn"`
}

// DependencyScheduleConflicts is the check the edge exists for. It needs
// both ends to carry a timeline, so it is a project-to-project answer
// today; a programme schedules nothing, and a dependency on one is
// reported as unschedulable rather than as a conflict.
func (e *Engine) DependencyScheduleConflicts(ctx context.Context) ([]ScheduleConflict, error) {
	edges, err := e.DependencyGraph(ctx)
	if err != nil {
		return nil, err
	}
	docs, err := e.dependencyDocs(ctx)
	if err != nil {
		return nil, err
	}

	var out []ScheduleConflict
	for _, edge := range edges {
		if edge.NeedBy == "" || edge.FromKind != "Project" || edge.ToKind != "Project" {
			continue
		}
		from, ok := docs[edge.FromKind][edge.FromID]
		if !ok {
			continue
		}
		to, ok := docs[edge.ToKind][edge.ToID]
		if !ok {
			continue
		}
		needBy, found := phaseEnd(from, edge.NeedBy)
		if !found {
			continue
		}
		ready, found := timelineEnd(to)
		if !found {
			continue
		}
		if ready > needBy {
			out = append(out, ScheduleConflict{Edge: edge, NeedByOn: needBy, ReadyOn: ready})
		}
	}
	return out, nil
}

// phaseEnd is the month a named phase of this manifest finishes, counting
// the phases before it from the timeline's start.
func phaseEnd(doc map[string]any, phaseID string) (string, bool) {
	spec, _ := doc["spec"].(map[string]any)
	tl, _ := spec["timeline"].(map[string]any)
	if tl == nil {
		return "", false
	}
	start, _ := tl["start"].(string)
	phases, _ := tl["phases"].([]any)
	if start == "" || len(phases) == 0 {
		return "", false
	}
	months := 0
	for _, p := range phases {
		pm, ok := p.(map[string]any)
		if !ok {
			continue
		}
		months += intOf(pm["months"])
		if id, _ := pm["id"].(string); id == phaseID {
			end, err := addMonths(start, months)
			return end, err == nil
		}
	}
	return "", false
}

// timelineEnd is the month this manifest's last phase finishes.
func timelineEnd(doc map[string]any) (string, bool) {
	spec, _ := doc["spec"].(map[string]any)
	tl, _ := spec["timeline"].(map[string]any)
	if tl == nil {
		return "", false
	}
	start, _ := tl["start"].(string)
	phases, _ := tl["phases"].([]any)
	if start == "" || len(phases) == 0 {
		return "", false
	}
	months := 0
	for _, p := range phases {
		if pm, ok := p.(map[string]any); ok {
			months += intOf(pm["months"])
		}
	}
	end, err := addMonths(start, months)
	return end, err == nil
}

func intOf(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

// docName is the display name of a parsed manifest, falling back to its id
// so a message never reads as empty.
func docName(doc map[string]any, id string) string {
	meta, _ := doc["metadata"].(map[string]any)
	if meta != nil {
		if name, _ := meta["name"].(string); name != "" {
			return name
		}
	}
	return id
}
