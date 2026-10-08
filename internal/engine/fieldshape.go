package engine

import (
	"sort"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/kinds"
)

// FieldShape is what goes at a field, read from the kind's schema: a minimal
// example with every required part, and the names of the optional ones,
// so an agent can write a whole list (every milestone, every risk) in
// one call without reading the guide for each.
type FieldShape struct {
	Example  any      `json:"example"`
	Optional []string `json:"optional,omitempty"`
	// OneOf are the values a fixed choice takes.
	OneOf []string `json:"oneOf,omitempty"`
}

// FieldShapeAt is the shape of the field at a JSON pointer in a kind's
// manifest; list items are "-" or an index. False when the schema has no
// such field.
func (e *Engine) FieldShapeAt(kind, pointer string) (FieldShape, bool) {
	spec, ok := kinds.ByName(kind)
	if !ok {
		return FieldShape{}, false
	}
	node, file, ok := e.schemaNode(spec.SchemaFile, e.schemas.raw[spec.SchemaFile])
	if !ok {
		return FieldShape{}, false
	}
	for _, seg := range strings.Split(strings.Trim(pointer, "/"), "/") {
		if seg == "" {
			continue
		}
		if node, file, ok = e.schemaNode(file, node); !ok {
			return FieldShape{}, false
		}
		if items, isList := node["items"].(map[string]any); isList && (seg == "-" || isIndexSeg(seg)) {
			node = items
			continue
		}
		props, _ := node["properties"].(map[string]any)
		next, found := props[seg].(map[string]any)
		if !found {
			return FieldShape{}, false
		}
		node = next
	}
	node, file, _ = e.schemaNode(file, node)
	var optional []string
	target := node
	if items, isList := node["items"].(map[string]any); isList {
		target, _, _ = e.schemaNode(file, items)
	}
	req := map[string]bool{}
	for _, r := range stringsOf(target["required"]) {
		req[r] = true
	}
	if props, ok := target["properties"].(map[string]any); ok {
		for k := range props {
			if !req[k] {
				optional = append(optional, k)
			}
		}
	}
	sort.Strings(optional)
	return FieldShape{Example: e.example(file, node, 0), Optional: optional, OneOf: stringsOf(node["enum"])}, true
}

// schemaNode resolves a node's $ref (in its file or another) and its
// first allOf, so a field's own schema is read.
func (e *Engine) schemaNode(file string, node map[string]any) (map[string]any, string, bool) {
	for i := 0; i < 8 && node != nil; i++ {
		if ref, ok := node["$ref"].(string); ok {
			target, frag, _ := strings.Cut(ref, "#")
			if target != "" {
				file = target
			}
			doc := e.schemas.raw[file]
			if doc == nil {
				return nil, file, false
			}
			node = doc
			for _, p := range strings.Split(strings.Trim(frag, "/"), "/") {
				if p == "" {
					continue
				}
				next, _ := node[p].(map[string]any)
				node = next
			}
			continue
		}
		if all, ok := node["allOf"].([]any); ok && len(all) > 0 && node["properties"] == nil {
			first, _ := all[0].(map[string]any)
			node = first
			continue
		}
		return node, file, true
	}
	return node, file, node != nil
}

// example is a minimal value of a schema node: its required parts only.
func (e *Engine) example(file string, node map[string]any, depth int) any {
	node, file, ok := e.schemaNode(file, node)
	if !ok || depth > 6 {
		return nil
	}
	if enum, ok := node["enum"].([]any); ok && len(enum) > 0 {
		return enum[0]
	}
	if enum, ok := node["x-cartograph-enum"].([]any); ok && len(enum) > 0 {
		return enum[0]
	}
	if one, ok := node["oneOf"].([]any); ok && len(one) > 0 {
		first, _ := one[0].(map[string]any)
		return e.example(file, first, depth+1)
	}
	if alt, ok := node["anyOf"].([]any); ok && len(alt) > 0 {
		first, _ := alt[0].(map[string]any)
		return e.example(file, first, depth+1)
	}
	switch t, _ := node["type"].(string); t {
	case "object":
		out := map[string]any{}
		props, _ := node["properties"].(map[string]any)
		for _, r := range stringsOf(node["required"]) {
			if p, ok := props[r].(map[string]any); ok {
				out[r] = e.example(file, p, depth+1)
			}
		}
		return out
	case "array":
		items, _ := node["items"].(map[string]any)
		return []any{e.example(file, items, depth+1)}
	case "number", "integer":
		return 0
	case "boolean":
		return false
	case "string":
		if node["x-cartograph-ref"] != nil {
			return "<id of a " + strings.TrimSpace(node["x-cartograph-ref"].(string)) + ">"
		}
		if p, ok := node["pattern"].(string); ok && strings.Contains(p, "[0-9]{4}") {
			return "2026-09"
		}
		return "…"
	}
	if props, ok := node["properties"].(map[string]any); ok && len(props) > 0 {
		return e.example(file, map[string]any{"type": "object", "properties": props, "required": node["required"]}, depth+1)
	}
	return "…"
}

func isIndexSeg(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// ListFields are a kind's spec fields that hold lists, as JSON pointers,
// in the schema's order of names: what a document may give many of.
func (e *Engine) ListFields(kind string) []string {
	spec, ok := kinds.ByName(kind)
	if !ok {
		return nil
	}
	root, file, ok := e.schemaNode(spec.SchemaFile, e.schemas.raw[spec.SchemaFile])
	if !ok {
		return nil
	}
	props, _ := root["properties"].(map[string]any)
	sp, _ := props["spec"].(map[string]any)
	sp, file, _ = e.schemaNode(file, sp)
	fields, _ := sp["properties"].(map[string]any)
	var out []string
	for name, f := range fields {
		node, _ := f.(map[string]any)
		if node, _, ok := e.schemaNode(file, node); ok && node["type"] == "array" && node["deprecated"] != true {
			out = append(out, "/spec/"+name)
		}
	}
	sort.Strings(out)
	return out
}
