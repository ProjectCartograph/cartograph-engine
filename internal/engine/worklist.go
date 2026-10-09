package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// The order of work across many manifests. The record is a directed
// acyclic graph (TAXONOMY.md D28), and the work walks it once from the
// top: what a thing names comes before it, so each manifest is finished
// in one visit (what it is, its numbers, and its links to what is already
// there) and nothing finished is opened again. A check that a later
// manifest settles by naming this one (an outcome waits for the gap that
// names it, a goal for the KPI that measures it) is placed where that
// later manifest is written, so it reads as the next thing to write, not
// as something to go back to. Within one manifest the flow's own order
// holds: its phases (define, measure, align) and then its steps. The
// flows say each check's phase and step and the order says the order of
// kinds, so nothing here is written per kind.

var phaseOrder = map[string]int{"define": 0, "measure": 1, "align": 2}

// aimMeasureChecks are the checks an aim's measures settle.
var aimMeasureChecks = map[string]bool{"smart-measurable": true, "smart-attainable": true, "smart-time-bound": true}

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
	// Field is the field that settles it, as a JSON pointer, where one
	// field does.
	Field string `json:"field,omitempty"`
	// By is the kind written to settle it, where that is another kind (a
	// gap settles an outcome's closes-gap): the stage it waits for.
	By string `json:"by,omitempty"`
	// Choices are the records that already exist to settle it with, where
	// a reference or a link settles it: offered before defining another.
	Choices []Candidate `json:"choices,omitempty"`
	// Stage is its place in the order of kinds, the directed acyclic
	// graph's rank: what a stage names is in an earlier one.
	Stage int `json:"-"`
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
	// holds it, prefixed "link:", and level that kind's level, for a Goal.
	field, level string
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
						Level string `json:"level"`
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
			m["section:"+st.Key] = checkPlace{or(st.Phase, "define"), st.Key, i, "", ""}
			for _, f := range st.Fields {
				for _, c := range f.Checks {
					// A check several fields can settle (a gap's states,
					// written or read from its KPI) is placed at the
					// earliest of them.
					pl := checkPlace{or(f.Phase, or(st.Phase, "define")), st.Key, i, f.Path, ""}
					if have, ok := m[c]; !ok || phaseOrder[pl.phase] < phaseOrder[have.phase] {
						m[c] = pl
					}
				}
			}
			for _, l := range st.Links {
				if l.Check == "" {
					continue
				}
				// A check several kinds can settle (a gap's coverage, by a
				// project or a programme) is placed with the first the flow
				// names.
				if have, ok := m[l.Check]; ok && strings.HasPrefix(have.field, "link:") {
					continue
				}
				m[l.Check] = checkPlace{or(l.Phase, "align"), st.Key, i, "link:" + l.Kind, l.Level}
			}
		}
		out[kind] = m
	}
	return out, nil
}

// placeOf is where a task falls in the order of work: at its own
// manifest's place, or for a check settled by a link, at the place of the
// kind that holds the link and is written to make it.
func placeOf(kind, level string, pl checkPlace) int {
	if holder, ok := strings.CutPrefix(pl.field, "link:"); ok {
		return rank(holder, pl.level)
	}
	return rank(kind, level)
}

