package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Placeholders, and the order agents are held to (TAXONOMY.md D31).
//
// The record is written in order: what a manifest names exists before it.
// A person sometimes reaches a reference before the thing it should name
// exists, mid-way through defining something else. Rather than leave the
// walk, they record a placeholder (metadata.pending): which field, the
// kind and the name of what is still to be defined. The field stays
// empty, the record's checks list the placeholder until the reference is
// made, and anything that needs the field (a handoff) still waits for it.
//
// An agent never needs one and is never allowed one. It works in its own
// change set, in the order of work, so whatever it names it defines first;
// a draft that names something neither in the record nor in its change
// set is refused, saying what to define first.

// pendingEntry is one placeholder.
type pendingEntry struct {
	Path, Kind, Name, Note string
}

// pendingOf reads a manifest's placeholders.
func pendingOf(doc map[string]any) []pendingEntry {
	meta, _ := doc["metadata"].(map[string]any)
	list, _ := meta["pending"].([]any)
	var out []pendingEntry
	for _, raw := range list {
		m, _ := raw.(map[string]any)
		if m == nil {
			continue
		}
		p := pendingEntry{}
		p.Path, _ = m["path"].(string)
		p.Kind, _ = m["kind"].(string)
		p.Name, _ = m["name"].(string)
		p.Note, _ = m["note"].(string)
		out = append(out, p)
	}
	return out
}

// refRulePath is a reference rule's path as a pattern over JSON pointers,
// a list's items matching any index.
func refRulePath(r refRule) *regexp.Regexp {
	var b strings.Builder
	b.WriteString("^")
	for _, s := range r.path {
		if s.wildcard {
			b.WriteString(`/\d+`)
		} else {
			b.WriteString("/" + regexp.QuoteMeta(escapePointerToken(s.prop)))
		}
	}
	b.WriteString("$")
	return regexp.MustCompile(b.String())
}

// pendingProblems refuses a placeholder that stands for no reference field
// of its kind, names the wrong kind, or stands for a field already filled:
// a placeholder holds the place of a reference not yet made.
func (e *Engine) pendingProblems(kind string, doc map[string]any) []Problem {
	var out []Problem
	filled := map[string]bool{}
	for _, f := range extractRefs(doc, e.refRules[kind]) {
		filled[f.path] = true
	}
	for i, p := range pendingOf(doc) {
		at := fmt.Sprintf("/metadata/pending/%d", i)
		target := ""
		for _, r := range e.refRules[kind] {
			// A placeholder for one item of a list stands at that item's
			// pointer; one for the list as a whole at the list's.
			whole := refRule{path: r.path, kind: r.kind}
			if n := len(r.path); n > 0 && r.path[n-1].wildcard {
				whole.path = r.path[:n-1]
			}
			if refRulePath(r).MatchString(p.Path) || refRulePath(whole).MatchString(p.Path) {
				target = r.kind
				break
			}
		}
		switch {
		case target == "":
			out = append(out, Problem{Path: at + "/path", Message: fmt.Sprintf("%s is not a reference a %s makes", p.Path, kind)})
		case target != "*" && target != p.Kind:
			out = append(out, Problem{Path: at + "/kind", Message: fmt.Sprintf("%s names a %s, not a %s", p.Path, target, p.Kind)})
		case filled[p.Path]:
			out = append(out, Problem{Path: at, Message: fmt.Sprintf("%s is already filled in; remove the placeholder for %s", p.Path, p.Name)})
		}
	}
	return out
}

// pendingCheck lists a record's placeholders as one check, so the person
// sees every reference still to make and what each waits on. It advises:
// a placeholder never refuses a save, and a handoff that needs the field
// is held by that field's own check.
func pendingCheck(doc map[string]any) (ProgrammeCheck, bool) {
	ps := pendingOf(doc)
	if len(ps) == 0 {
		return ProgrammeCheck{}, false
	}
	parts := make([]string, len(ps))
	for i, p := range ps {
		parts[i] = fmt.Sprintf("the %s %q (%s)", kindWord(p.Kind), p.Name, p.Path)
	}
	return ProgrammeCheck{ID: "pending", State: programmeCheckWarn,
		Message: fmt.Sprintf("Waiting on %s, not defined yet. Define %s, then name %s here.",
			englishList(parts), plural3(len(ps), "it", "them"), plural3(len(ps), "it", "them"))}, true
}

func plural3(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// kindWord is a kind as a sentence names it.
func kindWord(kind string) string {
	switch kind {
	case "KPI":
		return "indicator"
	case "Operation":
		return "service"
	case "FundingSource":
		return "budget"
	}
	var b strings.Builder
	for i, r := range kind {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return strings.ToLower(b.String())
}

// agentOrderProblems holds an agent's draft to the order of work: no
// placeholder, nothing named that is neither in the record nor in its
// change set, and nothing named that comes later in the order. Each
// problem says what to define first.
func (e *Engine) agentOrderProblems(ctx context.Context, set map[string]bool, kind, id string, doc map[string]any) []Problem {
	var out []Problem
	if len(pendingOf(doc)) > 0 {
		out = append(out, Problem{Path: "/metadata/pending", Message: "an agent never leaves a placeholder: define what this names first, with guide for its kind, in this change set, then name it"})
	}
	l := &lookup{ctx: ctx, store: e.manifests, codec: e.codec}
	for _, f := range extractRefs(doc, e.refRules[kind]) {
		if set[f.kind+"/"+f.id] || l.exists(f.kind, f.id) {
			continue
		}
		out = append(out, Problem{Path: f.path, Message: fmt.Sprintf(
			"names the %s %q, which does not exist yet: define it first (guide for %s, then save_draft in this change set), then name it here. Cartograph is written in order: a %s names only what already exists",
			kindWord(f.kind), f.id, f.kind, kindWord(kind))})
	}
	out = append(out, e.orderProblems(kind, id, doc, l)...)
	return out
}

// stepOfField is the flow step that asks for a field, by JSON pointer, a
// list item's index read as the flow's "-": where a person goes to make
// the reference a placeholder stands in for.
func (e *Engine) stepOfField(kind, path string) string {
	raw, found, err := e.FlowJSON(kind)
	if err != nil || !found {
		return ""
	}
	var flow struct {
		Spec struct {
			Steps []struct {
				Key    string `json:"key"`
				Fields []struct {
					Path string `json:"path"`
				} `json:"fields"`
			} `json:"steps"`
		} `json:"spec"`
	}
	if json.Unmarshal(raw, &flow) != nil {
		return ""
	}
	generic := regexp.MustCompile(`/\d+(/|$)`).ReplaceAllString(path, "/-$1")
	for _, st := range flow.Spec.Steps {
		for _, f := range st.Fields {
			if f.Path == generic || f.Path == path || strings.HasPrefix(generic, f.Path+"/") {
				return st.Key
			}
		}
	}
	return ""
}
