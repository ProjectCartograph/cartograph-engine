package activity

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/contract"
)

// The shortest path of a task is the fewest presses and answers an
// interface that follows the contract needs to finish it. It is a fact
// of the build, worked out from the flows and the schemas, so a change
// that makes a screen harder to use shows as a longer path and fails the
// budget below until someone says why.
//
// To define a record: one press to open its flow, one press of Next for
// each step after the first, one answer for each field the schema
// requires (a list that must hold an item costs a press to add it and an
// answer for each field its item requires, its id aside, which is
// generated), and one press to save it as a version.

//go:embed paths.json
var budgetJSON []byte

// Budget is the committed budget of each task's shortest path, by
// "type/Kind": what Paths may not exceed without the budget changing in
// the same change, and what cartograph ux reads extra interactions
// against.
func Budget() (map[string]int, error) {
	out := map[string]int{}
	if err := json.Unmarshal(budgetJSON, &out); err != nil {
		return nil, fmt.Errorf("paths.json: %w", err)
	}
	return out, nil
}

// Paths works out every task's shortest path from the contract, by
// "type/Kind".
func Paths(flows map[string]Flow) (map[string]int, error) {
	schemas, err := readSchemas()
	if err != nil {
		return nil, err
	}
	out := map[string]int{}
	for kind, f := range flows {
		root, ok := schemas[strings.ToLower(kind)+".schema.json"]
		if !ok {
			return nil, fmt.Errorf("no schema for %s", kind)
		}
		n := 1 + max(len(f.Steps)-1, 0) + 1
		for _, fl := range f.Fields {
			n += cost(schemas, root, strings.ToLower(kind)+".schema.json", fl)
		}
		out[Define+"/"+kind] = n
	}
	return out, nil
}

// cost is how many presses and answers a field needs before its record
// can be saved as a version.
func cost(schemas map[string]map[string]any, root map[string]any, file string, fl FlowField) int {
	segs := strings.Split(strings.TrimPrefix(fl.Path, "/"), "/")
	parent, pfile := root, file
	for _, s := range segs[:len(segs)-1] {
		props, _ := parent["properties"].(map[string]any)
		next, _ := props[s].(map[string]any)
		if next == nil {
			return 0
		}
		parent, pfile = resolve(schemas, next, pfile)
	}
	name := segs[len(segs)-1]
	if !contains(parent["required"], name) {
		return 0
	}
	props, _ := parent["properties"].(map[string]any)
	node, _ := props[name].(map[string]any)
	if node == nil {
		return 1
	}
	node, nfile := resolve(schemas, node, pfile)
	if node["type"] != "array" {
		return 1
	}
	if minItems, _ := node["minItems"].(float64); minItems < 1 {
		return 0
	}
	items, _ := node["items"].(map[string]any)
	n := 1
	if items != nil {
		item, _ := resolve(schemas, items, nfile)
		reqs, _ := item["required"].([]any)
		for _, r := range reqs {
			if r != "id" {
				n++
			}
		}
	}
	return n
}

// resolve follows a node's $ref, within its file or to another, until it
// reaches a node with none.
func resolve(schemas map[string]map[string]any, node map[string]any, file string) (map[string]any, string) {
	for range 10 {
		ref, ok := node["$ref"].(string)
		if !ok {
			return node, file
		}
		target, pointer, _ := strings.Cut(ref, "#")
		if target != "" {
			file = target
		}
		next := any(schemas[file])
		for _, s := range strings.Split(strings.TrimPrefix(pointer, "/"), "/") {
			if s == "" {
				continue
			}
			m, _ := next.(map[string]any)
			next = m[s]
		}
		n, _ := next.(map[string]any)
		if n == nil {
			return map[string]any{}, file
		}
		node = n
	}
	return node, file
}

func contains(list any, s string) bool {
	l, _ := list.([]any)
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func readSchemas() (map[string]map[string]any, error) {
	out := map[string]map[string]any{}
	entries, err := fs.ReadDir(contract.Schemas, "schemas")
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		b, err := fs.ReadFile(contract.Schemas, "schemas/"+e.Name())
		if err != nil {
			return nil, err
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		out[e.Name()] = m
	}
	return out, nil
}

// PathsJSON is the budget file's text for paths: sorted, one task a line,
// for a diff that reads at a glance.
func PathsJSON(paths map[string]int) []byte {
	keys := make([]string, 0, len(paths))
	for k := range paths {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("{\n")
	for i, k := range keys {
		sep := ","
		if i == len(keys)-1 {
			sep = ""
		}
		fmt.Fprintf(&b, "  %q: %d%s\n", k, paths[k], sep)
	}
	b.WriteString("}\n")
	return []byte(b.String())
}
