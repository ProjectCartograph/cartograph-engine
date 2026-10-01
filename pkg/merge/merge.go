// Package merge is the data structure under multiplayer editing: a
// last-writer-wins map over a manifest's leaf paths, which is a CRDT
// (Shapiro et al., "A comprehensive study of Convergent and Commutative
// Replicated Data Types", 2011, section 3.2, LWW-Register and LWW-Map).
// Any set of operations, applied in any order, materialises the same
// document. That is the property the two interfaces (and anyone's third)
// rely on to converge without a coordinator.
//
// Why this and not a general JSON CRDT. A manifest is a form, not a prose
// document: fields are short, most are scalars or references, and the
// lists that matter (key results, deliverables, stakeholders) are lists
// of identified things. Last writer wins per field, with lists merged by
// key, is what Kubernetes server-side apply does for the same shape of
// document, and it is what a person expects: the field shows what was
// last saved, and a conflict is a note, never a lost edit. A character
// level text CRDT (RGA, Peritext) can back one control later if a long
// text field ever needs co-typing; nothing here prevents it.
//
// Paths are JSON pointers (RFC 6901) with one extension: an element of a
// keyed list is addressed by its key, not its index, written {key}. So
// /spec/keyResults/{kr-2}/target is the target of the key result whose
// key is kr-2, wherever it sits. Which lists are keyed, and by what, is
// the schema's business (x-cartograph-list-key); this package only needs the
// paths it is given to be stable, which keys are and indices are not.
package merge

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Clock is a hybrid logical clock stamp (Kulkarni et al., 2014): wall
// time in milliseconds, a logical counter for events within one
// millisecond, and the actor as the final tiebreak. Total order, so two
// replicas always agree on which write wins.
type Clock struct {
	Wall    int64  `json:"wall"`
	Logical uint32 `json:"logical"`
	Actor   string `json:"actor"`
}

// Less orders clocks: wall, then logical, then actor.
func (c Clock) Less(o Clock) bool {
	if c.Wall != o.Wall {
		return c.Wall < o.Wall
	}
	if c.Logical != o.Logical {
		return c.Logical < o.Logical
	}
	return c.Actor < o.Actor
}

// IsZero reports an unset clock.
func (c Clock) IsZero() bool { return c.Wall == 0 && c.Logical == 0 && c.Actor == "" }

// HLC issues clocks for one actor. Now never returns a clock lower than
// one it has seen (Observe), so a replica that received a newer write
// stamps its next write after it, whatever its wall clock says.
type HLC struct {
	Actor string
	wall  int64
	logic uint32
}

// Now stamps a new event at wall time ms.
func (h *HLC) Now(ms int64) Clock {
	if ms > h.wall {
		h.wall, h.logic = ms, 0
	} else {
		h.logic++
	}
	return Clock{Wall: h.wall, Logical: h.logic, Actor: h.Actor}
}

// Observe advances the clock past a stamp received from elsewhere.
func (h *HLC) Observe(c Clock) {
	if c.Wall > h.wall || (c.Wall == h.wall && c.Logical > h.logic) {
		h.wall, h.logic = c.Wall, c.Logical
	}
}

// Op is one write to one leaf: set Value at Path, or delete it. Base is
// the clock of the value the writer saw there, so a concurrent overwrite
// can be noticed (see Conflict); zero when the writer saw nothing.
type Op struct {
	Path   string `json:"path"`
	Value  any    `json:"value,omitempty"`
	Delete bool   `json:"delete,omitempty"`
	Clock  Clock  `json:"clock"`
	Base   Clock  `json:"base"`
	// Text, when set, is a character edit to a text leaf rather than a
	// write of the whole value; Value and Delete are then ignored.
	Text *TextOp `json:"text,omitempty"`
}

type entry struct {
	value   any
	deleted bool
	clock   Clock
	text    *Text // set when the leaf is a text under character merging
}

// State is the replicated map. The zero value is empty and usable.
type State struct {
	leaves map[string]entry
}

// New returns an empty State.
func New() *State { return &State{leaves: map[string]entry{}} }

