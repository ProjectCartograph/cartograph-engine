package engine

import (
	"context"
	"sort"
)

// Where a node sits in a project's data lineage, from left to right.
const (
	LineageUpstream   = "upstream"   // a project that produces data this one uses
	LineageSource     = "source"     // a data source this project uses
	LineageProject    = "project"    // the project itself
	LineageOutput     = "output"     // a data source this project produces into
	LineageDownstream = "downstream" // a KPI or a project that reads what it produces
)

var lineageColumn = map[string]int{LineageUpstream: 0, LineageSource: 1, LineageProject: 2, LineageOutput: 3, LineageDownstream: 4}

// LineageNode is one thing in a project's data lineage, placed in its
// column (X) and its row in that column (Y).
type LineageNode struct {
	Kind, ID, Name string
	Role           string
	X, Y           float64
}

// Lineage is a project's data lineage: where its data comes from, what
// it produces, and what reads that, every edge in the direction the
// data flows.
type Lineage struct {
	Nodes []LineageNode
	Edges []GraphEdge
}

// LineageOf draws a project's data lineage from the data it uses and
// produces as given (the project as it is being edited, which may not be
// saved yet), and the rest of the workspace as ctx reads it: the projects
// producing what it uses, and the KPIs and projects reading what it
// produces. The engine places it, the same way every time.
func (e *Engine) LineageOf(ctx context.Context, id, name string, uses, produces []string) (Lineage, error) {
	names := map[Ref]string{}
	read := func(kind string) ([]inPlayDoc, error) {
		docs, err := e.allInPlay(ctx, kind)
		for _, d := range docs {
			names[Ref{Kind: kind, ID: d.id}] = nameOf(d.doc, d.id)
		}
		return docs, err
	}
	if _, err := read("DataSource"); err != nil {
		return Lineage{}, err
	}
	projects, err := read("Project")
	if err != nil {
		return Lineage{}, err
	}
	kpis, err := read("KPI")
	if err != nil {
		return Lineage{}, err
	}
	var out Lineage
	at := map[Ref]bool{}
	add := func(r Ref, role string) {
		if at[r] {
			return
		}
		at[r] = true
		n := names[r]
		if n == "" {
			n = r.ID
		}
		out.Nodes = append(out.Nodes, LineageNode{Kind: r.Kind, ID: r.ID, Name: n, Role: role})
	}
	edge := func(from, to Ref) { out.Edges = append(out.Edges, GraphEdge{From: from, To: to}) }
	self := Ref{Kind: "Project", ID: id}
	names[self] = name
	add(self, LineageProject)
	used, made := map[string]bool{}, map[string]bool{}
	for _, s := range uses {
		if s == "" || used[s] {
			continue
		}
		used[s] = true
		r := Ref{Kind: "DataSource", ID: s}
		add(r, LineageSource)
		edge(r, self)
	}
	for _, s := range produces {
		if s == "" || made[s] {
			continue
		}
		made[s] = true
		r := Ref{Kind: "DataSource", ID: s}
		if !at[r] {
			add(r, LineageOutput)
		}
		edge(self, r)
	}
	for _, d := range projects {
		if d.id == id {
			continue
		}
		p := Ref{Kind: "Project", ID: d.id}
		ins, outs := dataOf(specOf(d.doc))
		for _, s := range outs {
			if used[s] {
				add(p, LineageUpstream)
				edge(p, Ref{Kind: "DataSource", ID: s})
			}
		}
		for _, s := range ins {
			if made[s] {
				add(p, LineageDownstream)
				edge(Ref{Kind: "DataSource", ID: s}, p)
			}
		}
	}
	for _, d := range kpis {
		k := Ref{Kind: "KPI", ID: d.id}
		for _, s := range stringsOf(specOf(d.doc)["sources"]) {
			if made[s] {
				add(k, LineageDownstream)
				edge(Ref{Kind: "DataSource", ID: s}, k)
			}
		}
	}
	placeLineage(&out)
	return out, nil
}

// dataOf is the data sources a project's spec uses and produces into.
func dataOf(spec map[string]any) (uses, produces []string) {
	data, _ := spec["data"].(map[string]any)
	for _, c := range listOf(data["consumes"]) {
		if s, _ := c["source"].(string); s != "" {
			uses = append(uses, s)
		}
	}
	for _, p := range listOf(data["produces"]) {
		if s, _ := p["sink"].(string); s != "" {
			produces = append(produces, s)
		}
	}
	return uses, produces
}

func listOf(v any) []map[string]any {
	items, _ := v.([]any)
	out := make([]map[string]any, 0, len(items))
	for _, it := range items {
		if m, ok := it.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// placeLineage puts each node in its column, left to right as the data
// flows, and orders each column by name, centred on the project's row.
func placeLineage(l *Lineage) {
	sort.SliceStable(l.Nodes, func(i, j int) bool {
		a, b := l.Nodes[i], l.Nodes[j]
		if lineageColumn[a.Role] != lineageColumn[b.Role] {
			return lineageColumn[a.Role] < lineageColumn[b.Role]
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Kind+"/"+a.ID < b.Kind+"/"+b.ID
	})
	count := map[int]int{}
	for _, n := range l.Nodes {
		count[lineageColumn[n.Role]]++
	}
	row := map[int]int{}
	for i := range l.Nodes {
		c := lineageColumn[l.Nodes[i].Role]
		l.Nodes[i].X = float64(c)
		l.Nodes[i].Y = float64(row[c]) - float64(count[c]-1)/2
		row[c]++
	}
	sort.Slice(l.Edges, func(i, j int) bool {
		a, b := l.Edges[i], l.Edges[j]
		if a.From != b.From {
			return a.From.Kind+"/"+a.From.ID < b.From.Kind+"/"+b.From.ID
		}
		return a.To.Kind+"/"+a.To.ID < b.To.Kind+"/"+b.To.ID
	})
}
