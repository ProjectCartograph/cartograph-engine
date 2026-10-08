package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
)

// WriteFields sets and clears fields on a change set's draft the way an
// agent's write is applied (docs/adr/0027): a name where a register's
// reference goes finds or drafts it, a value is read in its field's
// format, and every field that can be kept is kept (a list item keyed by
// id is given one, as in every edit). With merge, a list the draft
// already holds is merged by id or name, never replaced, so what a port
// wrote into it is never lost to a later write.
//
// It answers the problems refused, field by field, and the records it
// drafted (with lines cut prefixed "cut: " and names refused prefixed
// "refused: "). A write that keeps nothing is a ValidationError.
func (e *Engine) WriteFields(ctx context.Context, set, kind, id string, put map[string]any, unset []string, merge bool) ([]Problem, []string, error) {
	var created []string
	var err error
	if len(put) > 0 {
		if put, created, err = e.NamedRefs(ctx, set, kind, put); err != nil {
			return nil, nil, err
		}
	}
	if len(put) == 0 && len(unset) == 0 {
		return nil, created, nil
	}
	if merge {
		put = e.mergeLists(ctx, set, kind, id, put)
	}
	_, err = e.EditInChangeSet(ctx, set, kind, id, put, unset)
	if err == nil {
		return nil, created, nil
	}
	var invalid *ValidationError
	if !errors.As(err, &invalid) {
		return nil, nil, err
	}
	var refused []Problem
	keys := make([]string, 0, len(put))
	for k := range put {
		keys = append(keys, k)
	}
	// Parents before children, as one edit would set them.
	sort.Slice(keys, func(i, j int) bool {
		return len(keys[i]) < len(keys[j]) || len(keys[i]) == len(keys[j]) && keys[i] < keys[j]
	})
	failed := 0
	for _, k := range keys {
		if _, err := e.EditInChangeSet(ctx, set, kind, id, map[string]any{k: put[k]}, nil); err != nil {
			failed++
			var one *ValidationError
			if errors.As(err, &one) {
				refused = append(refused, one.Problems...)
			} else {
				refused = append(refused, Problem{Path: k, Message: err.Error()})
			}
		}
	}
	if len(unset) > 0 {
		if _, err := e.EditInChangeSet(ctx, set, kind, id, nil, unset); err != nil {
			refused = append(refused, Problem{Message: err.Error()})
		}
	}
	// Nothing kept is a refusal, as it was before any field was tried: every
	// field refused, however many problems each had.
	if failed == len(keys) && len(keys) > 0 && len(unset) == 0 {
		return nil, nil, &ValidationError{Problems: refused}
	}
	return refused, created, nil
}

// withItemIDs gives every list item that the schema keys by id and that
// arrives without one a generated id (sc1, sc2): ids are generated,
// never left to the writer (AGENTS.md), so an item is never refused
// for the one field nobody reads from a document.
func (e *Engine) withItemIDs(kind string, put map[string]any) {
	for p, v := range put {
		var items []any
		base := p
		switch t := v.(type) {
		case []any:
			items = t
		case map[string]any:
			if b, ok := strings.CutSuffix(p, "/-"); ok {
				items, base = []any{t}, b
			}
		}
		if len(items) == 0 {
			continue
		}
		sh, ok := e.FieldShapeAt(kind, base+"/-")
		if !ok {
			continue
		}
		ex, _ := sh.Example.(map[string]any)
		if _, keyed := ex["id"]; !keyed && !slices.Contains(sh.Optional, "id") {
			continue
		}
		taken := map[string]bool{}
		for _, it := range items {
			if m, ok := it.(map[string]any); ok {
				if s, _ := m["id"].(string); s != "" {
					taken[s] = true
				}
			}
		}
		prefix := idPrefix(base)
		n := 0
		for _, it := range items {
			m, ok := it.(map[string]any)
			if !ok {
				continue
			}
			if s, _ := m["id"].(string); s != "" {
				continue
			}
			for {
				n++
				if id := fmt.Sprintf("%s%d", prefix, n); !taken[id] {
					m["id"], taken[id] = id, true
					break
				}
			}
			if _, appended := v.(map[string]any); appended {
				// One item appended: an id unlikely to meet the list's own.
				m["id"] = fmt.Sprintf("%s-%x", prefix, time.Now().UnixNano()&0xffffff)
			}
		}
	}
}