func (s *State) init() {
	if s.leaves == nil {
		s.leaves = map[string]entry{}
	}
}

// Conflict is a write that landed on a leaf another actor had changed
// since the writer last saw it. The state is the same whichever order the
// writes arrived in (that is the CRDT); the conflict is a note for the
// people, so neither value is silently lost. Winner is what the leaf now
// holds; Loser is the write that did not take.
type Conflict struct {
	Path   string
	Winner Op
	Loser  Op
}

// Apply merges one op into the state and reports whether it changed the
// leaf, and a Conflict when another actor's write sits between the op's
// Base and now.
func (s *State) Apply(op Op) (applied bool, conflict *Conflict) {
	s.init()
	if op.Text != nil {
		return s.applyText(op), nil
	}
	cur, ok := s.leaves[op.Path]
	if ok && cur.clock.Actor != op.Clock.Actor && !op.Base.IsZero() && cur.clock != op.Base {
		// Somebody else wrote here after what this writer saw.
		other := Op{Path: op.Path, Value: cur.value, Delete: cur.deleted, Clock: cur.clock}
		if cur.clock.Less(op.Clock) {
			conflict = &Conflict{Path: op.Path, Winner: op, Loser: other}
		} else {
			conflict = &Conflict{Path: op.Path, Winner: other, Loser: op}
		}
	}
	if ok && !cur.clock.Less(op.Clock) {
		return false, conflict
	}
	s.leaves[op.Path] = entry{value: op.Value, deleted: op.Delete, clock: op.Clock}
	return true, conflict
}

// applyText merges a character edit. The leaf becomes a text on the first
// text op (seeded from its scalar value, if it had one). A text op older
// than a plain Set that replaced the text is dropped: the Set is the
// later decision.
func (s *State) applyText(op Op) bool {
	cur, ok := s.leaves[op.Path]
	if ok && cur.text == nil {
		if op.Text.Less(cur.clock) {
			return false
		}
		seed, _ := cur.value.(string)
		cur.text = TextFrom(seed, cur.clock)
		cur.value = nil
	}
	if !ok {
		cur = entry{text: NewText(), clock: Clock{}}
	}
	if op.Text.Less(cur.clock) {
		return false
	}
	applied := cur.text.Apply(*op.Text)
	s.leaves[op.Path] = cur
	return applied
}

// Less orders a text op's own stamp against a leaf clock.
func (t TextOp) Less(c Clock) bool { return t.ID.Less(c) }

// TextEdit returns the ops that change the text leaf at path to want, the
// way a client with a plain input turns its new value into character
// edits. A leaf that is not yet a text is treated as one seeded from its
// current string.
func (s *State) TextEdit(path, want string, h *HLC) []Op {
	s.init()
	cur, ok := s.leaves[path]
	var text *Text
	switch {
	case ok && cur.text != nil:
		text = cur.text.Clone()
	case ok:
		seed, _ := cur.value.(string)
		text = TextFrom(seed, cur.clock)
	default:
		text = NewText()
	}
	tops := text.Edit(want, func() Clock { return h.Now(h.wall) })
	out := make([]Op, len(tops))
	for i := range tops {
		top := tops[i]
		out[i] = Op{Path: path, Clock: top.ID, Text: &top}
		if !top.Insert {
			out[i].Clock = h.Now(h.wall)
		}
	}
	return out
}

// Merge folds another state in: per leaf, the higher clock wins.
// Commutative, associative and idempotent.
func (s *State) Merge(o *State) {
	s.init()
	for p, e := range o.leaves {
		cur, ok := s.leaves[p]
		switch {
		case !ok:
			if e.text != nil {
				e.text = e.text.Clone()
			}
			s.leaves[p] = e
		case e.text != nil && cur.text != nil && cur.clock == e.clock:
			cur.text.Merge(e.text)
		case cur.clock.Less(e.clock):
			if e.text != nil {
				e.text = e.text.Clone()
			}
			s.leaves[p] = e
		}
	}
}