// Work lists the open checks of the manifests named and of everything the
// work joins them to, drafts included and checked as one set, in the order
// they are best settled.
func (e *Engine) Work(ctx context.Context, work []Ref, locale string) (Worklist, error) {
	places, err := e.checkPlaces()
	if err != nil {
		return Worklist{}, err
	}
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
				var open []Check
				for _, c := range a.Open {
					if leftFor(ctx, a.Kind, a.ID, c.ID) == "" {
						open = append(open, c)
					}
				}
				records[r] = &record{ref: r, name: a.Name, open: open}
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
			// A check left for the person is not the agent's next task.
			if c.Open() && leftFor(ctx, w.Kind, w.ID, c.ID) == "" {
				open = append(open, c)
			}
		}
		records[w] = &record{ref: w, name: name, level: level, open: open}
	}
	words := map[string]GuideBundle{}
	l := &lookup{ctx: ctx, store: e.manifests, codec: e.codec}
	out := Worklist{Tasks: []Task{}, Open: map[string]int{"define": 0, "measure": 0, "align": 0}}
	// Where each task falls: its place in the order, and the place of its
	// own manifest, which keeps a manifest's tasks together.
	placed := map[int]int{}
	own := map[int]int{}
	stepOrder := map[int]int{}
	for r, rec := range records {
		if rec.level == "" && r.Kind == "Goal" {
			_, _, rec.level, _ = e.checksInWork(ctx, r, work)
		}
		if _, ok := words[r.Kind]; !ok {
			words[r.Kind], _, _ = GuideBundleFor(r.Kind, locale)
		}
		for _, c := range rec.open {
			pl, ok := places[r.Kind][c.ID]
			if !ok {
				pl, ok = places[r.Kind]["section:"+c.Section]
			}
			if !ok {
				pl = checkPlace{"define", c.Section, 99, "", ""}
			}
			if r.Kind == "Goal" && aimMeasureChecks[c.ID] && !e.hasKeyResults(ctx, r) {
				// An aim is measured by its key results or by the KPIs
				// aligned to it, and those are written after the aims:
				// asked where the KPIs are, in the step that holds them.
				pl = checkPlace{"measure", "measures", pl.order, "link:KPI", ""}
			}
			if r.Kind == "Goal" && c.ID == "outcomes-close-gaps" {
				// Settled by the gaps its outcomes close, written later.
				pl = checkPlace{"align", "measures", pl.order, "link:Gap", ""}
			}
			placed[len(out.Tasks)] = placeOf(r.Kind, rec.level, pl)
			own[len(out.Tasks)] = rank(r.Kind, rec.level)
			if r.Kind == "Goal" && (pl.field == "link:KPI" && aimMeasureChecks[c.ID] || c.ID == "outcomes-close-gaps") {
				// After the KPIs' own tasks, once one can be aligned, and
				// among themselves from the top of the tree down.
				own[len(out.Tasks)] = 1<<20 + rank(r.Kind, rec.level)
			}
			stepOrder[len(out.Tasks)] = pl.order
			by, _ := strings.CutPrefix(pl.field, "link:")
			if by == pl.field || by == r.Kind {
				by = ""
			}
			out.Tasks = append(out.Tasks, Task{Phase: pl.phase, Kind: r.Kind, ID: r.ID, Name: rec.name, Check: c.ID, State: c.State,
				Message: c.Message, Step: pl.step, Do: words[r.Kind].Checks[c.ID], By: by, Choices: e.choices(l, r.Kind, rec.level, pl.field), Field: fieldOf(pl.field), Stage: rank(r.Kind, rec.level)})
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
		if placed[i] != placed[j] {
			return placed[i] < placed[j]
		}
		if own[i] != own[j] {
			return own[i] < own[j]
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		if a.ID != b.ID {
			return a.ID < b.ID
		}
		if phaseOrder[a.Phase] != phaseOrder[b.Phase] {
			return phaseOrder[a.Phase] < phaseOrder[b.Phase]
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
	text := func(ref Ref) []byte { return e.workText(ctx, ref) }
	for _, w := range append(append([]Ref{r}, work...), inPlayRefs(ctx)...) {
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

// hasKeyResults reports whether an aim, as the work holds it, measures
// itself; one that does not waits for the KPIs aligned to it.
func (e *Engine) hasKeyResults(ctx context.Context, r Ref) bool {
	text, ok := inPlay(ctx, r.Kind, r.ID)
	if !ok {
		var found bool
		var err error
		if text, found, err = e.manifests.GetWorking(ctx, r.Kind, r.ID); err != nil || !found {
			doc, _, _ := e.currentDoc(ctx, r.Kind, r.ID)
			spec, _ := doc["spec"].(map[string]any)
			krs, _ := spec["keyResults"].([]any)
			return len(krs) > 0
		}
	}
	var doc map[string]any
	if err := e.codec.DecodeInto(text, &doc); err != nil {
		return false
	}
	spec, _ := doc["spec"].(map[string]any)
	krs, _ := spec["keyResults"].([]any)
	return len(krs) > 0
}

// OpenNow is what is open across a change set that can be settled now:
// what propose would refuse, less what is left for the person and what
// waits on a stage not written yet (an aim's measures before any KPI, an
// outcome's gap before any gap).
func (e *Engine) OpenNow(ctx context.Context, set string) ([]OpenCheck, error) {
	open, err := e.OpenInChangeSet(ctx, set)
	if err != nil {
		return nil, err
	}
	places, err := e.checkPlaces()
	if err != nil {
		return nil, err
	}
	var out []OpenCheck
	for _, oc := range open {
		if oc.Left != "" {
			continue
		}
		waitsOn := ""
		if pl, ok := places[oc.Kind][oc.ID]; ok {
			waitsOn, _ = strings.CutPrefix(pl.field, "link:")
			if waitsOn == pl.field {
				waitsOn = ""
			}
		}
		switch {
		case oc.Kind == "Goal" && aimMeasureChecks[oc.ID]:
			waitsOn = "KPI"
		case oc.Kind == "Goal" && oc.ID == "outcomes-close-gaps":
			waitsOn = "Gap"
		}
		if waitsOn != "" && waitsOn != oc.Kind && !e.HasAny(ctx, waitsOn) {
			continue
		}
		out = append(out, oc)
	}
	return out, nil
}

// fieldOf is a place's field when it is one, not a link.
func fieldOf(f string) string {
	if strings.HasPrefix(f, "/") {
		return f
	}
	return ""
}

// workText is a record as the work has it: in play on ctx, else its
// working copy, else its current version; nil when there is none.
func (e *Engine) workText(ctx context.Context, ref Ref) []byte {
	if t, ok := inPlay(ctx, ref.Kind, ref.ID); ok {
		return t
	}
	if t, found, err := e.manifests.GetWorking(ctx, ref.Kind, ref.ID); err == nil && found {
		return t
	}
	if v, ok, err := e.manifests.GetCurrent(ctx, ref.Kind, ref.ID); err == nil && ok {
		return v.YAML
	}
	return nil
}
