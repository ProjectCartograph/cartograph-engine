package engine

import (
	"context"
	"fmt"
	"sort"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/decide"
)

// What happened, and what it reaches (TAXONOMY.md D52, D59). A person says
// what happened in their own words; the engine finds the item of the
// project it happened to, through the decision model, and walks from it
// to everything that waits on it, so the person sees every item a trigger
// reaches and records what follows, in one change set, each follow-on
// event naming the trigger as its cause.

// Triggerable is an item of a project something can happen to.
type Triggerable struct {
	// Item is its list and id: milestones/start, risks/r1.
	Item string `json:"item"`
	// Kind is what it is: milestone, deliverable, condition, risk,
	// dependency or criterion.
	Kind string `json:"kind"`
	Name string `json:"name"`
	// Happens are what can be recorded as happening to it, as the event
	// log names them.
	Happens []string `json:"happens"`
	// Likelihood is how likely the decision model judges what happened to
	// be about it, where it was asked.
	Likelihood float64 `json:"likelihood,omitempty"`
}

// Happened is the items what happened may be about: ranked by the decision
// model, likeliest first, or every item, as listed, where none answered.
type Happened struct {
	Available bool          `json:"available"`
	Matches   []Triggerable `json:"matches"`
}

// triggerLists are a project's lists something can happen to, and what.
var triggerLists = []struct {
	list, kind, name string
	happens          []string
}{
	{"milestones", "milestone", "name", []string{"reached", "slipped"}},
	{"deliverables", "deliverable", "name", []string{"accepted", "rejected", "slipped"}},
	{"conditions", "condition", "action", []string{"met", "notMet"}},
	{"risks", "risk", "description", []string{"occurred"}},
	{"successCriteria", "criterion", "statement", []string{"met", "notMet"}},
}

// Triggerables are the items of a project something can happen to, in
// the order the project lists them.
func (e *Engine) Triggerables(ctx context.Context, project string) ([]Triggerable, error) {
	doc, found, err := e.docInPlay(ctx, "Project", project)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("%w: Project/%s", ErrNotFound, project)
	}
	spec := specOf(doc)
	var out []Triggerable
	for _, tl := range triggerLists {
		items, _ := spec[tl.list].([]any)
		for _, it := range items {
			m, _ := it.(map[string]any)
			id, _ := m["id"].(string)
			name, _ := m[tl.name].(string)
			if id == "" {
				continue
			}
			kind, happens := tl.kind, tl.happens
			if t, _ := m["type"].(string); tl.list == "risks" && t == "dependency" {
				// A dependency that fails to arrive in time occurred, as a
				// risk does, and slips what waits on it.
				kind = "dependency"
			}
			out = append(out, Triggerable{Item: tl.list + "/" + id, Kind: kind, Name: name, Happens: happens})
		}
	}
	return out, nil
}

// whatHappenedFloor is the likelihood below which an item is not offered
// as what happened was about.
const whatHappenedFloor = 0.5

// WhatHappened ranks a project's items by how likely what a person says
// happened is about each. Meaning in text is the decision model's to
// judge (docs/adr/0030): without one, every item is offered, unranked,
// for the person to pick.
func (e *Engine) WhatHappened(ctx context.Context, project, text string) (Happened, error) {
	items, err := e.Triggerables(ctx, project)
	if err != nil {
		return Happened{}, err
	}
	qs := map[string]decide.Question{}
	for i, it := range items {
		qs[fmt.Sprintf("t%d", i)] = decide.Question{Type: decide.Choice, Instructions: "Is what happened about this item of the project?",
			Options: []decide.Option{{Key: "about", Description: "about: " + it.Name}, {Key: "other", Description: "about something else"}}}
	}
	answers, ok := e.ask(ctx, text, qs)
	if !ok {
		return Happened{Available: false, Matches: items}, nil
	}
	out := Happened{Available: true, Matches: []Triggerable{}}
	for i, it := range items {
		it.Likelihood = answers[fmt.Sprintf("t%d", i)].Probabilities["about"]
		if it.Likelihood > whatHappenedFloor {
			out.Matches = append(out.Matches, it)
		}
	}
	sort.SliceStable(out.Matches, func(i, j int) bool { return out.Matches[i].Likelihood > out.Matches[j].Likelihood })
	return out, nil
}

// Affects is every item a trigger on item reaches: what waits on it,
// directly or through others, and for a risk, every item whose timing
// names it and what waits on those. In the order of the project's waits.
func (e *Engine) Affects(ctx context.Context, project, item string) ([]TimeNode, error) {
	g, err := e.Waits(ctx, project)
	if err != nil {
		return nil, err
	}
	next := make([][]int, len(g.Nodes))
	for _, ed := range g.Edges {
		next[ed.From] = append(next[ed.From], ed.To)
	}
	reached := map[int]bool{}
	var queue []int
	start := func(i int) {
		if !reached[i] {
			reached[i] = true
			queue = append(queue, i)
		}
	}
	for i, n := range g.Nodes {
		if n.Record.Kind == "Project" && n.Record.ID == project && n.Item == item {
			start(i)
		}
		for _, r := range n.riskItems {
			if "risks/"+r == item {
				start(i)
			}
		}
	}
	for len(queue) > 0 {
		i := queue[0]
		queue = queue[1:]
		for _, t := range next[i] {
			start(t)
		}
	}
	out := []TimeNode{}
	for i, n := range g.Nodes {
		// The item itself is the trigger, not something it reaches.
		if reached[i] && !(n.Record.Kind == "Project" && n.Record.ID == project && n.Item == item) {
			out = append(out, n)
		}
	}
	return out, nil
}
