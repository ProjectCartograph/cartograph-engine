package postgres

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// A historical version is kept as the JSON Patch (RFC 6902) that turns
// the version before it into it (docs/adr/0012). These are the three
// operations this adapter writes, and the only ones it applies.
type patchOp struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value any    `json:"value"` // always written: null is a value; a remove ignores it
}

// decodeJSON reads a document keeping every number as written, so a
// patch round-trips without a float in between.
func decodeJSON(b []byte) (any, error) {
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

// diffJSON returns the patch that turns a into b. Arrays are compared
// position by position, which is what a growing series needs: a reading
// appended is one add, a reading restated is one replace.
func diffJSON(a, b []byte) ([]byte, error) {
	av, err := decodeJSON(a)
	if err != nil {
		return nil, err
	}
	bv, err := decodeJSON(b)
	if err != nil {
		return nil, err
	}
	ops := []patchOp{}
	diffValue("", av, bv, &ops)
	return json.Marshal(ops)
}

func diffValue(path string, a, b any, ops *[]patchOp) {
	if reflect.DeepEqual(a, b) {
		return
	}
	am, aMap := a.(map[string]any)
	bm, bMap := b.(map[string]any)
	if aMap && bMap {
		keys := make([]string, 0, len(am)+len(bm))
		for k := range am {
			keys = append(keys, k)
		}
		for k := range bm {
			if _, ok := am[k]; !ok {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		for _, k := range keys {
			p := path + "/" + escapeToken(k)
			av, inA := am[k]
			bv, inB := bm[k]
			switch {
			case !inB:
				*ops = append(*ops, patchOp{Op: "remove", Path: p})
			case !inA:
				*ops = append(*ops, patchOp{Op: "add", Path: p, Value: bv})
			default:
				diffValue(p, av, bv, ops)
			}
		}
		return
	}
	aa, aArr := a.([]any)
	ba, bArr := b.([]any)
	if aArr && bArr {
		n := min(len(aa), len(ba))
		for i := 0; i < n; i++ {
			diffValue(path+"/"+strconv.Itoa(i), aa[i], ba[i], ops)
		}
		for i := n; i < len(ba); i++ {
			*ops = append(*ops, patchOp{Op: "add", Path: path + "/" + strconv.Itoa(i), Value: ba[i]})
		}
		// From the end, so each index is still where it was.
		for i := len(aa) - 1; i >= n; i-- {
			*ops = append(*ops, patchOp{Op: "remove", Path: path + "/" + strconv.Itoa(i)})
		}
		return
	}
	*ops = append(*ops, patchOp{Op: "replace", Path: path, Value: b})
}

// applyPatch applies a patch diffJSON wrote to doc.
func applyPatch(doc, patch []byte) ([]byte, error) {
	root, err := decodeJSON(doc)
	if err != nil {
		return nil, fmt.Errorf("apply patch: document: %w", err)
	}
	var ops []struct {
		Op    string          `json:"op"`
		Path  string          `json:"path"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(patch, &ops); err != nil {
		return nil, fmt.Errorf("apply patch: %w", err)
	}
	for _, op := range ops {
		var value any
		if op.Op != "remove" {
			if value, err = decodeJSON(op.Value); err != nil {
				return nil, fmt.Errorf("apply patch: %s %s: %w", op.Op, op.Path, err)
			}
		}
		if root, err = applyOp(root, op.Op, op.Path, value); err != nil {
			return nil, err
		}
	}
	return json.Marshal(root)
}

func applyOp(root any, op, path string, value any) (any, error) {
	if path == "" {
		if op != "replace" {
			return nil, fmt.Errorf("apply patch: %s at the root", op)
		}
		return value, nil
	}
	tokens := strings.Split(path[1:], "/")
	for i, t := range tokens {
		tokens[i] = unescapeToken(t)
	}
	parent, err := walk(root, tokens[:len(tokens)-1])
	if err != nil {
		return nil, fmt.Errorf("apply patch: %s %s: %w", op, path, err)
	}
	last := tokens[len(tokens)-1]
	switch p := parent.(type) {
	case map[string]any:
		if op == "remove" {
			delete(p, last)
		} else {
			p[last] = value
		}
		return root, nil
	case []any:
		i, err := strconv.Atoi(last)
		if err != nil || i < 0 || i > len(p) || (op != "add" && i == len(p)) {
			return nil, fmt.Errorf("apply patch: %s %s: no index %s", op, path, last)
		}
		var next []any
		switch op {
		case "add":
			next = append(append(append([]any{}, p[:i]...), value), p[i:]...)
		case "remove":
			next = append(append([]any{}, p[:i]...), p[i+1:]...)
		default:
			p[i] = value
			return root, nil
		}
		// A slice that changed length is a new value: put it back where
		// it hangs.
		parentPath := ""
		if len(tokens) > 1 {
			parentPath = "/" + joinTokens(tokens[:len(tokens)-1])
		}
		return applyOp(root, "replace", parentPath, next)
	}
	return nil, fmt.Errorf("apply patch: %s %s: not a container", op, path)
}

func walk(v any, tokens []string) (any, error) {
	for _, t := range tokens {
		switch c := v.(type) {
		case map[string]any:
			next, ok := c[t]
			if !ok {
				return nil, fmt.Errorf("no member %q", t)
			}
			v = next
		case []any:
			i, err := strconv.Atoi(t)
			if err != nil || i < 0 || i >= len(c) {
				return nil, fmt.Errorf("no index %s", t)
			}
			v = c[i]
		default:
			return nil, fmt.Errorf("%q is not a container", t)
		}
	}
	return v, nil
}

func joinTokens(tokens []string) string {
	out := make([]string, len(tokens))
	for i, t := range tokens {
		out[i] = escapeToken(t)
	}
	return strings.Join(out, "/")
}

func escapeToken(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
}
func unescapeToken(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~1", "/"), "~0", "~")
}
