package merge

import (
	"math/rand"
	"reflect"
	"testing"
)

func keyResults(listPath string, elem map[string]any) (string, bool) {
	if listPath == "/spec/keyResults" {
		k, _ := elem["id"].(string)
		return k, k != ""
	}
	return "", false
}

func sampleDoc() map[string]any {
	return map[string]any{
		"apiVersion": "cartograph/v1",
		"kind":       "Goal",
		"metadata":   map[string]any{"id": "g1", "name": "Raise quality"},
		"spec": map[string]any{
			"level":     "objective",
			"objective": "Deliveries meet the grade.",
			"segments":  []any{"north", "south"},
			"keyResults": []any{
				map[string]any{"id": "kr-1", "metric": "graded A", "target": 90},
				map[string]any{"id": "kr-2", "metric": "returns", "target": 2},
			},
		},
	}
}

func TestDecomposeThenDocumentIsTheIdentity(t *testing.T) {
	doc := sampleDoc()
	s := New()
	for _, op := range Decompose(doc, Clock{Wall: 1, Actor: "a"}, keyResults) {
		s.Apply(op)
	}
	if got := s.Document(); !reflect.DeepEqual(got, sampleDoc()) {
		t.Fatalf("round trip changed the document:\n got %#v\nwant %#v", got, sampleDoc())
	}
}

func TestClockOrderAndHLC(t *testing.T) {
	h := &HLC{Actor: "a"}
	c1 := h.Now(100)
	c2 := h.Now(100) // same millisecond: logical advances
	c3 := h.Now(50)  // wall went backwards: still after c2
	if !c1.Less(c2) || !c2.Less(c3) {
		t.Fatalf("clocks must only go forward: %v %v %v", c1, c2, c3)
	}
	h.Observe(Clock{Wall: 500, Logical: 3, Actor: "b"})
	if c4 := h.Now(200); !(Clock{Wall: 500, Logical: 3, Actor: "b"}).Less(c4) {
		t.Fatalf("a stamp after an observed one must be later: %v", c4)
	}
	if !(Clock{Wall: 1, Actor: "a"}).Less(Clock{Wall: 1, Actor: "b"}) {
		t.Fatal("actor breaks ties")
	}
}

// The CRDT property: every order of the same ops gives the same document.
func TestAnyOrderConverges(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	actors := []string{"a", "b", "c"}
	clocks := map[string]*HLC{}
	for _, a := range actors {
		clocks[a] = &HLC{Actor: a}
	}
	var ops []Op
	paths := []string{"/spec/objective", "/spec/level", "/spec/keyResults/{kr-1}/target", "/spec/keyResults/{kr-1}/@rank",
		"/spec/keyResults/{kr-2}/metric", "/spec/keyResults/{kr-2}/@rank", "/spec/segments/{north}", "/spec/segments/{south}", "/metadata/name"}
	for i := 0; i < 200; i++ {
		a := actors[rng.Intn(len(actors))]
		p := paths[rng.Intn(len(paths))]
		op := Op{Path: p, Clock: clocks[a].Now(int64(1000 + rng.Intn(50)))}
		switch {
		case rng.Intn(6) == 0:
			op.Delete = true
		case p == "/spec/segments/{north}" || p == "/spec/segments/{south}":
			op.Value = rng.Intn(2) == 0
		case p == "/spec/keyResults/{kr-1}/@rank" || p == "/spec/keyResults/{kr-2}/@rank":
			op.Value = Rank(rng.Intn(3), 3)
		default:
			op.Value = rng.Intn(1000)
		}
		ops = append(ops, op)
	}
	var want map[string]any
	for trial := 0; trial < 25; trial++ {
		shuffled := append([]Op(nil), ops...)
		rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		s := New()
		for _, op := range shuffled {
			s.Apply(op)
		}
		got := s.Document()
		if want == nil {
			want = got
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("order %d diverged:\n got %#v\nwant %#v", trial, got, want)
		}
	}
}

func TestMergeIsCommutativeAssociativeIdempotent(t *testing.T) {
	mk := func(actor string, wall int64, v any) *State {
		s := New()
		s.Apply(Op{Path: "/spec/objective", Value: v, Clock: Clock{Wall: wall, Actor: actor}})
		s.Apply(Op{Path: "/spec/" + actor, Value: actor, Clock: Clock{Wall: wall, Actor: actor}})
		return s
	}
	a, b, c := mk("a", 1, "A"), mk("b", 2, "B"), mk("c", 3, "C")
	ab := a.Clone()
	ab.Merge(b)
	ba := b.Clone()
	ba.Merge(a)
	if !reflect.DeepEqual(ab.Document(), ba.Document()) {
		t.Fatal("merge is not commutative")
	}
	abc := ab.Clone()
	abc.Merge(c)
	bc := b.Clone()
	bc.Merge(c)
	a_bc := a.Clone()
	a_bc.Merge(bc)
	if !reflect.DeepEqual(abc.Document(), a_bc.Document()) {
		t.Fatal("merge is not associative")
	}
	again := abc.Clone()
	again.Merge(abc)
	if !reflect.DeepEqual(again.Document(), abc.Document()) {
		t.Fatal("merge is not idempotent")
	}
	if abc.Document()["spec"].(map[string]any)["objective"] != "C" {
		t.Fatal("the latest clock wins")
	}
}

