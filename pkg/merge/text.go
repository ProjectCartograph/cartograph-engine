package merge

import (
	"strconv"
	"strings"
)

// Text is a replicated sequence of characters: the Replicated Growable
// Array (Roh, Jeon, Kim, Lee, "Replicated abstract data types: Building
// blocks for collaborative applications", 2011). Every character has a
// unique id (the clock that inserted it) and names the character it was
// typed after; a deleted character stays as a tombstone so a concurrent
// insert after it still has its anchor. Two people typing in the same
// sentence at once both keep what they typed, in a deterministic order,
// on every replica.
//
// A text leaf in a State holds one of these instead of a scalar. The
// field is still one leaf: a plain Set on it (from a client that does
// not speak text ops, or an import) replaces the whole text with a new
// one seeded from the string, under the usual last-writer rule, and
// text ops from then on edit that one.
type Text struct {
	chars   []textChar         // in document order, tombstones included
	index   map[Clock]int      // id -> position in chars
	pending []TextOp           // ops whose anchor has not arrived yet
	seen    map[Clock]struct{} // ids already applied (inserts) or deleted
}

type textChar struct {
	id      Clock
	r       rune
	deleted bool
}

// TextOp is one edit to a text leaf: insert rune R with id ID after the
// character After (zero Clock means at the start), or delete the
// character ID.
type TextOp struct {
	Insert bool  `json:"insert,omitempty"`
	ID     Clock `json:"id"`
	After  Clock `json:"after,omitempty"`
	R      rune  `json:"r,omitempty"`
}

// NewText returns an empty Text.
func NewText() *Text {
	return &Text{index: map[Clock]int{}, seen: map[Clock]struct{}{}}
}

// TextFrom seeds a Text with s, every character stamped from clock
// onwards (logical counter advancing per character) by the given actor.
func TextFrom(s string, clock Clock) *Text {
	t := NewText()
	after := Clock{}
	for i, r := range []rune(s) {
		// Seeded characters are stamped under the leaf's own clock with a
		// per-character actor suffix, so every replica seeds the same
		// ids and none of them can collide with a stamp the actor's
		// clock issues later.
		id := Clock{Wall: clock.Wall, Logical: clock.Logical, Actor: clock.Actor + "\x1f" + strconv.Itoa(i)}
		t.Apply(TextOp{Insert: true, ID: id, After: after, R: r})
		after = id
	}
	return t
}

// String materialises the text.
func (t *Text) String() string {
	var b strings.Builder
	for _, c := range t.chars {
		if !c.deleted {
			b.WriteRune(c.r)
		}
	}
	return b.String()
}

// Apply merges one op. An insert whose anchor is unknown waits until the
// anchor arrives; a duplicate is ignored. Returns whether the op took
// effect now.
func (t *Text) Apply(op TextOp) bool {
	if t.index == nil {
		t.index = map[Clock]int{}
		t.seen = map[Clock]struct{}{}
	}
	if !op.Insert {
		if i, ok := t.index[op.ID]; ok {
			t.chars[i].deleted = true
			return true
		}
		// Delete of a character not yet seen: remember it.
		t.pending = append(t.pending, op)
		return false
	}
	if _, dup := t.seen[op.ID]; dup {
		return true
	}
	if !op.After.IsZero() {
		if _, ok := t.index[op.After]; !ok {
			t.pending = append(t.pending, op)
			return false
		}
	}
	t.insert(op)
	t.drain()
	return true
}

func (t *Text) insert(op TextOp) {
	// RGA: the new character goes right after its anchor, but behind any
	// character already there whose id is greater than its own (those
	// were inserted later and sit closer to the anchor by this same rule).
	pos := 0
	if !op.After.IsZero() {
		pos = t.index[op.After] + 1
	}
	for pos < len(t.chars) && op.ID.Less(t.chars[pos].id) {
		pos++
	}
	t.chars = append(t.chars, textChar{})
	copy(t.chars[pos+1:], t.chars[pos:])
	t.chars[pos] = textChar{id: op.ID, r: op.R}
	for i := pos; i < len(t.chars); i++ {
		t.index[t.chars[i].id] = i
	}
	t.seen[op.ID] = struct{}{}
}

// drain retries pending ops until none can proceed.
func (t *Text) drain() {
	for progress := true; progress && len(t.pending) > 0; {
		progress = false
		rest := t.pending[:0]
		for _, op := range t.pending {
			ready := !op.Insert || op.After.IsZero()
			if op.Insert && !op.After.IsZero() {
				_, ready = t.index[op.After]
			}
			if !op.Insert {
				_, ready = t.index[op.ID]
			}
			if ready {
				if op.Insert {
					if _, dup := t.seen[op.ID]; !dup {
						t.insert(op)
					}
				} else {
					t.chars[t.index[op.ID]].deleted = true
				}
				progress = true
			} else {
				rest = append(rest, op)
			}
		}
		t.pending = rest
	}
}

// Merge folds another Text in by replaying its characters as inserts
// and its tombstones as deletes. Commutative, associative, idempotent.
func (t *Text) Merge(o *Text) {
	for _, op := range o.Ops() {
		t.Apply(op)
	}
}

// Ops returns the inserts (in document order, so anchors precede what
// follows them) and then the deletes that rebuild this text elsewhere.
func (t *Text) Ops() []TextOp {
	out := make([]TextOp, 0, len(t.chars)*2)
	var after Clock
	for _, c := range t.chars {
		out = append(out, TextOp{Insert: true, ID: c.id, After: after, R: c.r})
		after = c.id
	}
	for _, c := range t.chars {
		if c.deleted {
			out = append(out, TextOp{ID: c.id})
		}
	}
	return out
}

// Clone copies the text.
func (t *Text) Clone() *Text {
	out := NewText()
	out.chars = append([]textChar(nil), t.chars...)
	for i, c := range out.chars {
		out.index[c.id] = i
		out.seen[c.id] = struct{}{}
	}
	out.pending = append([]TextOp(nil), t.pending...)
	return out
}

// Edit computes the text ops that turn the current string into want, as
// one person typing would: the common prefix and suffix are kept, the
// middle is deleted and retyped. Each new character gets its own clock
// from next. This is how a client that holds a plain input box (the web,
// a terminal) turns a keystroke into ops without tracking cursors.
func (t *Text) Edit(want string, next func() Clock) []TextOp {
	cur := []rune(t.String())
	target := []rune(want)
	p := 0
	for p < len(cur) && p < len(target) && cur[p] == target[p] {
		p++
	}
	s := 0
	for s < len(cur)-p && s < len(target)-p && cur[len(cur)-1-s] == target[len(target)-1-s] {
		s++
	}
	// Live characters in order, to map positions to ids.
	live := make([]Clock, 0, len(cur))
	for _, c := range t.chars {
		if !c.deleted {
			live = append(live, c.id)
		}
	}
	var ops []TextOp
	for i := p; i < len(cur)-s; i++ {
		ops = append(ops, TextOp{ID: live[i]})
	}
	after := Clock{}
	if p > 0 {
		after = live[p-1]
	}
	for i := p; i < len(target)-s; i++ {
		id := next()
		ops = append(ops, TextOp{Insert: true, ID: id, After: after, R: target[i]})
		after = id
	}
	return ops
}
