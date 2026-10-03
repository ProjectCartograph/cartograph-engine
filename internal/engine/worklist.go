package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// The order of work across many manifests. A strategy is settled in
// phases, the same for a person in the editor and an agent over MCP: first
// what each thing is (a gap's states, an outcome's statement), then its
// numbers (baselines and targets, from the documents at hand), then how
// it links to the rest (which outcome a gap closes, which aims a KPI
// measures). Within a phase, what something answers to comes first: gaps
// before the outcomes they close, outcomes before their objectives. The
// flows say each check's phase (a field's, its step's, or a link's) and
// the plan says the order of kinds, so nothing here is written per kind.

var phaseOrder = map[string]int{"define": 0, "measure": 1, "align": 2}

// Task is one open check of one manifest, placed in the order of work.
type Task struct {
	Phase   string `json:"phase"`
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Name    string `json:"name,omitempty"`
	Check   string `json:"check"`
	State   string `json:"state"`
	Message string `json:"message"`
	// Step is the flow step that settles it, and Do what the guidance says
	// to do about it.
	Step string `json:"step,omitempty"`
	Do   string `json:"do,omitempty"`
	// Choices are the records that already exist to settle it with, where
	// a reference or a link settles it: offered before defining another.
	Choices []Candidate `json:"choices,omitempty"`
}

// Worklist is a piece of work's open checks, in order.
type Worklist struct {
	// Tasks are every open check, the first being what to do next.
	Tasks []Task `json:"tasks"`
	// Open counts what is left in each phase.
	Open map[string]int `json:"open"`
}

type checkPlace struct {
	phase, step string
	// order is the step's place in its flow.
	order int
	// field is the field that settles it, or for a link the kind that
	// holds it, prefixed "link:".
	field string
}

// checkPlaces reads, from every flow, the phase and step of each check:
// a field's own phase, else its step's; a link's, else align.
func (e *Engine) checkPlaces() (map[string]map[string]checkPlace, error) {
	out := map[string]map[string]checkPlace{}
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
					Key    string `json:"key"`
					Phase  string `json:"phase"`
					Fields []struct {
						Path   string   `json:"path"`
						Phase  string   `json:"phase"`
						Checks []string `json:"checks"`
					} `json:"fields"`
					Links []struct {
						Kind  string `json:"kind"`
						Phase string `json:"phase"`
						Check string `json:"check"`
					} `json:"links"`
				} `json:"steps"`
			} `json:"spec"`
		}
		if err := json.Unmarshal(raw, &flow); err != nil {
			return nil, fmt.Errorf("flow %s: %w", kind, err)
		}
		m := map[string]checkPlace{}
		or := func(p, def string) string {
			if p == "" {
				return def
			}
			return p
		}
		for i, st := range flow.Spec.Steps {
			// A check a step's section names, with no field claiming it.
			m["section:"+st.Key] = checkPlace{or(st.Phase, "define"), st.Key, i, ""}
			for _, f := range st.Fields {
				for _, c := range f.Checks {
					// A check several fields can settle (a gap's states,
					// written or read from its KPI) is placed at the
					// earliest of them.
					pl := checkPlace{or(f.Phase, or(st.Phase, "define")), st.Key, i, f.Path}
					if have, ok := m[c]; !ok || phaseOrder[pl.phase] < phaseOrder[have.phase] {
						m[c] = pl
					}
				}
			}
			for _, l := range st.Links {
				if l.Check != "" {
					m[l.Check] = checkPlace{or(l.Phase, "align"), st.Key, i, "link:" + l.Kind}
				}
			}
		}
		out[kind] = m
	}
	return out, nil
}

// kindRanks is the order kinds are settled in within a phase: the plan for
// a top-level goal, deepest first, then the goal, then every other kind.
func (e *Engine) kindRanks(ctx context.Context) map[string]int {
	ranks := map[string]int{}
	plan, _ := e.Plan(ctx, "Goal", "goal", DefaultLocale)
	for _, p := range plan {
		key := p.Kind
		if p.Level != "" {
			key += "/" + p.Level
		}
		if _, ok := ranks[key]; !ok {
			ranks[key] = len(ranks)
		}
	}
	for _, k := range []string{"Goal/goal", "Purpose"} {
		if _, ok := ranks[k]; !ok {
			ranks[k] = len(ranks)
		}
	}
	for _, k := range graphKinds() {
		if _, ok := ranks[k]; !ok {
			ranks[k] = len(ranks)
		}
	}
	return ranks
}

