package engine

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// A link between two records, as a person draws it: from a node to
// another. Each kind of link names what it joins and is stored where the
// record that holds it keeps it.
const (
	LinkProblemGap      = "problem-gap"
	LinkProblemGroup    = "problem-group"
	LinkGapGroup        = "gap-group"
	LinkGapOutcome      = "gap-outcome"
	LinkGoalParent      = "goal-parent"
	LinkGoalContributes = "goal-contributes"
	LinkKPIGap          = "kpi-gap"
	LinkProjectOutcome  = "project-outcome"
)

// ErrUnknownLink is a link kind the engine does not know.
var ErrUnknownLink = fmt.Errorf("unknown link")

// LinkCandidate is one record a link could reach from where it starts:
// whether it may be made, whether it already is, and why not.
type LinkCandidate struct {
	Kind    string
	ID      string
	Name    string
	Allowed bool
	Linked  bool
	Reason  string
}

// linkTargets is the kind each link reaches.
var linkTargets = map[string]string{
	LinkProblemGap:      "Gap",
	LinkProblemGroup:    "BeneficiaryGroup",
	LinkGapGroup:        "BeneficiaryGroup",
	LinkGapOutcome:      "Goal",
	LinkGoalParent:      "Goal",
	LinkGoalContributes: "Goal",
	LinkKPIGap:          "Gap",
	LinkProjectOutcome:  "Goal",
}

// linkSources is the kind each link starts from.
var linkSources = map[string]string{
	LinkProblemGap:      "Project",
	LinkProblemGroup:    "Project",
	LinkGapGroup:        "Gap",
	LinkGapOutcome:      "Gap",
	LinkGoalParent:      "Goal",
	LinkGoalContributes: "Goal",
	LinkKPIGap:          "KPI",
	LinkProjectOutcome:  "Project",
}

