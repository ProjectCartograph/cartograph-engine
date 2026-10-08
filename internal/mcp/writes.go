package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
)

// How an agent's write is applied: names read as references, formats read, lists merged, ids given, and what may be left open.

// settleOf reads a settle call: its named inputs, and any JSON pointer
// beside them or under fields as a field to set.
func settleOf(raw map[string]any) (settleIn, error) {
	var in settleIn
	b, _ := json.Marshal(raw)
	if err := json.Unmarshal(b, &in); err != nil {
		return in, fmt.Errorf("settle: %w; send {kind, id, set: {JSON pointer: value}, open: [{check, reason}], asked, work}", err)
	}
	if in.Set == nil {
		in.Set = map[string]any{}
	}
	if f, ok := raw["fields"].(map[string]any); ok {
		for k, v := range f {
			in.Set[k] = v
		}
	}
	var unknown []string
	for k, v := range raw {
		switch {
		case strings.HasPrefix(k, "/"):
			in.Set[k] = v
		case k == "fields" || settleKeys[k]:
		default:
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return in, fmt.Errorf("settle does not take %s: fields go under set, as JSON pointers such as /spec/summary/about", strings.Join(unknown, ", "))
	}
	return in, nil
}

// settleKeys are settle's own inputs.
var settleKeys = map[string]bool{"changeSet": true, "kind": true, "id": true, "set": true, "unset": true, "open": true, "asked": true, "work": true}

// withFields adds to each refused field what goes there, from the kind's
// guide, so an agent sent "additional properties 'statement' not allowed"
// learns the fields it meant: a refusal that teaches, not only refuses.
func withFields(c call, kind string, err error) error {
	var invalid *engine.ValidationError
	if !errors.As(err, &invalid) {
		return err
	}
	g, gerr := c.o.Engine.Guide(c.ctx, kind, "", "")
	if gerr != nil {
		return err
	}
	var paths []string
	for _, st := range g.Steps {
		for _, f := range st.Fields {
			paths = append(paths, f.Path)
		}
	}
	out := &engine.ValidationError{}
	for _, p := range invalid.Problems {
		var segs []string
		for _, t := range strings.Split(strings.Trim(p.Path, "/"), "/") {
			if _, num := strconv.Atoi(t); num == nil {
				t = "-"
			}
			segs = append(segs, t)
		}
		at := "/" + strings.Join(segs, "/")
		var here []string
		for _, q := range paths {
			if rest, ok := strings.CutPrefix(q, at+"/"); ok && rest != "" {
				// A child, or a list's item's child: /spec/x/-/y.
				// Two levels down at most, list items not counted, so a
				// problem's fields reach problem/situation.
				if strings.Count(strings.ReplaceAll(rest, "-/", ""), "/") <= 1 {
					here = append(here, strings.Replace(q, "/-/", "/0/", -1))
				}
			}
		}
		if len(here) > 0 {
			if len(here) > 12 {
				here = here[:12]
			}
			p.Message += ". The fields there are: " + strings.Join(here, ", ")
		}
		out.Problems = append(out.Problems, p)
	}
	return out
}

// writtenFromTheDocument are checks on what a document itself states: its
// objective, problem and change, who it serves, its scope, what it hands
// over, how success is known, its schedule and what authorises it. An
// agent working from documents without its person writes these; it
// cannot leave them open as not available (docs/adr/0027), so a port is
// never proposed hollow with its content waived.
var writtenFromTheDocument = map[string]bool{
	"goals-objective": true, "aim-problem-change": true, "beneficiaries-named": true, "scope-in": true,
	"deliverables-count": true, "success-criteria": true, "timeline-start-phases": true, "aim-mandate": true,
}

// mayLeave refuses leaving open, with no person to ask, a check the
// document answers.
func mayLeave(check, asked string) error {
	if writtenFromTheDocument[check] && strings.EqualFold(strings.TrimSpace(asked), "not available") {
		return fmt.Errorf("%w: %s is what the document itself says: write it from the document with settle, rather than leave it open. "+
			"If the document truly does not say it, do not propose: tell your person what it lacks", engine.ErrBadEdit, check)
	}
	return nil
}

// applyFields sets and clears fields on a draft the way every agent write
// does (docs/adr/0027): a name where a register's reference goes finds or
// drafts it, a value is read in its field's format, and every field that
// can be kept is kept, the refused ones returned with what goes there.
// It answers the problems refused and the records drafted (and lines cut,
// prefixed "cut: ").
func applyFields(c call, set, kind, id string, put map[string]any, unset []string) ([]engine.Problem, []string, error) {
	e := c.o.Engine
	var created []string
	var err error
	if len(put) > 0 {
		if put, created, err = e.NamedRefs(c.ctx, set, kind, put); err != nil {
			return nil, nil, withFields(c, kind, err)
		}
	}
	if len(put) == 0 && len(unset) == 0 {
		return nil, created, nil
	}
	put = mergeLists(c, set, kind, id, put)
	withItemIDs(c.o.Engine, kind, put)
	_, err = e.EditInChangeSet(c.ctx, set, kind, id, put, unset)
	if err == nil {
		return nil, created, nil
	}
	var invalid *engine.ValidationError
	if !errors.As(err, &invalid) {
		return nil, nil, withFields(c, kind, err)
	}
	var refused []engine.Problem
	keys := make([]string, 0, len(put))
	for k := range put {
		keys = append(keys, k)
	}
	// Parents before children, as one edit would set them.
	sort.Slice(keys, func(i, j int) bool {
		return len(keys[i]) < len(keys[j]) || len(keys[i]) == len(keys[j]) && keys[i] < keys[j]
	})
	for _, k := range keys {
		if _, err := e.EditInChangeSet(c.ctx, set, kind, id, map[string]any{k: put[k]}, nil); err != nil {
			if taught, ok := withFields(c, kind, err).(*engine.ValidationError); ok {
				refused = append(refused, taught.Problems...)
			} else {
				refused = append(refused, engine.Problem{Path: k, Message: err.Error()})
			}
		}
	}
	if len(unset) > 0 {
		if _, err := e.EditInChangeSet(c.ctx, set, kind, id, nil, unset); err != nil {
			refused = append(refused, engine.Problem{Message: err.Error()})
		}
	}
	// Nothing kept is a refusal, as it was before any field was tried.
	if len(refused) >= len(keys) && len(keys) > 0 && len(unset) == 0 {
		return nil, nil, withFields(c, kind, &engine.ValidationError{Problems: refused})
	}
	return refused, created, nil
}

// schemaOnly reports whether every problem is the schema's: only then is
// a whole manifest built field by field, so the order of work and the
// rule against an agent's placeholder still refuse it whole.
func schemaOnly(ps []engine.Problem) bool {
	for _, p := range ps {
		if p.Keyword == "" {
			return false
		}
	}
	return len(ps) > 0
}

// withItemIDs gives every list item that the schema keys by id and that
// arrives without one a generated id (sc1, sc2): ids are generated,
// never left to the writer (AGENTS.md), so an item is never refused
// for the one field nobody reads from a document.
func withItemIDs(e *engine.Engine, kind string, put map[string]any) {
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
// items of a merge by id: an item with an id the list has replaces it, a
// new one is added, and every other item is kept. An agent re-sending a
// list never loses what the server wrote into it (a register's rows); it
// removes an item with unset.
func mergeLists(c call, set, kind, id string, put map[string]any) map[string]any {
	text, found, err := c.o.Engine.ChangeSetText(c.ctx, set, kind, id)
	if err != nil || !found {
		return put
	}
	var doc map[string]any
	if c.o.Engine.Codec().DecodeInto(text, &doc) != nil {
		return put
	}
	out := make(map[string]any, len(put))
	for p, v := range put {
		out[p] = v
		next, isList := v.([]any)
		// An item added to the end of a list is merged the same way, so
		// one the list already holds (by id or name) is not added twice.
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
		// An item is known by its id, else by what names it: its name,
		// description, KPI or statement.
		idOf := func(item any) string {
			m, _ := item.(map[string]any)
			for _, k := range []string{"id", "name", "description", "kpi", "statement", "item"} {
				if s, _ := m[k].(string); strings.TrimSpace(s) != "" {
					return k + ":" + strings.ToLower(strings.TrimSpace(s))
				}
			}
			return ""
		}
		merged := append([]any(nil), have...)
		index := map[string]int{}
		for i, it := range merged {
			if k := idOf(it); k != "" {
				index[k] = i
			}
			// An item with an id is also known by its name, so one sent
			// again without its id still finds it.
			if m, ok := it.(map[string]any); ok {
				for _, f := range []string{"name", "description", "kpi", "statement", "item"} {
					if s, _ := m[f].(string); strings.TrimSpace(s) != "" {
						index[f+":"+strings.ToLower(strings.TrimSpace(s))] = i
					}
				}
			}
		}
		keyed := true
		for _, it := range next {
			k := idOf(it)
			if k == "" {
				keyed = false
				break
			}
			if i, ok := index[k]; ok {
				// The item as it was, with what was sent over it: an id or
				// a field not sent again is kept.
				old, oldOK := merged[i].(map[string]any)
				nw, newOK := it.(map[string]any)
				if oldOK && newOK {
					both := make(map[string]any, len(old)+len(nw))
					for f, v := range old {
						both[f] = v
					}
					for f, v := range nw {
						both[f] = v
					}
					merged[i] = both
				} else {
					merged[i] = it
				}
			} else {
				index[k] = len(merged)
				merged = append(merged, it)
			}
		}
		// A list of plain values (scope lines, references) is set as sent;
		// a list of items never shrinks by being sent again.
		if keyed {
			out[p] = merged
			if appended {
				delete(out, p+"/-")
			}
		}
	}
	return out
}
