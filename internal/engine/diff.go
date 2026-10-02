package engine

import (
	"fmt"
	"reflect"
)

// diffValues recursively compares two parsed documents (maps, slices and
// scalars, as produced by yaml.Unmarshal into map[string]any) and returns
// every difference as a Change rooted at path (an empty string for the
// document root). Arrays of objects that all carry an "id" field are
// diffed by that id instead of by index, so reordering or inserting an
// entry does not spuriously touch every entry after it; every other array
// is diffed by index.
func diffValues(path string, a, b any) []Change {
	if reflect.DeepEqual(a, b) {
		return nil
	}
	if a == nil {
		return []Change{{Path: path, Op: "add", To: b}}
	}
	if b == nil {
		return []Change{{Path: path, Op: "remove", From: a}}
	}

	am, aIsMap := a.(map[string]any)
	bm, bIsMap := b.(map[string]any)
	if aIsMap && bIsMap {
		return diffMaps(path, am, bm)
	}

	aa, aIsArr := a.([]any)
	bb, bIsArr := b.([]any)
	if aIsArr && bIsArr {
		return diffArrays(path, aa, bb)
	}

	return []Change{{Path: path, Op: "replace", From: a, To: b}}
}

func diffMaps(path string, a, b map[string]any) []Change {
	var changes []Change
	seen := map[string]bool{}
	for k, av := range a {
		seen[k] = true
		p := path + "/" + escapePointerToken(k)
		if bv, ok := b[k]; ok {
			changes = append(changes, diffValues(p, av, bv)...)
		} else {
			changes = append(changes, Change{Path: p, Op: "remove", From: av})
		}
	}
	for k, bv := range b {
		if seen[k] {
			continue
		}
		p := path + "/" + escapePointerToken(k)
		changes = append(changes, Change{Path: p, Op: "add", To: bv})
	}
	return changes
}

func diffArrays(path string, a, b []any) []Change {
	if arrayIsIDKeyed(a) || arrayIsIDKeyed(b) {
		return diffArrayByID(path, a, b)
	}
	return diffArrayByIndex(path, a, b)
}

func arrayIsIDKeyed(a []any) bool {
	if len(a) == 0 {
		return false
	}
	for _, e := range a {
		m, ok := e.(map[string]any)
		if !ok {
			return false
		}
		if _, ok := m["id"]; !ok {
			return false
		}
	}
	return true
}

func diffArrayByID(path string, a, b []any) []Change {
	am, aOrder := indexByID(a)
	bm, _ := indexByID(b)

	var changes []Change
	seen := map[string]bool{}
	for _, id := range aOrder {
		seen[id] = true
		p := path + "/" + escapePointerToken(id)
		if bv, ok := bm[id]; ok {
			changes = append(changes, diffValues(p, am[id], bv)...)
		} else {
			changes = append(changes, Change{Path: p, Op: "remove", From: am[id]})
		}
	}
	_, bOrder := indexByID(b)
	for _, id := range bOrder {
		if seen[id] {
			continue
		}
		p := path + "/" + escapePointerToken(id)
		changes = append(changes, Change{Path: p, Op: "add", To: bm[id]})
	}
	return changes
}

func indexByID(a []any) (map[string]any, []string) {
	m := map[string]any{}
	order := make([]string, 0, len(a))
	for _, e := range a {
		em, ok := e.(map[string]any)
		if !ok {
			continue
		}
		id := fmt.Sprint(em["id"])
		m[id] = e
		order = append(order, id)
	}
	return m, order
}

func diffArrayByIndex(path string, a, b []any) []Change {
	var changes []Change
	n := len(a)
	if len(b) > n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		p := fmt.Sprintf("%s/%d", path, i)
		switch {
		case i < len(a) && i < len(b):
			changes = append(changes, diffValues(p, a[i], b[i])...)
		case i < len(a):
			changes = append(changes, Change{Path: p, Op: "remove", From: a[i]})
		default:
			changes = append(changes, Change{Path: p, Op: "add", To: b[i]})
		}
	}
	return changes
}

// ChangedFields lists, by JSON pointer, the fields that differ between two
// texts of a manifest; before may be empty for a new one. At most limit.
func (e *Engine) ChangedFields(before, after []byte, limit int) []string {
	var a, b map[string]any
	if len(before) > 0 {
		_ = e.codec.DecodeInto(before, &a)
	}
	if e.codec.DecodeInto(after, &b) != nil {
		return nil
	}
	if a == nil {
		a = map[string]any{}
	}
	var out []string
	for _, c := range diffValues("", a, b) {
		if len(out) == limit {
			break
		}
		out = append(out, c.Path)
	}
	return out
}