// LinkCandidates is every record a link of the given kind could reach
// from the record from (and, for a project's problem, that problem), each
// marked allowed or not with the reason, read as ctx reads the workspace,
// a change set's drafts included. The rules are the ones the checks hold
// (TAXONOMY.md D24, D28, D45), asked before the link is drawn rather than
// after it is saved.
func (e *Engine) LinkCandidates(ctx context.Context, link, from, problem string) ([]LinkCandidate, error) {
	target, ok := linkTargets[link]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownLink, link)
	}
	source, found, err := e.docInPlay(ctx, linkSources[link], from)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("%w: %s/%s", ErrNotFound, linkSources[link], from)
	}
	targets, err := e.allInPlay(ctx, target)
	if err != nil {
		return nil, err
	}
	spec := specOf(source)
	var rule func(id string, doc map[string]any) (linked bool, reason string)
	switch link {
	case LinkProblemGap, LinkProblemGroup:
		p := problemOf(spec, problem)
		if p == nil {
			return nil, fmt.Errorf("%w: problem %q of Project/%s", ErrNotFound, problem, from)
		}
		groups := stringsOf(p["groups"])
		cited := citedGaps(p)
		if link == LinkProblemGap {
			rule = func(id string, doc map[string]any) (bool, string) {
				if contains(cited, id) {
					return true, ""
				}
				affects := stringsOf(specOf(doc)["affects"])
				if len(affects) > 0 && len(groups) > 0 && !overlaps(affects, groups) {
					return false, "This gap affects none of the groups this problem names."
				}
				return false, ""
			}
			break
		}
		affected, known, err := e.affectedBy(ctx, cited)
		if err != nil {
			return nil, err
		}
		rule = func(id string, _ map[string]any) (bool, string) {
			if contains(groups, id) {
				return true, ""
			}
			if known && !affected[id] {
				return false, "No gap this problem cites affects this group."
			}
			return false, ""
		}
	case LinkGapGroup:
		affects := stringsOf(spec["affects"])
		rule = func(id string, _ map[string]any) (bool, string) { return contains(affects, id), "" }
	case LinkGapOutcome:
		outcomes := stringsOf(spec["outcomes"])
		rule = func(id string, doc map[string]any) (bool, string) {
			if contains(outcomes, id) {
				return true, ""
			}
			if level, _ := specOf(doc)["level"].(string); level != "outcome" {
				return false, "A gap closes into an outcome, not " + aWord(levelWord(level)) + "."
			}
			return false, ""
		}
	case LinkProjectOutcome:
		alignment, _ := spec["alignment"].(map[string]any)
		goals := stringsOf(alignment["goals"])
		rule = func(id string, doc map[string]any) (bool, string) {
			if contains(goals, id) {
				return true, ""
			}
			if level, _ := specOf(doc)["level"].(string); level != "outcome" {
				return false, "A project serves an outcome, not " + aWord(levelWord(level)) + "."
			}
			return false, ""
		}
	case LinkGoalParent, LinkGoalContributes:
		level, _ := spec["level"].(string)
		parent, _ := spec["parent"].(string)
		below := descendants(targets, from)
		var contributes []string
		if cs, ok := spec["contributesTo"].([]any); ok {
			for _, c := range cs {
				if cm, ok := c.(map[string]any); ok {
					if g, _ := cm["goal"].(string); g != "" {
						contributes = append(contributes, g)
					}
				}
			}
		}
		rule = func(id string, doc map[string]any) (bool, string) {
			if id == from {
				return false, "An aim cannot link to itself."
			}
			theirs, _ := specOf(doc)["level"].(string)
			if link == LinkGoalParent {
				if id == parent {
					return true, ""
				}
				if want := parentLevel(level); want != theirs {
					if want == "" {
						return false, "A goal sits at the top: it has no aim above it."
					}
					return false, capitalFirst(aWord(levelWord(level))) + " sits under " + aWord(levelWord(want)) + ", not " + aWord(levelWord(theirs)) + "."
				}
			} else {
				if contains(contributes, id) {
					return true, ""
				}
				if level != "outcome" {
					return false, "Only an outcome leads to other aims besides its own."
				}
				if id == parent {
					return false, "It already sits under this aim."
				}
			}
			if below[id] {
				return false, "That aim sits under this one, so the link would make a loop."
			}
			return false, ""
		}
	case LinkKPIGap:
		rule = func(id string, doc map[string]any) (bool, string) {
			measure, _ := specOf(doc)["measure"].(string)
			if measure == from {
				return true, ""
			}
			if measure != "" {
				return false, "Another indicator already measures this gap."
			}
			return false, ""
		}
	}
	out := make([]LinkCandidate, 0, len(targets))
	for _, t := range targets {
		linked, reason := rule(t.id, t.doc)
		out = append(out, LinkCandidate{Kind: target, ID: t.id, Name: nameOf(t.doc, t.id), Linked: linked, Allowed: !linked && reason == "", Reason: reason})
	}
	return out, nil
}

type inPlayDoc struct {
	id  string
	doc map[string]any
}

// docInPlay is a record as ctx reads it: a change set's draft where it
// has one, else its current version.
func (e *Engine) docInPlay(ctx context.Context, kind, id string) (map[string]any, bool, error) {
	v, found, err := e.currentInPlay(ctx, kind, id)
	if err != nil || !found {
		return nil, found, err
	}
	var doc map[string]any
	if err := e.codec.DecodeInto(e.normalizeLegacy(kind, v.YAML), &doc); err != nil {
		return nil, false, fmt.Errorf("parse %s/%s: %w", kind, id, err)
	}
	return doc, true, nil
}

