// Package conformance is the suite every adapter of the crdt port must
// pass. It holds an adapter to what the port promises: saves round-trip,
// a reconcile is minimal and idempotent, identified list items keep their
// identity, sentences merge character by character, concurrent writes are
// kept as conflicts, and any number of replicas converge however their
// sync messages are lost, repeated, reordered or partitioned.
//
// Every random choice comes from a seed, so a failure reproduces. Actors
// are random by design, so which concurrent write wins varies from run to
// run; the suite asserts only what holds whichever wins.
package conformance

import (
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/internal/crdt"
)

// Run exercises an adapter. newEngine returns a fresh engine, closed by
// the caller's cleanup.
func Run(t *testing.T, newEngine func(t *testing.T) crdt.Engine) {
	t.Helper()

	t.Run("save and load round-trip", func(t *testing.T) {
		e := newEngine(t)
		a := newDoc(t, e)
		reconcile(t, a, manifest(rand.New(rand.NewSource(1))))
		b := load(t, e, must(a.Save()))
		sameState(t, "loaded", a, b)

		f := must(a.Fork())
		t.Cleanup(func() { f.Close() })
		sameState(t, "forked", a, f)
		m := jsonOf(t, f)
		spec(m)["budget"] = 1.0
		reconcile(t, f, m)
		if spec(jsonOf(t, a))["budget"] == 1.0 {
			t.Fatal("an edit to a fork reached the original")
		}
	})

	t.Run("snapshot plus incremental chunks round-trip", func(t *testing.T) {
		e := newEngine(t)
		r := rand.New(rand.NewSource(2))
		a := newDoc(t, e)
		m := manifest(r)
		reconcile(t, a, m)
		saved := must(a.Save())
		if chunk := must(a.SaveIncremental()); chunk != nil {
			t.Fatalf("SaveIncremental right after Save = %d bytes, want nil", len(chunk))
		}
		var chunks [][]byte
		for range 3 {
			mutate(r, m, "a", 3)
			reconcile(t, a, m)
			chunks = append(chunks, must(a.SaveIncremental()))
		}
		b := load(t, e, slices.Concat(append([][]byte{saved}, chunks...)...))
		sameState(t, "snapshot and chunks", a, b)

		// A document loaded from a snapshot has nothing unsaved.
		if chunk := must(b.SaveIncremental()); chunk != nil {
			t.Fatalf("SaveIncremental right after Load = %d bytes, want nil", len(chunk))
		}
	})

	t.Run("load incremental is idempotent", func(t *testing.T) {
		e := newEngine(t)
		r := rand.New(rand.NewSource(3))
		a := newDoc(t, e)
		m := manifest(r)
		reconcile(t, a, m)
		b := load(t, e, must(a.Save()))
		mutate(r, m, "a", 4)
		reconcile(t, a, m)
		chunk := must(a.SaveIncremental())
		for range 3 {
			if err := b.LoadIncremental(chunk); err != nil {
				t.Fatal(err)
			}
			sameState(t, "after loading the chunk", a, b)
		}
		heads := must(b.Heads())
		if err := b.LoadIncremental(must(a.Save())); err != nil {
			t.Fatal(err)
		}
		if !must(b.Heads()).Equal(heads) {
			t.Fatal("loading a snapshot of changes already held changed the heads")
		}
	})

	t.Run("reconcile is idempotent and reads back", func(t *testing.T) {
		e := newEngine(t)
		for seed := int64(10); seed < 14; seed++ {
			r := rand.New(rand.NewSource(seed))
			d := newDoc(t, e)
			m := manifest(r)
			for step := range 12 {
				if !reconcile(t, d, m) && step == 0 {
					t.Fatalf("seed %d: the first reconcile reported no change", seed)
				}
				heads := must(d.Heads())
				if again := must(d.Reconcile(m, shape, crdt.Change{Message: "again"})); again {
					t.Fatalf("seed %d step %d: reconciling the same document again reported a change", seed, step)
				}
				if !must(d.Heads()).Equal(heads) {
					t.Fatalf("seed %d step %d: a reconcile with no change moved the heads", seed, step)
				}
				if got := jsonOf(t, d); !reflect.DeepEqual(got, m) {
					t.Fatalf("seed %d step %d: JSON() differs from what was reconciled\n got %v\nwant %v", seed, step, got, m)
				}
				mutate(r, m, "a", 1+r.Intn(4))
			}
		}
	})

	t.Run("a keyed item keeps its identity while others move", func(t *testing.T) {
		e := newEngine(t)
		a := newDoc(t, e)
		m := manifest(rand.New(rand.NewSource(4)))
		spec(m)["keyResults"] = []any{kr("k1", "fewer crates lost"), kr("k2", "cool within an hour"), kr("k3", "weekly routes"), kr("k4", "growers paid on time")}
		reconcile(t, a, m)
		b := load(t, e, must(a.Save()))

		// A edits inside k2.
		ma := jsonOf(t, a)
		k2 := item(ma, "k2")
		k2["target"] = 42.0
		k2["statement"] = "cool every crate within an hour"
		reconcile(t, a, ma)

		// B deletes k1, moves k4 to the front and inserts k5.
		mb := jsonOf(t, b)
		krs := spec(mb)["keyResults"].([]any)
		spec(mb)["keyResults"] = []any{krs[3], krs[1], krs[2], kr("k5", "a second packing line")}
		reconcile(t, b, mb)

		syncPair(t, e, a, b)
		sameState(t, "after sync", a, b)
		got := jsonOf(t, a)
		if ids := keys(got); !slices.Equal(ids, []string{"k4", "k2", "k3", "k5"}) {
			t.Fatalf("keyResults = %v, want [k4 k2 k3 k5]", ids)
		}
		if k2 := item(got, "k2"); k2["target"] != 42.0 || k2["statement"] != "cool every crate within an hour" {
			t.Fatalf("k2 lost A's edit: %v", k2)
		}
	})

	t.Run("a delete beats a concurrent edit inside the deleted item", func(t *testing.T) {
		e := newEngine(t)
		a := newDoc(t, e)
		m := manifest(rand.New(rand.NewSource(5)))
		spec(m)["keyResults"] = []any{kr("k1", "fewer crates lost"), kr("k2", "cool within an hour"), kr("k3", "weekly routes")}
		reconcile(t, a, m)
		b := load(t, e, must(a.Save()))

		ma := jsonOf(t, a)
		spec(ma)["keyResults"] = slices.DeleteFunc(spec(ma)["keyResults"].([]any), func(x any) bool { return x.(map[string]any)["id"] == "k2" })
		reconcile(t, a, ma)

		mb := jsonOf(t, b)
		k2 := item(mb, "k2")
		k2["target"] = 7.0
		k2["statement"] = "cool within half an hour"
		k2["owner"] = "logistics lead"
		reconcile(t, b, mb)

		syncPair(t, e, a, b)
		sameState(t, "after sync", a, b)
		got := jsonOf(t, a)
		if ids := keys(got); !slices.Equal(ids, []string{"k1", "k3"}) {
			t.Fatalf("keyResults = %v, want [k1 k3]: the edit inside k2 resurrected it", ids)
		}
		for _, x := range spec(got)["keyResults"].([]any) {
			if _, ok := x.(map[string]any)["owner"]; ok {
				t.Fatalf("a field of the deleted item landed on another: %v", x)
			}
		}
	})

	t.Run("concurrent splices of one text both survive", func(t *testing.T) {
		e := newEngine(t)
		a := newDoc(t, e)
		m := manifest(rand.New(rand.NewSource(6)))
		spec(m)["summary"] = "pack the harvest and deliver it"
		reconcile(t, a, m)
		b := load(t, e, must(a.Save()))

		ma := jsonOf(t, a)
		spec(ma)["summary"] = "pack the morning harvest and deliver it"
		reconcile(t, a, ma)
		mb := jsonOf(t, b)
		spec(mb)["summary"] = "pack the harvest and deliver it cold"
		reconcile(t, b, mb)

		syncPair(t, e, a, b)
		sameState(t, "after sync", a, b)
		if got := spec(jsonOf(t, a))["summary"]; got != "pack the morning harvest and deliver it cold" {
			t.Fatalf("summary = %q, want both insertions", got)
		}
		if c := must(a.Conflicts()); len(c) != 0 {
			t.Fatalf("a merged text reported conflicts: %v", c)
		}
	})

	t.Run("concurrent scalar writes converge and are kept as a conflict", func(t *testing.T) {
		e := newEngine(t)
		a := newDoc(t, e)
		reconcile(t, a, manifest(rand.New(rand.NewSource(7))))
		b := load(t, e, must(a.Save()))

		ma := jsonOf(t, a)
		spec(ma)["budget"] = 100.0
		reconcile(t, a, ma)
		mb := jsonOf(t, b)
		spec(mb)["budget"] = 200.0
		reconcile(t, b, mb)

		syncPair(t, e, a, b)
		sameState(t, "after sync", a, b)
		conflicts := must(a.Conflicts())
		if !reflect.DeepEqual(conflicts, must(b.Conflicts())) {
			t.Fatal("replicas report different conflicts")
		}
		if len(conflicts) != 1 || conflicts[0].Path != "/spec/budget" || len(conflicts[0].Values) != 2 {
			t.Fatalf("conflicts = %v, want /spec/budget with two values", conflicts)
		}
		vals := conflicts[0].Values
		if vals[0] != spec(jsonOf(t, a))["budget"] {
			t.Fatalf("the first value %v is not the one the document shows", vals[0])
		}
		if !(vals[0] == 100.0 && vals[1] == 200.0) && !(vals[0] == 200.0 && vals[1] == 100.0) {
			t.Fatalf("values = %v, want 100 and 200", vals)
		}

		// A later write resolves the conflict.
		spec(ma)["budget"] = 300.0
		reconcile(t, a, ma)
		if c := must(a.Conflicts()); len(c) != 0 {
			t.Fatalf("a later write left conflicts: %v", c)
		}
	})

	t.Run("generate reports nothing to send once peers are in sync", func(t *testing.T) {
		e := newEngine(t)
		a := newDoc(t, e)
		b := newDoc(t, e)
		reconcile(t, a, manifest(rand.New(rand.NewSource(8))))
		sa, sb := syncState(t, e), syncState(t, e)
		for range 10 {
			ma, okA := must2(a.GenerateSyncMessage(sa))
			if okA {
				mustDo(t, b.ReceiveSyncMessage(sb, ma))
			}
			mb, okB := must2(b.GenerateSyncMessage(sb))
			if okB {
				mustDo(t, a.ReceiveSyncMessage(sa, mb))
			}
			if !okA && !okB {
				sameState(t, "quiescent", a, b)
				return
			}
		}
		t.Fatal("peers still had messages to send after ten rounds")
	})

	t.Run("used after close", func(t *testing.T) {
		e := newEngine(t)
		d := must(e.New())
		mustDo(t, d.Close())
		if _, err := d.JSON(); !errors.Is(err, crdt.ErrClosed) {
			t.Fatalf("JSON after Close: %v, want ErrClosed", err)
		}
		s := syncState(t, e)
		mustDo(t, s.Close())
		a := newDoc(t, e)
		if _, _, err := a.GenerateSyncMessage(s); !errors.Is(err, crdt.ErrClosed) {
			t.Fatalf("GenerateSyncMessage with a closed sync state: %v, want ErrClosed", err)
		}
	})

	for _, n := range []int{3, 4, 5} {
		t.Run(fmt.Sprintf("%d replicas converge across lossy partitions", n), func(t *testing.T) {
			e := newEngine(t)
			net := newNetwork(t, e, int64(100+n), n)
			for round := range 16 {
				if round%4 == 0 {
					net.partition()
				}
				for i := range n {
					if net.r.Intn(3) > 0 {
						net.edit(i, 1+net.r.Intn(3))
					}
				}
				net.exchange(func(i, j int) bool { return net.group[i] == net.group[j] })
			}
			net.heal()
			net.converged()
		})
	}

	t.Run("an offline replica converges after many changes elsewhere", func(t *testing.T) {
		e := newEngine(t)
		net := newNetwork(t, e, 200, 3)
		const offline = 2
		online := func(i, j int) bool { return i != offline && j != offline }
		for round := range 30 {
			net.edit(round%2, 2)
			if round%3 == 0 {
				net.edit(offline, 1)
			}
			net.exchange(online)
		}
		m := jsonOf(t, net.docs[offline])
		m["metadata"].(map[string]any)["labels"].(map[string]any)["offline"] = "edited"
		reconcile(t, net.docs[offline], m)
		net.heal()
		net.converged()
		labels := jsonOf(t, net.docs[0])["metadata"].(map[string]any)["labels"].(map[string]any)
		if labels["offline"] != "edited" {
			t.Fatalf("the offline replica's edit was lost: labels = %v", labels)
		}
	})
}