func TestConflictIsNotedAndNothingIsLost(t *testing.T) {
	s := New()
	base := Clock{Wall: 1, Actor: "a"}
	s.Apply(Op{Path: "/spec/objective", Value: "first", Clock: base})

	// b edits from the base; a edits from the base too, later.
	_, c1 := s.Apply(Op{Path: "/spec/objective", Value: "b's", Clock: Clock{Wall: 2, Actor: "b"}, Base: base})
	if c1 != nil {
		t.Fatalf("writing over what you saw is not a conflict: %+v", c1)
	}
	applied, c2 := s.Apply(Op{Path: "/spec/objective", Value: "a's", Clock: Clock{Wall: 3, Actor: "a"}, Base: base})
	if !applied || c2 == nil || c2.Loser.Value != "b's" || c2.Winner.Value != "a's" {
		t.Fatalf("a's later write wins and b's is kept as the loser: %v %+v", applied, c2)
	}
	// The same two writes in the other order: the state is the same, the
	// note names the same winner.
	s2 := New()
	s2.Apply(Op{Path: "/spec/objective", Value: "first", Clock: base})
	s2.Apply(Op{Path: "/spec/objective", Value: "a's", Clock: Clock{Wall: 3, Actor: "a"}, Base: base})
	applied, c3 := s2.Apply(Op{Path: "/spec/objective", Value: "b's", Clock: Clock{Wall: 2, Actor: "b"}, Base: base})
	if applied || c3 == nil || c3.Winner.Value != "a's" || c3.Loser.Value != "b's" {
		t.Fatalf("older write arriving later loses, and is noted: %v %+v", applied, c3)
	}
	if !reflect.DeepEqual(s.Document(), s2.Document()) {
		t.Fatal("state must not depend on arrival order")
	}
}

func TestKeyedListsMergeByKeyAndKeepOrder(t *testing.T) {
	s := New()
	for _, op := range Decompose(sampleDoc(), Clock{Wall: 1, Actor: "a"}, keyResults) {
		s.Apply(op)
	}
	// b changes kr-2's target while a inserts kr-3 between the two.
	s.Apply(Op{Path: "/spec/keyResults/{kr-2}/target", Value: 1, Clock: Clock{Wall: 2, Actor: "b"}})
	r1, _ := s.leaves["/spec/keyResults/{kr-1}/@rank"]
	r2, _ := s.leaves["/spec/keyResults/{kr-2}/@rank"]
	mid := Between(r1.value.(string), r2.value.(string))
	s.Apply(Op{Path: "/spec/keyResults/{kr-3}/@rank", Value: mid, Clock: Clock{Wall: 2, Actor: "a"}})
	s.Apply(Op{Path: "/spec/keyResults/{kr-3}/id", Value: "kr-3", Clock: Clock{Wall: 2, Actor: "a"}})
	s.Apply(Op{Path: "/spec/keyResults/{kr-3}/metric", Value: "late deliveries", Clock: Clock{Wall: 2, Actor: "a"}})
	// and c removes "south" from the segments set.
	s.Apply(Op{Path: "/spec/segments/{south}", Value: false, Clock: Clock{Wall: 2, Actor: "c"}})

	spec := s.Document()["spec"].(map[string]any)
	krs := spec["keyResults"].([]any)
	if len(krs) != 3 || krs[0].(map[string]any)["id"] != "kr-1" || krs[1].(map[string]any)["id"] != "kr-3" || krs[2].(map[string]any)["target"] != 1 {
		t.Fatalf("keyed list did not merge by key and rank: %#v", krs)
	}
	if segs := spec["segments"].([]any); len(segs) != 1 || segs[0] != "north" {
		t.Fatalf("set removal: %#v", segs)
	}
}

func TestEscapesSlashesAndBraces(t *testing.T) {
	segs := Split("/spec/a~1b/{k~2v}/x")
	if len(segs) != 4 || segs[1] != "a/b" || segs[2] != "{k{v}" {
		t.Fatalf("got %#v", segs)
	}
}