// Clone copies the state.
func (s *State) Clone() *State {
	out := New()
	for p, e := range s.leaves {
		if e.text != nil {
			e.text = e.text.Clone()
		}
		out.leaves[p] = e
	}
	return out
}

// Ops returns every live leaf and tombstone as ops, sorted by path, so a
// state can be shipped or replayed.
func (s *State) Ops() []Op {
	out := make([]Op, 0, len(s.leaves))
	for p, e := range s.leaves {
		if e.text != nil {
			out = append(out, Op{Path: p, Value: e.text.String(), Clock: e.clock})
			for _, top := range e.text.Ops() {
				top := top
				out = append(out, Op{Path: p, Clock: top.ID, Text: &top})
			}
			continue
		}
		out = append(out, Op{Path: p, Value: e.value, Delete: e.deleted, Clock: e.clock})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// ClockAt returns the clock of a leaf, for a writer to carry as Base.
func (s *State) ClockAt(path string) (Clock, bool) {
	e, ok := s.leaves[path]
	return e.clock, ok
}

// Document materialises the state into the nested document a schema
// validates. Keyed lists come back as lists ordered by each element's
// "@rank" leaf when present, then by key; a scalar set (leaves of the
// form /list/{value} holding true) comes back as a sorted list of values.
func (s *State) Document() map[string]any {
	root := map[string]any{}
	paths := make([]string, 0, len(s.leaves))
	for p, e := range s.leaves {
		if !e.deleted {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	for _, p := range paths {
		e := s.leaves[p]
		if e.text != nil {
			setLeaf(root, Split(p), e.text.String())
			continue
		}
		setLeaf(root, Split(p), e.value)
	}
	return finish(root).(map[string]any)
}

// keyed is the intermediate shape of a keyed list while materialising:
// elements by key, finalised into an ordered slice by finish.
type keyed struct {
	elems map[string]any
}

func setLeaf(node map[string]any, segs []string, value any) {
	for i, seg := range segs {
		last := i == len(segs)-1
		if isKey(seg) {
			// The parent node must be a keyed list.
			return
		}
		if last {
			node[seg] = value
			return
		}
		next := segs[i+1]
		if isKey(next) {
			k, _ := node[seg].(*keyed)
			if k == nil {
				k = &keyed{elems: map[string]any{}}
				node[seg] = k
			}
			key := trimKey(next)
			if i+1 == len(segs)-1 {
				// /list/{value}: a set member.
				k.elems[key] = value
				return
			}
			child, _ := k.elems[key].(map[string]any)
			if child == nil {
				child = map[string]any{}
				k.elems[key] = child
			}
			node = child
			segs = segs[i+2:]
			setLeaf(node, segs, value)
			return
		}
		child, _ := node[seg].(map[string]any)
		if child == nil {
			child = map[string]any{}
			node[seg] = child
		}
		node = child
	}
}

func finish(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			t[k] = finish(val)
		}
		return t
	case *keyed:
		keys := make([]string, 0, len(t.elems))
		for k := range t.elems {
			keys = append(keys, k)
		}
		isSet := true
		for _, k := range keys {
			if _, ok := t.elems[k].(map[string]any); ok {
				isSet = false
				break
			}
		}
		if isSet {
			sort.Strings(keys)
			out := make([]any, 0, len(keys))
			for _, k := range keys {
				if b, ok := t.elems[k].(bool); ok && !b {
					continue
				}
				out = append(out, k)
			}
			return out
		}
		sort.Slice(keys, func(i, j int) bool {
			ri, rj := rankOf(t.elems[keys[i]]), rankOf(t.elems[keys[j]])
			if ri != rj {
				return ri < rj
			}
			return keys[i] < keys[j]
		})
		out := make([]any, 0, len(keys))
		for _, k := range keys {
			elem, _ := t.elems[k].(map[string]any)
			delete(elem, "@rank")
			out = append(out, finish(elem))
		}
		return out
	default:
		return v
	}
}

func rankOf(v any) string {
	if m, ok := v.(map[string]any); ok {
		if r, ok := m["@rank"].(string); ok {
			return r
		}
	}
	return "~" // unranked sorts last
}

// Keyer says whether a list at listPath is keyed and, for one element,
// which key names it. Return ok=false for a list that is not keyed (it
// is then stored as one leaf, last writer wins on the whole list).
type Keyer func(listPath string, elem map[string]any) (key string, ok bool)

// Decompose turns a whole document into leaf ops stamped with clock, the
// way a client that edits a form rather than single fields hands its
// draft back, and the way a committed version becomes the base state.
func Decompose(doc map[string]any, clock Clock, keys Keyer) []Op {
	var ops []Op
	walk("", doc, clock, keys, &ops)
	sort.Slice(ops, func(i, j int) bool { return ops[i].Path < ops[j].Path })
	return ops
}

func walk(path string, v any, clock Clock, keys Keyer, ops *[]Op) {
	switch t := v.(type) {
	case map[string]any:
		if len(t) == 0 {
			*ops = append(*ops, Op{Path: path, Value: map[string]any{}, Clock: clock})
			return
		}
		for k, val := range t {
			walk(path+"/"+Escape(k), val, clock, keys, ops)
		}
	case []any:
		if len(t) == 0 {
			*ops = append(*ops, Op{Path: path, Value: []any{}, Clock: clock})
			return
		}
		allScalar := true
		for _, e := range t {
			if _, ok := e.(map[string]any); ok {
				allScalar = false
				break
			}
		}
		if allScalar {
			// A set of scalars: /list/{value} = true.
			for _, e := range t {
				*ops = append(*ops, Op{Path: path + "/{" + Escape(fmt.Sprint(e)) + "}", Value: true, Clock: clock})
			}
			return
		}
		for i, e := range t {
			elem, _ := e.(map[string]any)
			key, ok := "", false
			if keys != nil && elem != nil {
				key, ok = keys(path, elem)
			}
			if !ok {
				// Not keyed: the whole list is one leaf.
				*ops = append(*ops, Op{Path: path, Value: t, Clock: clock})
				return
			}
			base := path + "/{" + Escape(key) + "}"
			*ops = append(*ops, Op{Path: base + "/@rank", Value: Rank(i, len(t)), Clock: clock})
			walk(base, elem, clock, keys, ops)
		}
	default:
		*ops = append(*ops, Op{Path: path, Value: v, Clock: clock})
	}
}

// Rank is a lexically sortable position for element i of n, with room
// to insert between neighbours without renumbering them (fractional
// indexing, the way collaborative editors order lists). Five digits per
// level is plenty for a form's lists.
func Rank(i, n int) string {
	return fmt.Sprintf("%05d", (i+1)*1000)
}

// Between returns a rank that sorts between a and b (either may be "").
func Between(a, b string) string {
	lo, hi := 0, 100000
	if a != "" {
		lo, _ = strconv.Atoi(a)
	}
	if b != "" {
		hi, _ = strconv.Atoi(b)
	}
	if hi-lo < 2 {
		return a + "5"
	}
	return fmt.Sprintf("%05d", (lo+hi)/2)
}

// Split splits a pointer into segments, unescaping RFC 6901's ~0 and ~1.
func Split(path string) []string {
	if path == "" || path == "/" {
		return nil
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for i, p := range parts {
		if isKey(p) {
			parts[i] = "{" + unescape(trimKey(p)) + "}"
		} else {
			parts[i] = unescape(p)
		}
	}
	return parts
}

// Escape escapes one segment per RFC 6901 (and the braces this package
// reserves for keys).
func Escape(seg string) string {
	r := strings.NewReplacer("~", "~0", "/", "~1", "{", "~2", "}", "~3")
	return r.Replace(seg)
}

func unescape(seg string) string {
	r := strings.NewReplacer("~3", "}", "~2", "{", "~1", "/", "~0", "~")
	return r.Replace(seg)
}

func isKey(seg string) bool {
	return len(seg) >= 2 && seg[0] == '{' && seg[len(seg)-1] == '}'
}

func trimKey(seg string) string { return seg[1 : len(seg)-1] }