// shape is how manifest() maps onto the CRDT: two levels of identified
// lists and sentences at three depths, as a kind's schema would give.
var shape = crdt.Shape{
	ListKeys: map[string]string{
		"/spec/keyResults":            "id",
		"/spec/phases":                "id",
		"/spec/phases/*/deliverables": "id",
	},
	Texts: []string{
		"/spec/summary",
		"/spec/keyResults/*/statement",
		"/spec/phases/*/deliverables/*/title",
	},
}

var words = []string{"pack", "the", "harvest", "cool", "crates", "deliver", "weekly", "routes", "growers", "orchard", "store", "fresh", "early", "line"}

func sentence(r *rand.Rand) string {
	n := 3 + r.Intn(5)
	w := make([]string, n)
	for i := range w {
		w[i] = words[r.Intn(len(words))]
	}
	return strings.Join(w, " ")
}

func kr(id, statement string) map[string]any {
	return map[string]any{"id": id, "statement": statement, "target": 10.0, "unit": "percent"}
}

// manifest is a document shaped like a real one: nested objects, keyed
// lists inside keyed lists, an unkeyed list, texts, integers, a float,
// booleans and a null.
func manifest(r *rand.Rand) map[string]any {
	var krs, phases []any
	for i := range 3 + r.Intn(3) {
		krs = append(krs, map[string]any{"id": fmt.Sprintf("kr-%d", i), "statement": sentence(r), "target": float64(r.Intn(100)), "unit": "percent"})
	}
	for i := range 2 + r.Intn(2) {
		var ds []any
		for j := range 1 + r.Intn(3) {
			ds = append(ds, map[string]any{"id": fmt.Sprintf("d-%d-%d", i, j), "title": sentence(r), "done": r.Intn(2) == 0})
		}
		phases = append(phases, map[string]any{"id": fmt.Sprintf("ph-%d", i), "name": fmt.Sprintf("Phase %d", i+1), "deliverables": ds})
	}
	return map[string]any{
		"apiVersion": "cartograph/v1",
		"kind":       "Project",
		"metadata": map[string]any{
			"id":     "prj-7f3a",
			"name":   "Cold chain",
			"labels": map[string]any{"area": "logistics"},
		},
		"spec": map[string]any{
			"summary":    sentence(r),
			"budget":     float64(r.Intn(100000)),
			"share":      0.25,
			"active":     true,
			"parent":     nil,
			"keyResults": krs,
			"phases":     phases,
			"tags":       []any{"cold", "chain"},
			"window":     map[string]any{"start": "2026-01-01", "end": "2026-12-31"},
		},
	}
}

