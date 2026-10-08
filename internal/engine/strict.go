package engine

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// unfinished are the schema keywords a draft fails only for lacking
// something not written yet: a required field, too few items, an empty
// text. A draft is built up a field at a time, so these are its checks'
// business. Every other failure is a shape the record must never hold.
var unfinished = map[string]bool{"required": true, "minItems": true, "minLength": true, "minProperties": true}

// StructuralProblems are what is built wrong in a manifest, as against
// what is not finished yet (docs/adr/0027): every failure of the kind's
// strict profile (its schema with the shapes the discipline refuses, such
// as a second objective on a project, or a person's name on a role) or of
// its schema that more writing cannot mend. An agent's draft with any is
// refused, so an improper structure is never saved, however it is sent.
func (e *Engine) StructuralProblems(_ context.Context, kind string, text []byte) ([]Problem, error) {
	var doc map[string]any
	if err := e.codec.DecodeInto(text, &doc); err != nil {
		return []Problem{{Message: "invalid yaml: " + err.Error()}}, nil
	}
	if doc == nil {
		return []Problem{{Message: "empty manifest"}}, nil
	}
	rewriteLegacyFields(kind, doc)
	schema, ok := e.schemas.strict[kind]
	if !ok {
		if schema, ok = e.schemas.compiled[kind]; !ok {
			return nil, fmt.Errorf("%w: %s", ErrUnknownKind, kind)
		}
	}
	// No person appears in Cartograph (AGENTS.md): a name led by a
	// person's title is refused in any text, a note as much as a name.
	out := personNames(doc, "")
	err := schema.Validate(any(doc))
	if err == nil {
		return out, nil
	}
	for _, p := range schemaProblems(err) {
		if !unfinished[p.Keyword] {
			out = append(out, p)
		}
	}
	return out, nil
}

// titledName is a person named by their title: Dr. M. Francis, Ms Rao,
// Prof. Ada Mensah. A role or a body is named instead.
var titledName = regexp.MustCompile(`\b(Dr|Mr|Mrs|Ms|Mx|Prof|Professor|Sir|Dame)\.?[ \t]+(?:[A-Z]\.[ \t]*)*[A-Z][a-z]+`)

// personNames are the strings in a document that name a person by their
// title, each a problem on its path, the name itself not repeated.
func personNames(v any, path string) []Problem {
	var out []Problem
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			out = append(out, personNames(t[k], path+"/"+k)...)
		}
	case []any:
		for i, x := range t {
			out = append(out, personNames(x, fmt.Sprintf("%s/%d", path, i))...)
		}
	case string:
		if titledName.MatchString(t) {
			out = append(out, Problem{Path: path, Keyword: "person",
				Message: "names a person by their title: Cartograph names roles and bodies, never people (name the role, such as the lead analyst, or the body)"})
		}
	}
	return out
}

// introducedProblems are the strict profile's problems a version would
// bring in, for anyone who saves it (docs/adr/0029): a second objective,
// a person's name where a role goes. The engine refuses them however the
// version was written, so the rule never rests on a writer, person or
// model, getting it right. A record stored so before this rule keeps what
// it has and may shed it: a list over its limit may shrink, never grow,
// and a problem it already had is not new.
func (e *Engine) introducedProblems(ctx context.Context, kind string, doc map[string]any, text []byte) ([]Problem, error) {
	now, err := e.StructuralProblems(ctx, kind, text)
	if err != nil || len(now) == 0 {
		return nil, err
	}
	var before map[string]any
	had := map[string]bool{}
	if id, ok := docID(doc); ok {
		if v, err := e.Get(ctx, kind, id); err == nil {
			_ = e.codec.DecodeInto(v.YAML, &before)
			old, err := e.StructuralProblems(ctx, kind, v.YAML)
			if err != nil {
				return nil, err
			}
			for _, p := range old {
				had[p.Path+"\x00"+p.Keyword] = true
			}
		}
	}
	var out []Problem
	for _, p := range now {
		if had[p.Path+"\x00"+p.Keyword] {
			// Already over its limit: it may stay or shrink.
			if p.Keyword != "maxItems" || listLen(doc, p.Path) <= listLen(before, p.Path) {
				continue
			}
		}
		out = append(out, p)
	}
	return out, nil
}

// listLen is the length of the list at a JSON pointer in doc, 0 when
// there is none.
func listLen(doc map[string]any, pointer string) int {
	var cur any = doc
	for _, seg := range strings.Split(strings.Trim(pointer, "/"), "/") {
		m, ok := cur.(map[string]any)
		if !ok {
			return 0
		}
		cur = m[seg]
	}
	list, _ := cur.([]any)
	return len(list)
}
