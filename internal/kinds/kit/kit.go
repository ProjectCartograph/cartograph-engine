// Package kit holds the small, dependency-free types that both the engine
// and the per-kind rule packages need. It exists so kind packages
// (internal/kinds/<kind>) can report problems and query other manifests
// without importing internal/engine, which would create an import cycle
// (engine imports the kinds registry to run their rules).
package kit

import (
	"fmt"
	"strings"
)

// Problem is one validation failure. Path is a JSON pointer into the
// manifest's spec (rooted at "/spec", to match paths from schema and
// reference validation).
type Problem struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// Lookup lets a kind's rules see other stored manifests, for cross-manifest
// checks.
type Lookup interface {
	// Documents returns the current parsed manifest document (the full
	// envelope: apiVersion, kind, metadata, spec) for every existing
	// manifest of the given kind, keyed by metadata.id.
	Documents(kind string) (map[string]map[string]any, error)
	// HasSnapshots returns true if the manifest of the given kind and id has
	// any immutable versions (version.Number >= 1). Returns false if not found.
	HasSnapshots(kind, id string) (bool, error)
}

// RuleContext carries what a kind's Rules function needs beyond the
// document being validated.
type RuleContext struct {
	// ID is the id of the manifest being validated. During an update this
	// is the existing manifest's id; rules that scan other manifests of the
	// same kind should exclude this id to avoid comparing a document
	// against itself.
	ID     string
	Lookup Lookup
}

// RulesFunc validates one kind's document (the full envelope, parsed as
// map[string]any) beyond what its JSON Schema and generic reference checks
// already cover.
type RulesFunc func(doc map[string]any, ctx RuleContext) []Problem

// KeyResultUnitProblem checks the one KeyResult rule its JSON Schema cannot
// express: unit is required when kind is count, money or duration, and
// forbidden when kind is percent or ratio. kr is one key result object
// (already known to be a map); path is its location in the manifest, for
// example "/spec/keyResults/0". Shared by every kind that embeds
// KeyResult (Goal, Project), so the rule reads the same everywhere.
func KeyResultUnitProblem(kr map[string]any, path string) *Problem {
	kind, _ := kr["kind"].(string)
	_, hasUnit := kr["unit"]
	switch kind {
	case "count", "money", "duration":
		if !hasUnit {
			return &Problem{Path: path + "/unit", Message: "unit is required when kind is count, money or duration"}
		}
	case "percent", "ratio":
		if hasUnit {
			return &Problem{Path: path + "/unit", Message: "unit is not allowed when kind is percent or ratio"}
		}
	}
	return nil
}

// ObjectiveDigitProblem flags an objective statement that contains a digit,
// a sign it states a target rather than a qualitative aim (the number
// belongs in a key result instead). path is the objective field's own
// location, for example "/spec/objective". Shared by Goal and Project,
// which both carry an objective statement.
func ObjectiveDigitProblem(objective, path string) *Problem {
	if strings.ContainsAny(objective, "0123456789") {
		return &Problem{
			Path:    path,
			Message: "An objective is qualitative; this reads like a key result. Put the number in a key result.",
		}
	}
	return nil
}

// LocalRefLists maps the name a local reference uses to where that list
// actually sits in a spec. Every entry is a direct child of spec except
// the timeline's phases, which are nested; naming the path here keeps the
// one exception in a table instead of in every reader.
var LocalRefLists = map[string][]string{
	"resources":       {"resources"},
	"objectives":      {"objectives"},
	"deliverables":    {"deliverables"},
	"successCriteria": {"successCriteria"},
	"risks":           {"risks"},
	"problems":        {"summary", "problems"},
	"phases":          {"timeline", "phases"},
}

// LocalList returns the items of one of a spec's own lists by the name a
// local reference uses for it, and whether that name is one Cartograph knows.
func LocalList(spec map[string]any, name string) ([]any, bool) {
	path, known := LocalRefLists[name]
	if !known {
		return nil, false
	}
	var node any = spec
	for _, step := range path[:len(path)-1] {
		m, ok := node.(map[string]any)
		if !ok {
			return nil, true
		}
		node = m[step]
	}
	m, ok := node.(map[string]any)
	if !ok {
		return nil, true
	}
	items, _ := m[path[len(path)-1]].([]any)
	return items, true
}