// mutate makes n random edits to m in place, as a person editing the
// manifest would; tag keeps new ids unique to the replica.
func mutate(r *rand.Rand, m map[string]any, tag string, n int) {
	s := spec(m)
	for range n {
		krs := s["keyResults"].([]any)
		phases := s["phases"].([]any)
		switch r.Intn(13) {
		case 0:
			s["budget"] = float64(r.Intn(100000))
		case 1:
			s["summary"] = editText(r, s["summary"].(string))
		case 2:
			if len(krs) > 0 {
				krs[r.Intn(len(krs))].(map[string]any)["target"] = float64(r.Intn(100))
			}
		case 3:
			if len(krs) > 0 {
				k := krs[r.Intn(len(krs))].(map[string]any)
				k["statement"] = editText(r, k["statement"].(string))
			}
		case 4:
			id := fmt.Sprintf("kr-%s-%d", tag, r.Int63())
			s["keyResults"] = slices.Insert(krs, r.Intn(len(krs)+1), any(map[string]any{"id": id, "statement": sentence(r), "target": 1.0, "unit": "percent"}))
		case 5:
			if len(krs) > 1 {
				i := r.Intn(len(krs))
				s["keyResults"] = slices.Delete(slices.Clone(krs), i, i+1)
			}
		case 6:
			if len(krs) > 1 {
				i := r.Intn(len(krs))
				x := krs[i]
				rest := slices.Delete(slices.Clone(krs), i, i+1)
				s["keyResults"] = slices.Insert(rest, r.Intn(len(rest)+1), x)
			}
		case 7:
			if len(phases) > 0 {
				p := phases[r.Intn(len(phases))].(map[string]any)
				ds := p["deliverables"].([]any)
				switch {
				case len(ds) > 0 && r.Intn(3) == 0:
					i := r.Intn(len(ds))
					p["deliverables"] = slices.Delete(slices.Clone(ds), i, i+1)
				case len(ds) > 0 && r.Intn(2) == 0:
					d := ds[r.Intn(len(ds))].(map[string]any)
					d["title"] = editText(r, d["title"].(string))
					d["done"] = !d["done"].(bool)
				default:
					id := fmt.Sprintf("d-%s-%d", tag, r.Int63())
					p["deliverables"] = slices.Insert(ds, r.Intn(len(ds)+1), any(map[string]any{"id": id, "title": sentence(r), "done": false}))
				}
			}
		case 8:
			tags := s["tags"].([]any)
			if len(tags) > 0 && r.Intn(2) == 0 {
				s["tags"] = tags[:len(tags)-1]
			} else {
				s["tags"] = append(slices.Clone(tags), words[r.Intn(len(words))])
			}
		case 9:
			labels := m["metadata"].(map[string]any)["labels"].(map[string]any)
			k := []string{"area", "season", "lead"}[r.Intn(3)]
			if r.Intn(3) == 0 {
				delete(labels, k)
			} else {
				labels[k] = words[r.Intn(len(words))]
			}
		case 10:
			if s["parent"] == nil {
				s["parent"] = map[string]any{"kind": "Programme", "id": fmt.Sprintf("prg-%d", r.Intn(9))}
			} else {
				s["parent"] = nil
			}
		case 11:
			s["share"] = r.Float64()
		case 12:
			s["active"] = !s["active"].(bool)
		}
	}
}

