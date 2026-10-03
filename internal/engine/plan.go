package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// The work around a manifest. Defining one thing well means defining what
// it answers to as well: an objective is met through outcomes, an outcome
// is the closing of a gap, and a gap is only usable once it is measured
// and scoped. The flows say which links finish which side (a link's
// needs), so the order of work is read from the contract, never written
// per kind here, and every agent and interface is led through it alike.

// relation is one link a flow describes: Holder holds Path naming Named.
type relation struct {
	Holder, Path, Named string
	// Needs is the side the link finishes: "holder" or "named".
	Needs string
	// When is the named manifest's levels it applies at; Level is the
	// holder's level, for a Goal.
	When  *flowWhen
	Level string
	Check string
}

// relations reads every flow's links once.
func (e *Engine) relations() ([]relation, error) {
	var out []relation
	for _, kind := range graphKinds() {
		raw, found, err := e.FlowJSON(kind)
		if err != nil {
			return nil, err
		}
		if !found {
			continue
		}
		var flow struct {
			Spec struct {
				Steps []struct {
					Links []struct {
						Kind  string    `json:"kind"`
						Path  string    `json:"path"`
						When  *flowWhen `json:"when"`
						Check string    `json:"check"`
						Needs string    `json:"needs"`
						Level string    `json:"level"`
					} `json:"links"`
				} `json:"steps"`
			} `json:"spec"`
		}
		if err := json.Unmarshal(raw, &flow); err != nil {
			return nil, fmt.Errorf("flow %s: %w", kind, err)
		}
		for _, st := range flow.Spec.Steps {
			for _, l := range st.Links {
				if l.Needs == "" {
					continue
				}
				out = append(out, relation{Holder: l.Kind, Path: l.Path, Named: kind, Needs: l.Needs, When: l.When, Level: l.Level, Check: l.Check})
			}
		}
	}
	return out, nil
}

// PlanItem is one manifest the work needs besides the one being defined,
// in the order to settle them.
type PlanItem struct {
	Kind  string `json:"kind"`
	Level string `json:"level,omitempty"`
	// For is what it is needed for, as Kind or Kind (level), and Link how
	// the two are joined: Holder at Path names the other.
	For  string `json:"for"`
	Link string `json:"link"`
	// Check is the check that says whether the link is made.
	Check string `json:"check,omitempty"`
	// Ask is what to ask the person; IfNone what to do when nothing they
	// have fits.
	Ask    string `json:"ask,omitempty"`
	IfNone string `json:"ifNone,omitempty"`
	// Existing is the organisation's records of that kind (and level) to
	// offer before defining another.
	Existing []Candidate `json:"existing,omitempty"`
}

type planNode struct{ kind, level string }

func (n planNode) String() string {
	if n.level == "" {
		return n.kind
	}
	return n.kind + " (" + n.level + ")"
}

// Plan is the order of work for defining kind at level: everything the
// flows say it needs, deepest first, so the person is asked first about
// what the new thing answers to (for an objective: the gaps, how each is
// measured and where it was found, the outcome closing each) and the
// thing itself last.
func (e *Engine) Plan(ctx context.Context, kind, level, locale string) ([]PlanItem, error) {
	rels, err := e.relations()
	if err != nil {
		return nil, err
	}
	l := &lookup{ctx: ctx, store: e.manifests, codec: e.codec}
	words := map[string]GuideBundle{}
	bundle := func(k string) GuideBundle {
		if b, ok := words[k]; ok {
			return b
		}
		b, _, _ := GuideBundleFor(k, locale)
		words[k] = b
		return b
	}
	existing := func(k, lvl string) []Candidate {
		cs := e.candidates(l, k, k, "", "")
		if lvl == "" {
			return cs
		}
		out := cs[:0]
		for _, c := range cs {
			if c.Detail == lvl {
				out = append(out, c)
			}
		}
		return out
	}

	var out []PlanItem
	seen := map[planNode]bool{{kind, level}: true}
	var expand func(n planNode, depth int)
	expand = func(n planNode, depth int) {
		if depth > 4 {
			return
		}
		// What should name this one: settled first, each with its own
		// work before it.
		for _, r := range rels {
			if r.Needs != "named" || r.Named != n.kind || !r.When.applies(n.level) {
				continue
			}
			child := planNode{r.Holder, r.Level}
			if seen[child] {
				continue
			}
			seen[child] = true
			expand(child, depth+1)
			item := PlanItem{Kind: child.kind, Level: child.level, For: n.String(), Link: r.Holder + " " + r.Path, Check: r.Check, Existing: existing(child.kind, child.level)}
			if w, ok := bundle(n.kind).Links[r.Holder+":"+r.Path]; ok {
				item.Ask, item.IfNone = w.Ask, w.IfNone
			}
			out = append(out, item)
			e.holderNeeds(rels, child, &out, seen, bundle, existing)
		}
	}
	expand(planNode{kind, level}, 0)
	e.holderNeeds(rels, planNode{kind, level}, &out, seen, bundle, existing)
	return out, nil
}

