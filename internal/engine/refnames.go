package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/kinds"
)

// NamedRefs resolves, in fields about to be set on a kind's draft, every
// name given where a reference to a register goes (docs/adr/0027): the
// record of that kind with that name, in the record or the change set,
// else a new draft of it in the change set, named so. A small agent porting
// a document writes "Education Health Services Unit" where a team goes and
// the team is there, rather than being refused for an id it never had.
// It answers the fields as they are to be set, and the records it drafted.
func (e *Engine) NamedRefs(ctx context.Context, set, kind string, fields map[string]any) (map[string]any, []string, error) {
	spec, ok := kinds.ByName(kind)
	if !ok {
		return fields, nil, nil
	}
	r := &refResolver{e: e, ctx: ctx, set: set, names: map[string]map[string]string{}, ids: map[string]map[string]bool{}}
	out := make(map[string]any, len(fields))
	for p, v := range fields {
		node, file, ok := e.nodeAt(spec.SchemaFile, p)
		if !ok {
			out[p] = v
			continue
		}
		out[p] = r.resolve(file, node, v)
		if r.err != nil {
			return nil, nil, r.err
		}
	}
	created := r.created
	for _, c := range r.clipped {
		created = append(created, "cut: "+c)
	}
	for _, c := range r.refused {
		created = append(created, "refused: "+c)
	}
	return out, created, nil
}

// nodeAt is the schema node at a JSON pointer in a kind's manifest.
func (e *Engine) nodeAt(file, pointer string) (map[string]any, string, bool) {
	node, file, ok := e.schemaNode(file, e.schemas.raw[file])
	segs := strings.Split(strings.Trim(pointer, "/"), "/")
	for i, seg := range segs {
		if !ok || seg == "" {
			continue
		}
		if items, isList := node["items"].(map[string]any); isList && (seg == "-" || isIndexSeg(seg)) {
			node, file, ok = e.schemaNode(file, items)
			continue
		}
		props, _ := node["properties"].(map[string]any)
		next, found := props[seg].(map[string]any)
		if !found {
			return nil, file, false
		}
		// The last node is answered as written, its annotations beside
		// its $ref kept; resolve reads through it.
		raw := next
		node, file, ok = e.schemaNode(file, next)
		if ok && i == len(segs)-1 {
			return raw, file, true
		}
	}
	return node, file, ok
}

type refResolver struct {
	e       *Engine
	ctx     context.Context
	set     string
	names   map[string]map[string]string // kind: lower name: id
	ids     map[string]map[string]bool   // kind: id
	created []string
	clipped []string
	refused []string
	err     error
}