// editText inserts or deletes a word, as typing would.
func editText(r *rand.Rand, s string) string {
	w := strings.Fields(s)
	if len(w) > 1 && r.Intn(3) == 0 {
		i := r.Intn(len(w))
		w = slices.Delete(w, i, i+1)
	} else {
		w = slices.Insert(w, r.Intn(len(w)+1), words[r.Intn(len(words))])
	}
	return strings.Join(w, " ")
}

// network is n replicas of one document and the links between them. A
// message is lost, repeated or held back at random, and replicas in
// different groups of a partition do not hear each other.
type network struct {
	t       *testing.T
	e       crdt.Engine
	r       *rand.Rand
	docs    []crdt.Doc
	states  [][]crdt.SyncState // states[i][j]: what i knows of peer j
	group   []int
	pending []message
}

type message struct {
	from, to int
	data     []byte
}

func newNetwork(t *testing.T, e crdt.Engine, seed int64, n int) *network {
	net := &network{t: t, e: e, r: rand.New(rand.NewSource(seed)), group: make([]int, n)}
	first := newDoc(t, e)
	reconcile(t, first, manifest(net.r))
	saved := must(first.Save())
	net.docs = append(net.docs, first)
	for range n - 1 {
		net.docs = append(net.docs, load(t, e, saved))
	}
	net.states = make([][]crdt.SyncState, n)
	for i := range n {
		net.states[i] = make([]crdt.SyncState, n)
		for j := range n {
			if i != j {
				net.states[i][j] = syncState(t, e)
			}
		}
	}
	return net
}

