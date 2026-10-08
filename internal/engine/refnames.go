package engine

import (
	"context"
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
	return out, r.created, nil
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
	if value == "" || !registerKinds[kind] {
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
	id := strings.ToLower(kind) + "-" + shortID()
	doc := map[string]any{"apiVersion": "cartograph/v1", "kind": kind, "metadata": map[string]any{"id": id, "name": value}, "spec": map[string]any{}}
	text, err := r.e.codec.Encode(doc)
	if err == nil {
		err = r.e.SaveInChangeSet(r.ctx, r.set, kind, id, text)
	}
	if err != nil {
		r.err = err
		return value
	}
	r.names[kind][strings.ToLower(value)], r.ids[kind][id] = id, true
	r.created = append(r.created, kind+"/"+id)
	return id
}