// Work lists the open checks of the manifests named and of everything the
// work joins them to, drafts included and checked as one set, in the order
// they are best settled.
func (e *Engine) Work(ctx context.Context, work []Ref, locale string) (Worklist, error) {
	places, err := e.checkPlaces()
	if err != nil {
		return Worklist{}, err
	}
	ranks := e.kindRanks(ctx)
	type record struct {
		ref   Ref
		name  string
		level string
		open  []Check
	}
	records := map[Ref]*record{}
	for _, w := range work {
		around, err := e.WorkAround(ctx, w.Kind, w.ID, work)
		if err != nil {
			return Worklist{}, err
		}
		for _, a := range around {
			r := Ref{Kind: a.Kind, ID: a.ID}
			if _, ok := records[r]; !ok {
				records[r] = &record{ref: r, name: a.Name, open: a.Open}
			}
		}
		if _, ok := records[w]; ok {
			continue
		}
		// The manifest itself, checked with the rest of the work.
		checks, name, level, err := e.checksInWork(ctx, w, work)
		if err != nil {
			return Worklist{}, err
		}
		var open []Check
		for _, c := range checks {
			if c.Open() {
				open = append(open, c)
			}
		}
		records[w] = &record{ref: w, name: name, level: level, open: open}
	}
	words := map[string]GuideBundle{}
	l := &lookup{ctx: ctx, store: e.manifests, codec: e.codec}
	out := Worklist{Tasks: []Task{}, Open: map[string]int{"define": 0, "measure": 0, "align": 0}}
	rankOf := map[Ref]int{}
	stepOrder := map[int]int{}
	for r, rec := range records {
		if rec.level == "" && r.Kind == "Goal" {
			_, _, rec.level, _ = e.checksInWork(ctx, r, work)
		}
		key := r.Kind
		if rec.level != "" {
			key += "/" + rec.level
		}
		rk, ok := ranks[key]
		if !ok {
			rk = ranks[r.Kind]
		}
		rankOf[r] = rk
		if _, ok := words[r.Kind]; !ok {
			words[r.Kind], _, _ = GuideBundleFor(r.Kind, locale)
		}
		for _, c := range rec.open {
			pl, ok := places[r.Kind][c.ID]
			if !ok {
				pl, ok = places[r.Kind]["section:"+c.Section]
			}
			if !ok {
				pl = checkPlace{"define", c.Section, 99, ""}
			}
			stepOrder[len(out.Tasks)] = pl.order
			out.Tasks = append(out.Tasks, Task{Phase: pl.phase, Kind: r.Kind, ID: r.ID, Name: rec.name, Check: c.ID, State: c.State,
				Message: c.Message, Step: pl.step, Do: words[r.Kind].Checks[c.ID], Choices: e.choices(l, r.Kind, rec.level, pl.field)})
			out.Open[pl.phase]++
		}
	}
	idx := make([]int, len(out.Tasks))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(x, y int) bool {
		i, j := idx[x], idx[y]
		a, b := out.Tasks[i], out.Tasks[j]
		if phaseOrder[a.Phase] != phaseOrder[b.Phase] {
			return phaseOrder[a.Phase] < phaseOrder[b.Phase]
		}
		ra, rb := rankOf[Ref{Kind: a.Kind, ID: a.ID}], rankOf[Ref{Kind: b.Kind, ID: b.ID}]
		if ra != rb {
			return ra < rb
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		if a.ID != b.ID {
			return a.ID < b.ID
		}
		if stepOrder[i] != stepOrder[j] {
			return stepOrder[i] < stepOrder[j]
		}
		return a.Check < b.Check
	})
	sorted := make([]Task, len(idx))
	for k, i := range idx {
		sorted[k] = out.Tasks[i]
	}
	out.Tasks = sorted
	return out, nil
}

// checksInWork checks one manifest as it stands, with the rest of the
// work's drafts in play, and says its name and, for a goal, its level.
func (e *Engine) checksInWork(ctx context.Context, r Ref, work []Ref) ([]Check, string, string, error) {
	docs := map[string]map[string]any{}
	text := func(ref Ref) []byte {
		if t, found, err := e.manifests.GetWorking(ctx, ref.Kind, ref.ID); err == nil && found {
			return t
		}
		if v, ok, err := e.manifests.GetCurrent(ctx, ref.Kind, ref.ID); err == nil && ok {
			return v.YAML
		}
		return nil
	}
	for _, w := range append([]Ref{r}, work...) {
		if t := text(w); t != nil {
			var d map[string]any
			if e.codec.DecodeInto(t, &d) == nil {
				docs[w.Kind+"/"+w.ID] = d
			}
		}
	}
	own := text(r)
	if own == nil {
		return nil, "", "", fmt.Errorf("%w: %s/%s", ErrNotFound, r.Kind, r.ID)
	}
	doc := docs[r.Kind+"/"+r.ID]
	name, level := "", ""
	if meta, ok := doc["metadata"].(map[string]any); ok {
		name, _ = meta["name"].(string)
	}
	if spec, ok := doc["spec"].(map[string]any); ok {
		level, _ = spec["level"].(string)
	}
	checks, err := e.ChecksOf(withProposed(ctx, docs), r.Kind, r.ID, own)
	return checks, name, level, err
}

// maxChoices bounds the records offered for one task; search finds more.
const maxChoices = 6

// choices are the existing records a task could be settled with: those a
// reference field names, or those that could hold a link.
func (e *Engine) choices(l *lookup, kind, level, field string) []Candidate {
	var cs []Candidate
	switch {
	case strings.HasPrefix(field, "link:"):
		holder := strings.TrimPrefix(field, "link:")
		cs = e.candidates(l, holder, "", "", "")
		// The goals that would sit under this one are of the level below.
		if holder == "Goal" && kind == "Goal" {
			below := map[string]string{"goal": "objective", "objective": "outcome"}[level]
			under := cs[:0]
			for _, c := range cs {
				if c.Detail == below {
					under = append(under, c)
				}
			}
			cs = under
		}
	case field != "":
		ref := e.refKindAt(kind, field)
		if ref == "" {
			// A list of references names its kind on its items.
			field += "/-"
			ref = e.refKindAt(kind, field)
		}
		if ref == "" || ref == "*" {
			return nil
		}
		cs = e.candidates(l, ref, kind, field, level)
	}
	if len(cs) > maxChoices {
		cs = cs[:maxChoices]
	}
	return cs
}
