package engine

import (
	"fmt"
	"strings"
)

// The engine finds every cross-manifest reference generically, by reading
// the x-cartograph-ref vendor extension straight out of each kind's raw JSON
// Schema document (not through the jsonschema validator, which has no
// concept of this extension) and then walking a parsed manifest instance
// along the matching path. The same rule list and walk serve two jobs:
// checking that a reference resolves to something that exists (validation)
// and recording every outgoing reference for the reference index (commit).

type refPathStep struct {
	prop     string
	wildcard bool
}

// refRule is one x-cartograph-ref location found in a kind's schema, expressed
// as a path of steps from the manifest root.
type refRule struct {
	path []refPathStep
	kind string // target kind name, or "*" when the kind is a sibling field
}

// foundRef is one concrete reference found in an instance document.
type foundRef struct {
	path string // JSON pointer from the manifest root, e.g. "/spec/team"
	kind string
	id   string
}

// collectRefRules walks a kind's schema document (following $ref within the
// raw schema files) and returns every x-cartograph-ref location.
func collectRefRules(raw map[string]map[string]any, schemaFile string) []refRule {
	var rules []refRule
	// currentFile tracks which schema document node belongs to, since a
	// $ref can jump into another file (project.schema.json's KeyResult
	// items into common.schema.json, say); any further local ("#/...")
	// $ref inside the resolved node must resolve against that file, not
	// the file the walk started from.
	var walk func(node map[string]any, currentFile string, path []refPathStep, visiting []string)
	walk = func(node map[string]any, currentFile string, path []refPathStep, visiting []string) {
		if node == nil {
			return
		}
		if xref, ok := node["x-cartograph-ref"].(string); ok {
			cp := make([]refPathStep, len(path))
			copy(cp, path)
			rules = append(rules, refRule{path: cp, kind: xref})
		}
		if refVal, ok := node["$ref"].(string); ok {
			file, resolved := resolveSchemaRef(raw, currentFile, refVal)
			key := file + refVal
			for _, v := range visiting {
				if v == key {
					return
				}
			}
			if resolved != nil {
				walk(resolved, file, path, append(visiting, key))
			}
			return
		}
		if props, ok := node["properties"].(map[string]any); ok {
			for name, pnode := range props {
				if pm, ok := pnode.(map[string]any); ok {
					walk(pm, currentFile, append(path, refPathStep{prop: name}), visiting)
				}
			}
		}
		if items, ok := node["items"].(map[string]any); ok {
			walk(items, currentFile, append(path, refPathStep{wildcard: true}), visiting)
		}
	}
	walk(raw[schemaFile], schemaFile, nil, nil)
	return rules
}

// resolveSchemaRef resolves a $ref value found while reading fromFile,
// returning the file it resolves into and the schema node at that pointer.
func resolveSchemaRef(raw map[string]map[string]any, fromFile, ref string) (string, map[string]any) {
	file := fromFile
	pointer := ref
	if i := strings.IndexByte(ref, '#'); i >= 0 {
		if ref[:i] != "" {
			file = ref[:i]
		}
		pointer = ref[i+1:]
	} else {
		pointer = ""
	}
	doc, ok := raw[file]
	if !ok {
		return file, nil
	}
	node := navigatePointer(doc, pointer)
	return file, node
}

func navigatePointer(doc map[string]any, pointer string) map[string]any {
	pointer = strings.TrimPrefix(pointer, "/")
	if pointer == "" {
		return doc
	}
	var cur any = doc
	for _, tok := range strings.Split(pointer, "/") {
		tok = strings.ReplaceAll(tok, "~1", "/")
		tok = strings.ReplaceAll(tok, "~0", "~")
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur, ok = m[tok]
		if !ok {
			return nil
		}
	}
	m, _ := cur.(map[string]any)
	return m
}

// extractRefs walks a parsed manifest document along every rule for its
// kind and returns every concrete reference found. Project.spec.operation
// set to the literal "new" is dropped here, once, for both callers
// (validation and indexing), since it is the one documented non-reference
// value for an x-cartograph-ref field.
func extractRefs(doc map[string]any, rules []refRule) []foundRef {
	var out []foundRef
	for _, r := range rules {
		walkInstance(doc, r.path, "", r.kind, &out)
	}
	filtered := out[:0]
	for _, f := range out {
		if f.kind == "Operation" && f.id == "new" {
			continue
		}
		filtered = append(filtered, f)
	}
	return filtered
}

func walkInstance(node any, steps []refPathStep, pathSoFar, refKind string, out *[]foundRef) {
	if len(steps) == 0 {
		return
	}
	step := steps[0]
	last := len(steps) == 1

	if step.wildcard {
		arr, ok := node.([]any)
		if !ok {
			return
		}
		for i, elem := range arr {
			p := fmt.Sprintf("%s/%d", pathSoFar, i)
			if last {
				if s, ok := elem.(string); ok {
					*out = append(*out, foundRef{path: p, kind: refKind, id: s})
				}
				continue
			}
			walkInstance(elem, steps[1:], p, refKind, out)
		}
		return
	}

	m, ok := node.(map[string]any)
	if !ok {
		return
	}
	val, present := m[step.prop]
	if !present {
		return
	}
	p := pathSoFar + "/" + escapePointerToken(step.prop)
	if last {
		s, ok := val.(string)
		if !ok {
			return
		}
		actualKind := refKind
		if refKind == "*" {
			k, ok := m["kind"].(string)
			if !ok {
				return
			}
			actualKind = k
		}
		*out = append(*out, foundRef{path: p, kind: actualKind, id: s})
		return
	}
	walkInstance(val, steps[1:], p, refKind, out)
}

func escapePointerToken(tok string) string {
	tok = strings.ReplaceAll(tok, "~", "~0")
	tok = strings.ReplaceAll(tok, "/", "~1")
	return tok
}