// LocalRefProblem checks one reference field: that it is a reference at
// all, and that a local one names a list Cartograph knows and an id that list
// actually holds. A cross-manifest reference is checked elsewhere, by the
// generic reference walker, and an external one names something the vault
// does not hold, so neither is resolved here. Returns nil when the field is
// absent, since whether a reference is required is each field's own rule.
func LocalRefProblem(spec map[string]any, v any, path string) *Problem {
	if v == nil {
		return nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return &Problem{Path: path, Message: "must be a reference, not a name"}
	}
	local, _ := m["local"].(string)
	if local == "" {
		return nil
	}
	id, _ := m["id"].(string)
	items, known := LocalList(spec, local)
	if !known {
		return &Problem{Path: path + "/local", Message: fmt.Sprintf("%q is not a list references can point at", local)}
	}
	for _, it := range items {
		if im, ok := it.(map[string]any); ok {
			if got, _ := im["id"].(string); got == id {
				return nil
			}
		}
	}
	return &Problem{Path: path + "/id", Message: fmt.Sprintf("references %s %q, which does not exist in this project", local, id)}
}

// RiskProblems checks the risk list a Project and a Programme both carry:
// that a dependency's far end resolves, that only a dependency carries an
// edge, that escalation states its reason, and that a phase named as the
// landing point is one the holder actually has.
//
// hasPhases says whether this kind schedules at all. A project does, so
// needBy names one of its own phases; a programme does not, and a phase it
// cannot have is refused rather than silently ignored — how a programme is
// staged is the delivery tool's, which is why it holds no timeline.
func RiskProblems(spec map[string]any, hasPhases bool) []Problem {
	risks, ok := spec["risks"].([]any)
	if !ok {
		return nil
	}
	phaseIDs := map[string]bool{}
	if hasPhases {
		phases, _ := LocalList(spec, "phases")
		for _, p := range phases {
			if pm, ok := p.(map[string]any); ok {
				if id, _ := pm["id"].(string); id != "" {
					phaseIDs[id] = true
				}
			}
		}
	}

	var problems []Problem
	for i, r := range risks {
		rm, ok := r.(map[string]any)
		if !ok {
			continue
		}
		path := fmt.Sprintf("/spec/risks/%d", i)

		if esc, ok := rm["escalate"].(map[string]any); ok {
			flag, _ := esc["flag"].(bool)
			reason, _ := esc["reason"].(string)
			if flag && reason == "" {
				problems = append(problems, Problem{
					Path:    path + "/escalate/reason",
					Message: "reason is required when escalate.flag is true",
				})
			}
		}

		dep, hasDep := rm["depends"].(map[string]any)
		if !hasDep {
			continue
		}
		if kindOf, _ := rm["type"].(string); kindOf != "dependency" {
			problems = append(problems, Problem{
				Path:    path + "/depends",
				Message: fmt.Sprintf("only a dependency carries an edge; this row is a %s", kindOf),
			})
		}
		if p := LocalRefProblem(spec, dep["on"], path+"/depends/on"); p != nil {
			problems = append(problems, *p)
		}
		needBy, _ := dep["needBy"].(string)
		if needBy == "" {
			continue
		}
		if !hasPhases {
			problems = append(problems, Problem{
				Path:    path + "/depends/needBy",
				Message: "names a phase, and this kind has no timeline to name one from",
			})
			continue
		}
		if !phaseIDs[needBy] {
			problems = append(problems, Problem{
				Path:    path + "/depends/needBy",
				Message: fmt.Sprintf("names phase %q, which this timeline does not have", needBy),
			})
		}
	}
	return problems
}