func (n *network) edit(i, edits int) {
	m := jsonOf(n.t, n.docs[i])
	mutate(n.r, m, fmt.Sprint(i), edits)
	reconcile(n.t, n.docs[i], m)
}

// partition splits the replicas into up to three groups.
func (n *network) partition() {
	groups := 1 + n.r.Intn(3)
	for i := range n.group {
		n.group[i] = n.r.Intn(groups)
	}
}

// exchange runs one round over unreliable links: every replica sends
// what it has for every peer it can reach, and each message in flight is
// then delivered, held for a later round, or lost when the link is down.
func (n *network) exchange(linked func(i, j int) bool) {
	for i := range n.docs {
		for j := range n.docs {
			if i == j || !linked(i, j) {
				continue
			}
			msg, ok := must2(n.docs[i].GenerateSyncMessage(n.states[i][j]))
			if !ok {
				continue
			}
			switch p := n.r.Intn(10); {
			case p < 2: // lost
			case p < 3: // repeated
				n.pending = append(n.pending, message{i, j, msg}, message{i, j, msg})
			default:
				n.pending = append(n.pending, message{i, j, msg})
			}
		}
	}
	n.r.Shuffle(len(n.pending), func(a, b int) { n.pending[a], n.pending[b] = n.pending[b], n.pending[a] })
	var held []message
	for _, m := range n.pending {
		switch {
		case !linked(m.from, m.to):
		case n.r.Intn(4) == 0:
			held = append(held, m)
		default:
			mustDo(n.t, n.docs[m.to].ReceiveSyncMessage(n.states[m.to][m.from], m.data))
		}
	}
	n.pending = held
}