func (r *refResolver) resolve(file string, node map[string]any, v any) any {
	// The annotation sits beside the $ref it qualifies: read it first.
	ref, _ := node["x-cartograph-ref"].(string)
	node, file, ok := r.e.schemaNode(file, node)
	if !ok || r.err != nil {
		return v
	}
	if ref == "" {
		ref, _ = node["x-cartograph-ref"].(string)
	}
	if ref != "" {
		if s, isText := v.(string); isText {
			return r.id(ref, s)
		}
	}
	props, _ := node["properties"].(map[string]any)
	// Formats, never shapes: a day where a month goes is that month, a
	// date where a timing goes is a timing on that date, and a line
	// longer than its field is cut at a word, said so in the answer.
	if s, isText := v.(string); isText {
		if _, timing := props["form"]; timing && props["date"] != nil {
			if m, ok := readMonth(s); ok {
				return map[string]any{"form": "date", "date": m}
			}
		}
		if pat, _ := node["pattern"].(string); strings.Contains(pat, "[0-9]{4}-(0[1-9]|1[0-2])") {
			if m, ok := readMonth(s); ok {
				return m
			}
		}
		if max, ok := number(node["maxLength"]); ok && len([]rune(s)) > int(max) {
			cut := clip(s, int(max))
			r.clipped = append(r.clipped, fmt.Sprintf("%q cut to %q", s, cut))
			return cut
		}
	}
	// A reference written as an object ({kind, id}, {local, id} or
	// {external}) given as a name is a role: a Resource.
	if _, hasID := props["id"]; hasID && props["external"] != nil {
		if s, isText := v.(string); isText && strings.TrimSpace(s) != "" {
			return map[string]any{"kind": "Resource", "id": r.id("Resource", s)}
		}
	}
	switch val := v.(type) {
	case []any:
		items, _ := node["items"].(map[string]any)
		out := make([]any, len(val))
		for i, it := range val {
			out[i] = r.resolve(file, items, it)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, it := range val {
			if p, ok := props[k].(map[string]any); ok {
				out[k] = r.resolve(file, p, it)
			} else {
				out[k] = it
			}
		}
		return out
	}
	return v
}

// id is the id for a value given where a reference to kind goes: the
// value itself when it is an id, the record of that name, or a new draft.
func (r *refResolver) id(kind, value string) string {
	value = strings.TrimSpace(value)
	if value == "" || !(registerKinds[kind] || kind == "KPI") {
		return value
	}
	if r.names[kind] == nil {
		docs, err := r.e.allInPlay(r.ctx, kind)
		if err != nil {
			r.err = err
			return value
		}
		r.names[kind], r.ids[kind] = map[string]string{}, map[string]bool{}
		for _, d := range docs {
			r.names[kind][strings.ToLower(nameOf(d.doc, d.id))] = d.id
			r.ids[kind][d.id] = true
		}
	}
	if r.ids[kind][value] {
		return value
	}
	if id, ok := r.names[kind][strings.ToLower(value)]; ok {
		return id
	}
	// A near enough name is the same record: "Members (all depots)"
	// and "Members of all depots" are one group, not two.
	for name, id := range r.names[kind] {
		if nearName(name, value) {
			return id
		}
	}
	id := strings.ToLower(kind) + "-" + shortID()
	doc := map[string]any{"apiVersion": "cartograph/v1", "kind": kind, "metadata": map[string]any{"id": id, "name": value}, "spec": registerDefaults(kind, value)}
	text, err := r.e.codec.Encode(doc)
	if err == nil {
		err = r.e.SaveInChangeSet(r.ctx, r.set, kind, id, text)
	}
	var invalid *ValidationError
	if errors.As(err, &invalid) {
		// A name the register refuses (a person's, where a role goes)
		// costs only the field it was given in: the field is refused when
		// it is set, and the reason said here.
		r.refused = append(r.refused, fmt.Sprintf("%q is not a %s's name: %s", value, kind, strictRule(r.e, kind)))
		return value
	}
	if err != nil {
		r.err = err
		return value
	}
	r.names[kind][strings.ToLower(value)], r.ids[kind][id] = id, true
	r.created = append(r.created, kind+"/"+id)
	return id
}

// registerDefaults are what a register drafted from a name needs to be
// valid, read from the name where it can be: a role's category (a body
// or a unit by its words, else a role), for the person to correct.
func registerDefaults(kind, name string) map[string]any {
	if kind == "KPI" {
		// An indicator named in a register: its definition is its name, in
		// percent where the name says so, else a count, rising; the checks
		// ask for its baseline, target, sources and cycle.
		unit := "count"
		if strings.Contains(name, "%") || strings.Contains(strings.ToLower(name), "percent") || strings.Contains(strings.ToLower(name), "share") {
			unit = "percent"
		}
		return map[string]any{"definition": clip(name, 300), "unit": unit, "direction": "increase"}
	}
	if kind != "Resource" {
		return map[string]any{}
	}
	n := " " + strings.ToLower(name) + " "
	category := "personRole"
	for _, w := range []string{" committee ", " board ", " council ", " task force ", " steering "} {
		if strings.Contains(n, w) {
			return map[string]any{"category": "governanceBody"}
		}
	}
	for _, w := range []string{" unit ", " division ", " department ", " office ", " agency ", " authority ", " services ", " limited ", " inspectorate ", " directorate ", " team ", " branch ", " section "} {
		if strings.Contains(n, w) {
			category = "orgUnit"
		}
	}
	return map[string]any{"category": category}
}

// nearName reports whether two names say the same thing: most of their
// words shared, once case, punctuation and short words are set aside.
func nearName(a, b string) bool {
	wa, wb := nameWords(a), nameWords(b)
	if len(wa) == 0 || len(wb) == 0 {
		return false
	}
	shared := 0
	for w := range wa {
		if wb[w] {
			shared++
		}
	}
	small := len(wa)
	if len(wb) < small {
		small = len(wb)
	}
	return float64(shared)/float64(small) >= 0.75 && shared >= 2
}

func nameWords(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	}) {
		if len(w) > 2 && w != "and" && w != "the" && w != "all" {
			out[strings.TrimSuffix(w, "s")] = true
		}
	}
	return out
}

// readMonth reads a date as its month, YYYY-MM: from YYYY-MM, YYYY-MM-DD,
// or DD/MM/YYYY.
func readMonth(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if len(s) >= 7 && s[4] == '-' && isDigits(s[:4]) && isDigits(s[5:7]) && s[5:7] >= "01" && s[5:7] <= "12" {
		return s[:7], len(s) == 7 || len(s) == 10 && s[7] == '-'
	}
	if len(s) == 10 && s[2] == '/' && s[5] == '/' && isDigits(s[:2]) && isDigits(s[3:5]) && isDigits(s[6:]) && s[3:5] >= "01" && s[3:5] <= "12" {
		return s[6:] + "-" + s[3:5], true
	}
	return "", false
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// strictRule is what a kind's strict profile says it refuses, for an
// agent told its value was refused.
func strictRule(e *Engine, kind string) string {
	spec, ok := kinds.ByName(kind)
	if !ok {
		return "it breaks the kind's rules"
	}
	if d, _ := e.schemas.raw["strict."+spec.SchemaFile]["description"].(string); d != "" {
		return d
	}
	return "it breaks the kind's rules"
}