// allInPlay is every record of a kind as ctx reads it, the ones a change
// set creates included, sorted by id.
func (e *Engine) allInPlay(ctx context.Context, kind string) ([]inPlayDoc, error) {
	sums, err := e.List(ctx, kind, Filter{}, false)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(sums))
	seen := map[string]bool{}
	for _, s := range sums {
		ids = append(ids, s.ID)
		seen[s.ID] = true
	}
	for _, r := range inPlayRefs(ctx) {
		if r.Kind == kind && !seen[r.ID] {
			ids = append(ids, r.ID)
			seen[r.ID] = true
		}
	}
	versions, err := e.GetMany(ctx, kind, ids)
	if err != nil {
		return nil, err
	}
	out := make([]inPlayDoc, 0, len(versions))
	for _, v := range versions {
		var doc map[string]any
		if e.codec.DecodeInto(v.YAML, &doc) != nil {
			continue
		}
		out = append(out, inPlayDoc{id: v.ID, doc: doc})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out, nil
}

// affectedBy is the groups the given gaps affect, and whether any of them
// names any at all.
func (e *Engine) affectedBy(ctx context.Context, gaps []string) (map[string]bool, bool, error) {
	out := map[string]bool{}
	known := false
	for _, g := range gaps {
		doc, found, err := e.docInPlay(ctx, "Gap", g)
		if err != nil {
			return nil, false, err
		}
		if !found {
			continue
		}
		for _, a := range stringsOf(specOf(doc)["affects"]) {
			out[a] = true
			known = true
		}
	}
	return out, known, nil
}

// descendants is every aim under from, by parent, among the goals given.
func descendants(goals []inPlayDoc, from string) map[string]bool {
	children := map[string][]string{}
	for _, g := range goals {
		if p, _ := specOf(g.doc)["parent"].(string); p != "" {
			children[p] = append(children[p], g.id)
		}
	}
	out := map[string]bool{}
	stack := []string{from}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, c := range children[n] {
			if !out[c] {
				out[c] = true
				stack = append(stack, c)
			}
		}
	}
	return out
}

// parentLevel is the level an aim of the given level sits under, or ""
// for a goal, which sits under none.
func parentLevel(level string) string {
	switch level {
	case "outcome":
		return "objective"
	case "objective":
		return "goal"
	}
	return ""
}

// aWord is a word with its article: an outcome, a goal.
func aWord(w string) string {
	if w != "" && strings.ContainsRune("aeiou", rune(w[0])) {
		return "an " + w
	}
	return "a " + w
}

// capitalFirst starts a phrase with a capital, as a sentence does.
func capitalFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// levelWord is a level as a sentence says it.
func levelWord(level string) string {
	if level == "" {
		return "goal"
	}
	return level
}

func specOf(doc map[string]any) map[string]any {
	spec, _ := doc["spec"].(map[string]any)
	if spec == nil {
		return map[string]any{}
	}
	return spec
}

// problemOf is the problem with the given id in a project's spec, "#n"
// for the one at position n (a problem not yet given an id), or the first
// when id is empty.
func problemOf(spec map[string]any, id string) map[string]any {
	summary, _ := spec["summary"].(map[string]any)
	problems, _ := summary["problems"].([]any)
	if n, err := strconv.Atoi(strings.TrimPrefix(id, "#")); strings.HasPrefix(id, "#") && err == nil {
		if n >= 0 && n < len(problems) {
			pm, _ := problems[n].(map[string]any)
			return pm
		}
		return nil
	}
	for _, p := range problems {
		pm, ok := p.(map[string]any)
		if !ok {
			continue
		}
		if pid, _ := pm["id"].(string); id == "" || pid == id {
			return pm
		}
	}
	return nil
}

// citedGaps is the ids of the gaps a problem cites, in either form.
func citedGaps(problem map[string]any) []string {
	var out []string
	cites, _ := problem["gaps"].([]any)
	for _, c := range cites {
		id, _ := c.(string)
		if cm, ok := c.(map[string]any); ok {
			id, _ = cm["gap"].(string)
		}
		if id != "" {
			out = append(out, id)
		}
	}
	return out
}

func stringsOf(v any) []string {
	list, _ := v.([]any)
	out := make([]string, 0, len(list))
	for _, x := range list {
		if s, ok := x.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

func overlaps(a, b []string) bool {
	for _, x := range a {
		if contains(b, x) {
			return true
		}
	}
	return false
}
