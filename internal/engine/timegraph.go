package engine

import (
	"context"
	"fmt"
	"sort"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/layout"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/timing"
)

// The waits of a piece of work (TAXONOMY.md D47, D48): every item of a
// project that says when it falls, across kinds, and what each waits on.
// A milestone, a deliverable, a condition, a purchase and a dependency on
// another project are the project's own; a KPI's target or baseline set
// when one of them happens is the KPI's; another project's item it waits
// on is that project's. The engine places each on time, marks the chain
// that decides the last date, what no longer fits what it waits on, and
// what a risk could move, and lays it out; an interface draws it.

// TimeNode is one item that says when it falls.
type TimeNode struct {
	// Kind is what it is: milestone, deliverable, condition, purchase,
	// dependency, target, baseline, or ready (when another project is
	// ready, for a dependency on it).
	Kind string `json:"kind"`
	// Record is the record it is in, to open: Project/id or KPI/id.
	Record Ref    `json:"record"`
	Item   string `json:"item,omitempty"`
	Name   string `json:"name"`
	// Timing is the timing as written, and Follows the name of what it
	// waits on, where it follows something: for the words an interface
	// says it in.
	Timing  map[string]any `json:"timing,omitempty"`
	Follows string         `json:"follows,omitempty"`
	Month   string         `json:"month,omitempty"`
	// Risks are the descriptions of the risks the timing says could move
	// it: recorded, never simulated.
	Risks []string `json:"risks,omitempty"`
	// riskItems are the ids of those risks, for what a risk reaches.
	riskItems []string
	// Critical is on the chain that decides the last date; Conflict no
	// longer fits its own date or window, or what it needs lands after
	// it; Late is set once something happens, past the month expected;
	// Unplaced has no month to be worked out.
	Critical bool    `json:"critical"`
	Conflict bool    `json:"conflict"`
	Late     bool    `json:"late"`
	Unplaced bool    `json:"unplaced"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
}

// TimeEdge runs from what comes first to what waits on it, by index.
type TimeEdge struct {
	From int `json:"from"`
	To   int `json:"to"`
}

// TimeGraph is a project's waits.
type TimeGraph struct {
	Nodes []TimeNode `json:"nodes"`
	Edges []TimeEdge `json:"edges"`
}

// timeLists are a project's lists whose items say when they fall, and the
// field each says it in.
var timeLists = []struct{ list, field, kind string }{
	{"milestones", "timing", "milestone"},
	{"deliverables", "due", "deliverable"},
	{"conditions", "due", "condition"},
	{"procurement", "requiredBy", "purchase"},
}

// Waits is a project's graph of time, as ctx reads it.
func (e *Engine) Waits(ctx context.Context, project string) (TimeGraph, error) {
	p := &placer{e: e, ctx: ctx, specs: map[string]map[string]any{}, month: map[spot]string{}, state: map[spot]int{}, from: map[spot]string{}}
	spec, found, err := p.spec(project)
	if err != nil {
		return TimeGraph{}, err
	}
	if !found {
		return TimeGraph{}, fmt.Errorf("%w: Project/%s", ErrNotFound, project)
	}
	g := &waits{p: p, project: project, index: map[string]int{}}
	riskNames := map[string]string{}
	risks, _ := spec["risks"].([]any)
	for _, r := range risks {
		rm, _ := r.(map[string]any)
		if id, _ := rm["id"].(string); id != "" {
			riskNames[id], _ = rm["description"].(string)
		}
	}
	// The project's own items, then what each waits on.
	for _, tl := range timeLists {
		items, _ := spec[tl.list].([]any)
		for _, it := range items {
			m, _ := it.(map[string]any)
			id, _ := m["id"].(string)
			tm, _ := m[tl.field].(map[string]any)
			if id == "" || tm == nil {
				continue
			}
			i := g.item(project, tl.list, id, tl.kind, m)
			g.nodes[i].Timing = tm
			for _, rid := range stringList(tm["risks"]) {
				g.nodes[i].riskItems = append(g.nodes[i].riskItems, rid)
				if n := riskNames[rid]; n != "" {
					g.nodes[i].Risks = append(g.nodes[i].Risks, n)
				}
			}
		}
	}
	for _, tl := range timeLists {
		items, _ := spec[tl.list].([]any)
		for _, it := range items {
			m, _ := it.(map[string]any)
			id, _ := m["id"].(string)
			at, ok := g.index[key(project, tl.list, id)]
			if !ok {
				continue
			}
			if ev := eventOf(g.nodes[at].Timing); ev != nil {
				g.waitOn(at, project, ev)
			}
			if tl.list == "milestones" {
				ws, _ := m["waitsOn"].([]any)
				for _, w := range ws {
					if ev, ok := w.(map[string]any); ok {
						g.waitOn(at, project, ev)
					}
				}
			}
		}
	}
	// A dependency on another project: needed by its timing, ready when
	// that project's last milestone falls.
	for _, r := range risks {
		rm, _ := r.(map[string]any)
		dep, _ := rm["depends"].(map[string]any)
		id, _ := rm["id"].(string)
		on, ok := parseRef(dep["on"])
		if dep == nil || id == "" || !ok || on.Kind != "Project" || on.ID == "" || dep["direction"] == "neededBy" {
			continue
		}
		name, _ := rm["description"].(string)
		needed, _ := dep["needed"].(map[string]any)
		at := g.add(TimeNode{Kind: "dependency", Record: Ref{Kind: "Project", ID: project}, Item: "risks/" + id, Name: name,
			Timing: needed, Month: p.when(project, needed)}, key(project, "risks", id))
		if ev := eventOf(needed); ev != nil {
			g.waitOn(at, project, ev)
		}
		ready, _ := p.ready(on.ID)
		readyAt := g.add(TimeNode{Kind: "ready", Record: Ref{Kind: "Project", ID: on.ID}, Name: e.projectName(ctx, on.ID), Month: ready}, on.ID+"#ready")
		g.edge(readyAt, at)
		if ready != "" && g.nodes[at].Month != "" && ready > g.nodes[at].Month {
			g.nodes[at].Conflict = true
		}
	}
	// A KPI's target or baseline set when one of the project's items
	// happens, or due after one.
	kpis, err := e.allInPlay(ctx, "KPI")
	if err != nil {
		return TimeGraph{}, err
	}
	for _, k := range kpis {
		ks := specOf(k.doc)
		name := docName(k.doc, k.id)
		target, _ := ks["target"].(map[string]any)
		for _, part := range []struct {
			kind string
			tm   map[string]any
		}{
			{"target", asMap(target["setWhen"])},
			{"target", asMap(target["due"])},
			{"baseline", baselineTiming(ks["baseline"])},
		} {
			ev := eventOf(part.tm)
			if ev == nil || !onProject(ev, project) {
				continue
			}
			at := g.add(TimeNode{Kind: part.kind, Record: Ref{Kind: "KPI", ID: k.id}, Name: name, Timing: part.tm, Month: p.when(project, part.tm)},
				"KPI/"+k.id+"#"+part.kind)
			g.waitOn(at, project, ev)
		}
	}
	g.mark()
	e.placeWaits(g)
	return TimeGraph{Nodes: g.nodes, Edges: g.edges}, nil
}

// waits builds a TimeGraph, each item once.
type waits struct {
	p       *placer
	project string
	nodes   []TimeNode
	edges   []TimeEdge
	index   map[string]int
	seen    map[[2]int]bool
}

func key(project, list, id string) string { return project + "#" + list + "/" + id }

func (g *waits) add(n TimeNode, k string) int {
	if i, ok := g.index[k]; ok {
		return i
	}
	g.index[k] = len(g.nodes)
	g.nodes = append(g.nodes, n)
	return len(g.nodes) - 1
}

// item adds a project's item, placed on time.
func (g *waits) item(project, list, id, kind string, m map[string]any) int {
	name, _ := m["name"].(string)
	if name == "" {
		name, _ = m["action"].(string)
	}
	var month string
	switch list {
	case "milestones":
		month = g.p.place(project, id)
	default:
		tm, _ := m[map[string]string{"deliverables": "due", "conditions": "due", "procurement": "requiredBy"}[list]].(map[string]any)
		month = g.p.when(project, tm)
	}
	return g.add(TimeNode{Kind: kind, Record: Ref{Kind: "Project", ID: project}, Item: list + "/" + id, Name: name, Month: month}, key(project, list, id))
}

func (g *waits) edge(from, to int) {
	if g.seen == nil {
		g.seen = map[[2]int]bool{}
	}
	if from == to || g.seen[[2]int{from, to}] {
		return
	}
	g.seen[[2]int{from, to}] = true
	g.edges = append(g.edges, TimeEdge{From: from, To: to})
}

// waitOn joins the item at to the item an event happens to, in this
// project or another, adding that one where it is not yet in the graph.
func (g *waits) waitOn(to int, project string, ev map[string]any) {
	on, _ := ev["on"].(map[string]any)
	list, _ := on["local"].(string)
	id, _ := on["id"].(string)
	if k, _ := on["kind"].(string); k == "Project" && id != "" {
		item, _ := ev["item"].(string)
		if item == "" {
			return
		}
		project, id = id, item
		list = ""
		for _, tl := range timeLists {
			if g.p.item(project, tl.list, id) != nil {
				list = tl.list
				break
			}
		}
	}
	if list == "" || id == "" {
		return
	}
	from, ok := g.index[key(project, list, id)]
	if !ok {
		m := g.p.item(project, list, id)
		if m == nil {
			return
		}
		kind := ""
		for _, tl := range timeLists {
			if tl.list == list {
				kind = tl.kind
			}
		}
		from = g.item(project, list, id, kind, m)
		if field := map[string]string{"milestones": "timing", "deliverables": "due", "conditions": "due", "procurement": "requiredBy"}[list]; field != "" {
			g.nodes[from].Timing, _ = m[field].(map[string]any)
		}
	}
	g.edge(from, to)
	if g.nodes[to].Follows == "" {
		g.nodes[to].Follows = g.nodes[from].Name
	}
}

// mark finds what is late, unplaced or in conflict, and the chain that
// decides the last date: back from the latest item, through what each
// waits on that falls latest.
func (g *waits) mark() {
	into := make([][]int, len(g.nodes))
	for _, ed := range g.edges {
		into[ed.To] = append(into[ed.To], ed.From)
	}
	last := -1
	for i := range g.nodes {
		n := &g.nodes[i]
		t := timing.Read(n.Timing)
		n.Unplaced = n.Month == ""
		n.Late = t.Late
		// Its own date or window's end, against where what it waits on
		// puts it.
		if own := timing.Read(n.Timing); (own.Form == "date" || own.Form == "window") && own.Month != "" && n.Month > own.Month {
			n.Conflict = true
		}
		if n.Month != "" && (last < 0 || n.Month > g.nodes[last].Month) {
			last = i
		}
	}
	if len(g.nodes) < 2 {
		return
	}
	for at, steps := last, 0; at >= 0 && steps <= len(g.nodes); steps++ {
		g.nodes[at].Critical = true
		next := -1
		for _, f := range into[at] {
			if g.nodes[f].Month != "" && (next < 0 || g.nodes[f].Month > g.nodes[next].Month) {
				next = f
			}
		}
		at = next
	}
}

// placeWaits lays the graph out, a column to each month in order and the
// unplaced last, as the layout port places any graph.
func (e *Engine) placeWaits(g *waits) {
	months := map[string]bool{}
	for _, n := range g.nodes {
		if n.Month != "" {
			months[n.Month] = true
		}
	}
	order := make([]string, 0, len(months))
	for m := range months {
		order = append(order, m)
	}
	sort.Strings(order)
	column := map[string]int{}
	for i, m := range order {
		column[m] = i
	}
	nodes := make([]layout.Node, len(g.nodes))
	for i, n := range g.nodes {
		if n.Month == "" {
			nodes[i] = layout.Node{Group: len(order)}
		} else {
			nodes[i] = layout.Node{Group: column[n.Month]}
		}
	}
	edges := make([]layout.Edge, len(g.edges))
	for i, ed := range g.edges {
		edges[i] = layout.Edge{S: ed.From, T: ed.To}
	}
	if e.layout == nil {
		return
	}
	for i, pt := range e.layout.Place(nodes, edges) {
		g.nodes[i].X, g.nodes[i].Y = pt.X, pt.Y
	}
}

// eventOf is the event a timing follows or is set by, if any.
func eventOf(tm map[string]any) map[string]any {
	if tm == nil {
		return nil
	}
	if f, _ := tm["form"].(string); f != "after" && f != "when" {
		return nil
	}
	ev, _ := tm["event"].(map[string]any)
	return ev
}

// onProject is whether an event happens to the project, or to an item
// of it.
func onProject(ev map[string]any, project string) bool {
	on, _ := ev["on"].(map[string]any)
	k, _ := on["kind"].(string)
	id, _ := on["id"].(string)
	return k == "Project" && id == project
}

// baselineTiming reads a baseline not known yet as a timing set when its
// event happens, by the month it is expected.
func baselineTiming(v any) map[string]any {
	b, _ := v.(map[string]any)
	ev, _ := b["when"].(map[string]any)
	if ev == nil {
		return nil
	}
	out := map[string]any{"form": "when", "event": ev}
	if by, _ := b["expectedBy"].(string); by != "" {
		out["expectedBy"] = by
	}
	return out
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func stringList(v any) []string {
	items, _ := v.([]any)
	out := make([]string, 0, len(items))
	for _, it := range items {
		if s, ok := it.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// projectName is a project's name as ctx reads it, else its id.
func (e *Engine) projectName(ctx context.Context, id string) string {
	doc, found, err := e.docInPlay(ctx, "Project", id)
	if err != nil || !found {
		return id
	}
	return docName(doc, id)
}