// GapCitationProblems checks the one thing a citation's schema cannot: that
// every segment it names is a segment the gap it cites actually has.
//
// A gap enumerates the slices its shortfall was observed in. Work that
// reaches some of them addresses part of the gap, which is what naming
// segments records. Naming a segment the gap does not have is not a
// narrower claim, it is a different one — the evidence never covered that
// slice, so nothing there is being addressed on this gap's authority
// (TAXONOMY.md D9).
//
// Shared by Project and Programme, which carry the same problems list, so
// the rule reads the same on both. An absent or empty segments list means
// the whole gap and is always allowed.
func GapCitationProblems(problems []any, path string, lookup Lookup) []Problem {
	if lookup == nil || len(problems) == 0 {
		return nil
	}
	gaps, err := lookup.Documents("Gap")
	if err != nil {
		return nil
	}
	segmentsOf := func(gapID string) (map[string]bool, bool) {
		doc, ok := gaps[gapID]
		if !ok {
			return nil, false
		}
		spec, _ := doc["spec"].(map[string]any)
		list, _ := spec["segments"].([]any)
		out := map[string]bool{}
		for _, s := range list {
			if id, ok := s.(string); ok {
				out[id] = true
			}
		}
		return out, true
	}

	var out []Problem
	for i, p := range problems {
		pm, ok := p.(map[string]any)
		if !ok {
			continue
		}
		cites, _ := pm["gaps"].([]any)
		for j, c := range cites {
			cm, ok := c.(map[string]any)
			if !ok {
				continue
			}
			gapID, _ := cm["gap"].(string)
			named, _ := cm["segments"].([]any)
			if gapID == "" || len(named) == 0 {
				continue
			}
			has, found := segmentsOf(gapID)
			if !found {
				// A gap that does not exist is the reference walker's to
				// report; saying it twice helps nobody.
				continue
			}
			for k, s := range named {
				id, _ := s.(string)
				if id == "" || has[id] {
					continue
				}
				where := fmt.Sprintf("%s/%d/gaps/%d/segments/%d", path, i, j, k)
				if len(has) == 0 {
					out = append(out, Problem{Path: where, Message: fmt.Sprintf(
						"gap %q names no segments, so it can only be cited whole", gapID)})
					continue
				}
				out = append(out, Problem{Path: where, Message: fmt.Sprintf(
					"gap %q was not observed in %q, so this work cannot address it there", gapID, id)})
			}
		}
	}
	return out
}

// ParentCycleProblems refuses a manifest of kind whose spec.parent would
// make it its own ancestor. A tree with a cycle is not a deep hierarchy,
// it is one with no top, and everything that walks it (teams' reach, a
// segment's labels) would walk it forever. noun names one in the message.
func ParentCycleProblems(doc map[string]any, ctx RuleContext, kind, noun string) []Problem {
	spec, _ := doc["spec"].(map[string]any)
	parent, _ := spec["parent"].(string)
	if parent == "" || ctx.Lookup == nil {
		return nil
	}
	if parent == ctx.ID {
		return []Problem{{Path: "/spec/parent", Message: "a " + noun + " cannot be its own parent"}}
	}
	others, err := ctx.Lookup.Documents(kind)
	if err != nil {
		return nil
	}
	parentOf := func(id string) string {
		s, _ := others[id]["spec"].(map[string]any)
		p, _ := s["parent"].(string)
		return p
	}
	// Walk up from the proposed parent. Bounded by the number of others,
	// so a cycle already in the store cannot hang this.
	seen := map[string]bool{ctx.ID: true}
	for at := parent; at != ""; at = parentOf(at) {
		if seen[at] {
			return []Problem{{Path: "/spec/parent", Message: fmt.Sprintf("this would make %q its own ancestor", ctx.ID)}}
		}
		seen[at] = true
		if len(seen) > len(others)+1 {
			break
		}
	}
	return nil
}

// ParentsCycleProblems is ParentCycleProblems for a kind that names its
// parents in a list (spec.<field>[]), as a sub-programme names its
// programmes and a portfolio the portfolios it is in: refused when any of
// them is, or has as an ancestor, the manifest itself. Several parents
// make a graph rather than a tree, so every path up is walked, each
// manifest once.
func ParentsCycleProblems(doc map[string]any, ctx RuleContext, kind, field, noun string) []Problem {
	spec, _ := doc["spec"].(map[string]any)
	parents := stringList(spec[field])
	if len(parents) == 0 {
		return nil
	}
	path := "/spec/" + field
	for i, p := range parents {
		if p == ctx.ID && p != "" {
			return []Problem{{Path: fmt.Sprintf("%s/%d", path, i), Message: "a " + noun + " cannot be part of itself"}}
		}
	}
	if ctx.Lookup == nil || ctx.ID == "" {
		return nil
	}
	others, err := ctx.Lookup.Documents(kind)
	if err != nil {
		return nil
	}
	parentsOf := func(id string) []string {
		s, _ := others[id]["spec"].(map[string]any)
		return stringList(s[field])
	}
	for i, start := range parents {
		seen := map[string]bool{}
		stack := []string{start}
		for len(stack) > 0 {
			at := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if at == ctx.ID {
				return []Problem{{Path: fmt.Sprintf("%s/%d", path, i), Message: fmt.Sprintf("this would make %q part of itself", ctx.ID)}}
			}
			if seen[at] {
				continue
			}
			seen[at] = true
			stack = append(stack, parentsOf(at)...)
		}
	}
	return nil
}

func stringList(v any) []string {
	list, _ := v.([]any)
	out := make([]string, 0, len(list))
	for _, x := range list {
		if s, ok := x.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}
