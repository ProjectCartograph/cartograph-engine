package merge

import (
	"math/rand"
	"testing"
)

func TestTextFromAndString(t *testing.T) {
	x := TextFrom("Deliveries meet the grade.", Clock{Wall: 1, Actor: "a"})
	if x.String() != "Deliveries meet the grade." {
		t.Fatalf("got %q", x.String())
	}
}

func TestTwoPeopleTypingInOneSentenceBothKeepTheirWords(t *testing.T) {
	base := TextFrom("Deliveries meet grade.", Clock{Wall: 1, Actor: "seed"})
	a, b := base.Clone(), base.Clone()
	ha, hb := &HLC{Actor: "a"}, &HLC{Actor: "b"}
	// a inserts "the " before "grade"; b appends " every week" before the full stop.
	opsA := a.Edit("Deliveries meet the grade.", func() Clock { return ha.Now(2) })
	for _, op := range opsA {
		a.Apply(op)
	}
	opsB := b.Edit("Deliveries meet grade every week.", func() Clock { return hb.Now(2) })
	for _, op := range opsB {
		b.Apply(op)
	}
	for _, op := range opsB {
		a.Apply(op)
	}
	for _, op := range opsA {
		b.Apply(op)
	}
	if a.String() != b.String() {
		t.Fatalf("replicas differ: %q vs %q", a.String(), b.String())
	}
	if a.String() != "Deliveries meet the grade every week." {
		t.Fatalf("both edits must survive: %q", a.String())
	}
}

func TestTextConvergesInAnyOrderWithPendingAnchors(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	seed := TextFrom("abcdef", Clock{Wall: 1, Actor: "s"})
	reps := []*Text{seed.Clone(), seed.Clone(), seed.Clone()}
	clocks := []*HLC{{Actor: "a"}, {Actor: "b"}, {Actor: "c"}}
	var all []TextOp
	for i := 0; i < 150; i++ {
		r := rng.Intn(3)
		rep := reps[r]
		cur := []rune(rep.String())
		var want string
		if rng.Intn(3) == 0 && len(cur) > 0 {
			k := rng.Intn(len(cur))
			want = string(append(append([]rune{}, cur[:k]...), cur[k+1:]...))
		} else {
			k := rng.Intn(len(cur) + 1)
			want = string(append(append(append([]rune{}, cur[:k]...), rune('A'+rng.Intn(26))), cur[k:]...))
		}
		ops := rep.Edit(want, func() Clock { return clocks[r].Now(int64(10 + i)) })
		for _, op := range ops {
			rep.Apply(op)
		}
		all = append(all, ops...)
	}
	var want string
	for trial := 0; trial < 20; trial++ {
		shuffled := append([]TextOp(nil), all...)
		rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		x := seed.Clone()
		for _, op := range shuffled {
			x.Apply(op)
		}
		if len(x.pending) != 0 {
			t.Fatalf("trial %d: %d ops never found their anchor", trial, len(x.pending))
		}
		if want == "" {
			want = x.String()
			continue
		}
		if x.String() != want {
			t.Fatalf("trial %d diverged: %q vs %q", trial, x.String(), want)
		}
	}
	for i, rep := range reps {
		for _, op := range all {
			rep.Apply(op)
		}
		if rep.String() != want {
			t.Fatalf("replica %d: %q vs %q", i, rep.String(), want)
		}
	}
}

func TestTextMergeIsIdempotentAndCommutative(t *testing.T) {
	a := TextFrom("one", Clock{Wall: 1, Actor: "a"})
	b := TextFrom("two", Clock{Wall: 1, Actor: "b"})
	ab := a.Clone()
	ab.Merge(b)
	ba := b.Clone()
	ba.Merge(a)
	if ab.String() != ba.String() {
		t.Fatalf("commutative: %q vs %q", ab.String(), ba.String())
	}
	again := ab.Clone()
	again.Merge(ab)
	if again.String() != ab.String() {
		t.Fatal("idempotent")
	}
}

func TestStateHoldsTextLeaves(t *testing.T) {
	s := New()
	h := &HLC{Actor: "a"}
	s.Apply(Op{Path: "/spec/objective", Value: "Deliveries meet grade.", Clock: h.Now(1)})
	// A text op on a scalar leaf turns it into text seeded from the scalar.
	ops := s.TextEdit("/spec/objective", "Deliveries meet the grade.", h)
	for _, op := range ops {
		s.Apply(op)
	}
	if got := s.Document()["spec"].(map[string]any)["objective"]; got != "Deliveries meet the grade." {
		t.Fatalf("got %q", got)
	}
	// A later plain Set replaces the text wholesale.
	s.Apply(Op{Path: "/spec/objective", Value: "Replaced.", Clock: h.Now(5)})
	if got := s.Document()["spec"].(map[string]any)["objective"]; got != "Replaced." {
		t.Fatalf("got %q", got)
	}
	// And text edits after that edit the new text.
	for _, op := range s.TextEdit("/spec/objective", "Replaced, then edited.", h) {
		s.Apply(op)
	}
	if got := s.Document()["spec"].(map[string]any)["objective"]; got != "Replaced, then edited." {
		t.Fatalf("got %q", got)
	}
}