// idPrefix is the initials of a list's field: successCriteria is sc,
// deliverables d.
func idPrefix(pointer string) string {
	name := pointer[strings.LastIndex(pointer, "/")+1:]
	out := strings.ToLower(name[:1])
	for _, r := range name[1:] {
		if r >= 'A' && r <= 'Z' {
			out += strings.ToLower(string(r))
		}
	}
	return out
}

// mergeLists makes setting a whole list that the draft already holds
// items of a merge: an item known by its id, else by its name,
// description, KPI or statement, has what was sent laid over it, a new
// one is added, and every other item is kept. A list of items never
// shrinks by being sent again; an item goes only with unset. A list of
// plain values (scope lines, references) is set as sent.
func (e *Engine) mergeLists(ctx context.Context, set, kind, id string, put map[string]any) map[string]any {
	text, found, err := e.ChangeSetText(ctx, set, kind, id)
	if err != nil || !found {
		return put
	}
	var doc map[string]any
	if e.codec.DecodeInto(text, &doc) != nil {
		return put
	}
	out := make(map[string]any, len(put))
	for p, v := range put {
		out[p] = v
		next, isList := v.([]any)
		// An item added to the end of a list is merged the same way, so
		// one the list already holds is not added twice.
		appended := false
		if base, ok := strings.CutSuffix(p, "/-"); ok {
			if _, isItem := v.(map[string]any); isItem {
				if _, both := put[base]; !both {
					next, isList, appended = []any{v}, true, true
					p = base
				}
			}
		}
		if !isList {
			continue
		}
		var cur any = doc
		for _, seg := range strings.Split(strings.Trim(p, "/"), "/") {
			m, ok := cur.(map[string]any)
			if !ok {
				cur = nil
				break
			}
			cur = m[seg]
		}
		have, ok := cur.([]any)
		if !ok || len(have) == 0 {
			continue
		}
		merged, keyed := mergeItems(have, next)
		if keyed {
			out[p] = merged
			if appended {
				delete(out, p+"/-")
			}
		}
	}
	return out
}

// itemNames are the fields that name a list item when it has no id.
var itemNames = []string{"name", "description", "kpi", "statement", "item"}

// mergeItems lays next over have, each item matched by its id or a field
// that names it; keyed is false when an item sent has neither, and then
// there is nothing to merge by.
func mergeItems(have, next []any) ([]any, bool) {
	keyOf := func(item any) string {
		m, _ := item.(map[string]any)
		for _, k := range append([]string{"id"}, itemNames...) {
			if s, _ := m[k].(string); strings.TrimSpace(s) != "" {
				return k + ":" + strings.ToLower(strings.TrimSpace(s))
			}
		}
		return ""
	}
	merged := append([]any(nil), have...)
	index := map[string]int{}
	for i, it := range merged {
		if k := keyOf(it); k != "" {
			index[k] = i
		}
		// An item with an id is also known by its name, so one sent
		// again without its id still finds it.
		if m, ok := it.(map[string]any); ok {
			for _, f := range itemNames {
				if s, _ := m[f].(string); strings.TrimSpace(s) != "" {
					index[f+":"+strings.ToLower(strings.TrimSpace(s))] = i
				}
			}
		}
	}
	for _, it := range next {
		k := keyOf(it)
		if k == "" {
			return nil, false
		}
		i, ok := index[k]
		if !ok {
			index[k] = len(merged)
			merged = append(merged, it)
			continue
		}
		// The item as it was, with what was sent over it: an id or a
		// field not sent again is kept.
		old, oldOK := merged[i].(map[string]any)
		nw, newOK := it.(map[string]any)
		if !oldOK || !newOK {
			merged[i] = it
			continue
		}
		both := make(map[string]any, len(old)+len(nw))
		for f, v := range old {
			both[f] = v
		}
		for f, v := range nw {
			both[f] = v
		}
		merged[i] = both
	}
	return merged, true
}