// heal reconnects everyone: messages in flight are gone, and every
// connection starts again from a new sync state, as a reconnecting peer
// does. Then replicas sync pairwise until a full pass sends nothing.
func (n *network) heal() {
	n.pending = nil
	for i := range n.docs {
		for j := range n.docs {
			if i != j {
				mustDo(n.t, n.states[i][j].Close())
				n.states[i][j] = syncState(n.t, n.e)
			}
		}
	}
	for range len(n.docs) + 2 {
		sent := false
		for i := range n.docs {
			for j := i + 1; j < len(n.docs); j++ {
				if exchangeAll(n.t, n.docs[i], n.docs[j], n.states[i][j], n.states[j][i]) {
					sent = true
				}
			}
		}
		if !sent {
			return
		}
	}
	n.t.Fatal("replicas still had messages to send after healing")
}

func (n *network) converged() {
	n.t.Helper()
	for i := 1; i < len(n.docs); i++ {
		sameState(n.t, fmt.Sprintf("replica %d", i), n.docs[0], n.docs[i])
		if a, b := must(n.docs[0].Conflicts()), must(n.docs[i].Conflicts()); !reflect.DeepEqual(a, b) {
			n.t.Fatalf("replica %d reports different conflicts:\n%v\n%v", i, a, b)
		}
	}
}

// syncPair syncs two documents over a reliable link with fresh states.
func syncPair(t *testing.T, e crdt.Engine, a, b crdt.Doc) {
	t.Helper()
	exchangeAll(t, a, b, syncState(t, e), syncState(t, e))
	if exchangeAll(t, a, b, syncState(t, e), syncState(t, e)) && !must(a.Heads()).Equal(must(b.Heads())) {
		t.Fatal("a second sync did not converge")
	}
}

// exchangeAll runs the protocol until neither side has anything to send
// and reports whether any message was sent.
func exchangeAll(t *testing.T, a, b crdt.Doc, sa, sb crdt.SyncState) bool {
	t.Helper()
	sent := false
	for range 50 {
		ma, okA := must2(a.GenerateSyncMessage(sa))
		if okA {
			mustDo(t, b.ReceiveSyncMessage(sb, ma))
		}
		mb, okB := must2(b.GenerateSyncMessage(sb))
		if okB {
			mustDo(t, a.ReceiveSyncMessage(sa, mb))
		}
		if !okA && !okB {
			return sent
		}
		sent = true
	}
	t.Fatal("no quiescence after fifty rounds")
	return sent
}

func sameState(t *testing.T, what string, a, b crdt.Doc) {
	t.Helper()
	if ha, hb := must(a.Heads()), must(b.Heads()); !ha.Equal(hb) {
		t.Fatalf("%s: heads differ: %v and %v", what, ha, hb)
	}
	if ja, jb := jsonOf(t, a), jsonOf(t, b); !reflect.DeepEqual(ja, jb) {
		t.Fatalf("%s: JSON differs:\n%v\n%v", what, ja, jb)
	}
}

func newDoc(t *testing.T, e crdt.Engine) crdt.Doc {
	t.Helper()
	d := must(e.New())
	t.Cleanup(func() { d.Close() })
	return d
}

func load(t *testing.T, e crdt.Engine, data []byte) crdt.Doc {
	t.Helper()
	d, err := e.Load(data)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func syncState(t *testing.T, e crdt.Engine) crdt.SyncState {
	t.Helper()
	s := must(e.NewSyncState())
	t.Cleanup(func() { s.Close() })
	return s
}

func reconcile(t *testing.T, d crdt.Doc, m map[string]any) bool {
	t.Helper()
	changed, err := d.Reconcile(m, shape, crdt.Change{Message: "conformance", Time: 1790000000000})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	return changed
}

func jsonOf(t *testing.T, d crdt.Doc) map[string]any {
	t.Helper()
	m, err := d.JSON()
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	return m
}

func spec(m map[string]any) map[string]any { return m["spec"].(map[string]any) }

func item(m map[string]any, id string) map[string]any {
	for _, x := range spec(m)["keyResults"].([]any) {
		if x.(map[string]any)["id"] == id {
			return x.(map[string]any)
		}
	}
	return nil
}

func keys(m map[string]any) []string {
	var ids []string
	for _, x := range spec(m)["keyResults"].([]any) {
		ids = append(ids, x.(map[string]any)["id"].(string))
	}
	return ids
}

// must and its kin turn an unexpected error into a panic, which the test
// runner reports with the stack; they keep the scenarios readable.
func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func must2[T any](v T, ok bool, err error) (T, bool) {
	if err != nil {
		panic(err)
	}
	return v, ok
}

func mustDo(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
