package engine

import (
	"sort"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/crdt"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/kinds"
)

// Shape says how a kind's manifests map onto a CRDT document, read from
// the kind's schema so the engine and every interface agree without
// either deciding it by hand:
//
//   - A list of objects is keyed by x-cartograph-list-key when the schema
//     names one, otherwise by "id" when its items have an id. A keyed
//     item keeps its identity when other items move, so an edit inside it
//     is never lost to a reorder.
//   - A string is a text, merged character by character, when it is
//     free prose: no enum, const, format, pattern, or x-cartograph-ref.
//     Everything else (an id, a reference, a date, a choice) is a scalar,
//     where the last write wins and a concurrent one is kept as a
//     conflict, because merging two ids character by character would
//     produce an id nobody wrote.
func (e *Engine) Shape(kind string) crdt.Shape {
	spec, ok := kinds.ByName(kind)
	if !ok {
		return crdt.Shape{ListKeys: map[string]string{}}
	}
	w := shapeWalker{all: e.schemas.raw, keys: map[string]string{}, seen: map[string]bool{}}
	w.walk(e.schemas.raw[spec.SchemaFile], spec.SchemaFile, "")
	sort.Strings(w.texts)
	return crdt.Shape{ListKeys: w.keys, Texts: w.texts}
}

type shapeWalker struct {
	all   map[string]map[string]any
	keys  map[string]string
	texts []string
	// seen guards against a recursive schema ($ref back to an ancestor)
	// walking forever; the key is the ref and the path it was met at.
	seen map[string]bool
}

// walk visits a schema node at a pointer pattern. file is the schema
// file the node came from, so a same-file "#/..." ref resolves.
func (w *shapeWalker) walk(node map[string]any, file, path string) {
	if node == nil || strings.Count(path, "/") > 24 {
		return
	}
	// A reference marker or a constraint on the node itself makes a
	// string a scalar even when the type comes from a $ref.
	scalarHint := hasAny(node, "enum", "const", "format", "pattern", "x-cartograph-ref")
	node, file = w.resolve(node, file, path)
	if node == nil {
		return
	}
	for _, k := range []string{"allOf", "anyOf", "oneOf"} {
		if subs, ok := node[k].([]any); ok {
			for _, s := range subs {
				if m, ok := s.(map[string]any); ok {
					w.walk(m, file, path)
				}
			}
		}
	}
	if props, ok := node["properties"].(map[string]any); ok {
		for name, p := range props {
			if m, ok := p.(map[string]any); ok {
				w.walk(m, file, path+"/"+escapePointer(name))
			}
		}
	}
	if items, ok := node["items"].(map[string]any); ok {
		resolved, itemFile := w.resolve(items, file, path+"/*")
		if key, _ := node["x-cartograph-list-key"].(string); key != "" {
			w.keys[path] = key
		} else if resolved != nil {
			if props, ok := resolved["properties"].(map[string]any); ok {
				if _, hasID := props["id"]; hasID {
					w.keys[path] = "id"
				}
			}
		}
		w.walk(items, file, path+"/*")
		_ = itemFile
	}
	if typeIs(node, "string") && !scalarHint && !hasAny(node, "enum", "const", "format", "pattern", "x-cartograph-ref", "contentEncoding") {
		if path != "" && !contains(w.texts, path) {
			w.texts = append(w.texts, path)
		}
	}
}

// resolve follows $refs (within a file and across files) and returns the
// target with the file it lives in.
func (w *shapeWalker) resolve(node map[string]any, file, path string) (map[string]any, string) {
	for i := 0; node != nil && i < 8; i++ {
		ref, ok := node["$ref"].(string)
		if !ok {
			return node, file
		}
		target, frag, _ := strings.Cut(ref, "#")
		if target == "" {
			target = file
		}
		guard := target + "#" + frag + "@" + path
		if w.seen[guard] {
			return nil, file
		}
		w.seen[guard] = true
		cur := w.all[target]
		for _, seg := range strings.Split(strings.TrimPrefix(frag, "/"), "/") {
			if seg == "" || cur == nil {
				continue
			}
			cur, _ = cur[seg].(map[string]any)
		}
		node, file = cur, target
	}
	return node, file
}

func typeIs(node map[string]any, want string) bool {
	switch t := node["type"].(type) {
	case string:
		return t == want
	case []any:
		for _, x := range t {
			if x == want {
				return true
			}
		}
	}
	return false
}

func hasAny(node map[string]any, keys ...string) bool {
	for _, k := range keys {
		if _, ok := node[k]; ok {
			return true
		}
	}
	return false
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// escapePointer escapes one JSON pointer segment (RFC 6901).
func escapePointer(seg string) string {
	return strings.NewReplacer("~", "~0", "/", "~1").Replace(seg)
}