// holderNeeds adds what n should name itself: a gap's indicator and
// segments, a KPI's source and cycle.
func (e *Engine) holderNeeds(rels []relation, n planNode, out *[]PlanItem, seen map[planNode]bool, bundle func(string) GuideBundle, existing func(string, string) []Candidate) {
	for _, r := range rels {
		if r.Needs != "holder" || r.Holder != n.kind {
			continue
		}
		target := planNode{r.Named, ""}
		if seen[target] {
			continue
		}
		seen[target] = true
		item := PlanItem{Kind: target.kind, For: n.String(), Link: r.Holder + " " + r.Path, Check: r.Check, Existing: existing(target.kind, "")}
		if w, ok := bundle(n.kind).Fields[r.Path]; ok {
			item.Ask = w.Guide
		}
		*out = append(*out, item)
		// And what that one needs in turn: a KPI's source and cycle.
		e.holderNeeds(rels, target, out, seen, bundle, existing)
	}
}

// Around is one manifest joined to another along the work's links, with
// what it still lacks.
type Around struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
	// Link is how it is joined: Holder at Path names the other.
	Link string `json:"link"`
	// Draft is true while it has no version, or a draft newer than one.
	Draft bool    `json:"draft,omitempty"`
	Open  []Check `json:"open"`
}

// WorkAround returns the manifests joined to kind/id along the links the
// flows say finish the work, as far as they reach, each with its open
// checks: so whoever is defining an outcome is told that the gap it
// closes has no indicator yet. Saved links are read from the index;
// drafts are read where they are, and also names more manifests to read
// as drafts (what an agent is defining together), checked as one set.
func (e *Engine) WorkAround(ctx context.Context, kind, id string, also []Ref) ([]Around, error) {
	rels, err := e.relations()
	if err != nil {
		return nil, err
	}
	// Every manifest in play, as it stands now (its draft, where there is
	// one), checked together as a proposal would be.
	docs := map[string]map[string]any{}
	drafts := map[string]bool{}
	read := func(k, i string) map[string]any {
		key := k + "/" + i
		if d, ok := docs[key]; ok {
			return d
		}
		text, found, err := e.manifests.GetWorking(ctx, k, i)
		if err == nil && found {
			drafts[key] = true
		} else {
			v, ok, err := e.manifests.GetCurrent(ctx, k, i)
			if err != nil || !ok {
				docs[key] = nil
				return nil
			}
			text = v.YAML
		}
		var d map[string]any
		if e.codec.DecodeInto(text, &d) != nil {
			d = nil
		}
		docs[key] = d
		return d
	}
	levelOf := func(d map[string]any) string {
		spec, _ := d["spec"].(map[string]any)
		s, _ := spec["level"].(string)
		return s
	}
	for _, r := range also {
		read(r.Kind, r.ID)
	}
	start := read(kind, id)
	if start == nil {
		return nil, fmt.Errorf("%w: %s/%s", ErrNotFound, kind, id)
	}

	type found struct {
		ref  Ref
		link string
	}
	var order []found
	visited := map[Ref]bool{{Kind: kind, ID: id}: true}
	queue := []Ref{{Kind: kind, ID: id}}
	for depth := 0; len(queue) > 0 && depth < 5 && len(order) < 30; depth++ {
		var next []Ref
		for _, at := range queue {
			doc := docs[at.Kind+"/"+at.ID]
			if doc == nil {
				continue
			}
			add := func(r Ref, link string) {
				if visited[r] || read(r.Kind, r.ID) == nil {
					return
				}
				visited[r] = true
				order = append(order, found{r, link})
				next = append(next, r)
			}
			// What this one names that it needs, or that needs it.
			for _, f := range extractRefs(doc, e.refRules[at.Kind]) {
				for _, r := range rels {
					if r.Holder == at.Kind && r.Named == f.kind && strings.HasPrefix(f.path, r.Path) {
						add(Ref{Kind: f.kind, ID: f.id}, r.Holder+" "+r.Path)
						break
					}
				}
			}
			// What names this one along a link the work needs: saved, from
			// the index, and among the drafts in play.
			for _, r := range rels {
				if r.Named != at.Kind || (r.Needs == "named" && !r.When.applies(levelOf(doc))) {
					continue
				}
				link := r.Holder + " " + r.Path
				if saved, err := e.manifests.ListReferencing(ctx, at.Kind, at.ID); err == nil {
					for _, s := range saved {
						if s.Kind == r.Holder {
							add(Ref{Kind: s.Kind, ID: s.ID}, link)
						}
					}
				}
				for key, d := range docs {
					k, i, _ := strings.Cut(key, "/")
					if k != r.Holder || d == nil {
						continue
					}
					for _, f := range extractRefs(d, e.refRules[k]) {
						if f.kind == at.Kind && f.id == at.ID && strings.HasPrefix(f.path, r.Path) {
							add(Ref{Kind: k, ID: i}, link)
						}
					}
				}
			}
		}
		queue = next
	}

	checked := withProposed(ctx, docs)
	out := make([]Around, 0, len(order))
	for _, f := range order {
		key := f.ref.Kind + "/" + f.ref.ID
		doc := docs[key]
		text, err := e.codec.Encode(doc)
		if err != nil {
			return nil, err
		}
		checks, err := e.ChecksOf(checked, f.ref.Kind, f.ref.ID, text)
		if err != nil {
			return nil, err
		}
		a := Around{Kind: f.ref.Kind, ID: f.ref.ID, Link: f.link, Draft: drafts[key], Open: []Check{}}
		if meta, ok := doc["metadata"].(map[string]any); ok {
			a.Name, _ = meta["name"].(string)
		}
		for _, c := range checks {
			if c.Open() {
				a.Open = append(a.Open, c)
			}
		}
		out = append(out, a)
	}
	sort.SliceStable(out, func(i, j int) bool { return len(out[i].Open) > 0 && len(out[j].Open) == 0 })
	return out, nil
}
