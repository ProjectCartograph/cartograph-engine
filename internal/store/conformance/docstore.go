package conformance

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

// RunDocStore exercises every store.DocStore method against a freshly
// constructed adapter, including the races two replicas run into:
// creating the same manifest's document at once, appending to one
// document at once, and appending while it is compacted.
func RunDocStore(t *testing.T, newStore func(t *testing.T) store.DocStore) {
	t.Helper()
	ctx := context.Background()

	t.Run("create, then look up both ways", func(t *testing.T) {
		s := newStore(t)
		existing, created, err := s.Create(ctx, "Goal", "g1", "doc-1", []byte("snap"))
		must(t, err)
		if !created || existing != "doc-1" {
			t.Fatalf("first create: got %q created=%v", existing, created)
		}
		docID, err := s.DocumentFor(ctx, "Goal", "g1")
		must(t, err)
		if docID != "doc-1" {
			t.Fatalf("document for Goal/g1: %q", docID)
		}
		kind, id, err := s.ManifestFor(ctx, "doc-1")
		must(t, err)
		if kind != "Goal" || id != "g1" {
			t.Fatalf("manifest for doc-1: %s/%s", kind, id)
		}
		// The same id under another kind is another manifest.
		_, created, err = s.Create(ctx, "Project", "g1", "doc-2", []byte("other"))
		must(t, err)
		if !created {
			t.Fatal("Project/g1 is not Goal/g1")
		}
	})

	t.Run("a second create returns the first document", func(t *testing.T) {
		s := newStore(t)
		_, _, err := s.Create(ctx, "Goal", "g1", "doc-1", []byte("first"))
		must(t, err)
		existing, created, err := s.Create(ctx, "Goal", "g1", "doc-2", []byte("second"))
		must(t, err)
		if created || existing != "doc-1" {
			t.Fatalf("got %q created=%v, want doc-1 not created", existing, created)
		}
		snap, _, err := s.Load(ctx, "doc-1")
		must(t, err)
		if string(snap) != "first" {
			t.Fatalf("the loser's snapshot replaced the winner's: %q", snap)
		}
		if _, _, err := s.Load(ctx, "doc-2"); !errors.Is(err, store.ErrNoDocument) {
			t.Fatalf("the loser's document should not exist, got %v", err)
		}
	})

	t.Run("create is atomic under concurrency", func(t *testing.T) {
		s := newStore(t)
		const n = 8
		type result struct {
			existing string
			created  bool
			err      error
		}
		results := make([]result, n)
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := range n {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				e, c, err := s.Create(ctx, "Goal", "g1", fmt.Sprintf("doc-%d", i), fmt.Appendf(nil, "snap-%d", i))
				results[i] = result{e, c, err}
			}(i)
		}
		close(start)
		wg.Wait()
		winners := 0
		var winner string
		for i, r := range results {
			must(t, r.err)
			if r.created {
				winners++
				winner = fmt.Sprintf("doc-%d", i)
				if r.existing != winner {
					t.Fatalf("a created document reports %q, want its own %q", r.existing, winner)
				}
			}
		}
		if winners != 1 {
			t.Fatalf("%d creates won, want exactly 1", winners)
		}
		for _, r := range results {
			if r.existing != winner {
				t.Fatalf("a loser got %q, want the winner %q", r.existing, winner)
			}
		}
		docID, err := s.DocumentFor(ctx, "Goal", "g1")
		must(t, err)
		if docID != winner {
			t.Fatalf("document for Goal/g1 is %q, want %q", docID, winner)
		}
	})

	t.Run("unknown documents and manifests", func(t *testing.T) {
		s := newStore(t)
		if _, err := s.DocumentFor(ctx, "Goal", "none"); !errors.Is(err, store.ErrNoDocument) {
			t.Errorf("DocumentFor: %v", err)
		}
		if _, _, err := s.ManifestFor(ctx, "none"); !errors.Is(err, store.ErrNoDocument) {
			t.Errorf("ManifestFor: %v", err)
		}
		if _, err := s.Append(ctx, "none", []byte("x")); !errors.Is(err, store.ErrNoDocument) {
			t.Errorf("Append: %v", err)
		}
		if _, _, err := s.Load(ctx, "none"); !errors.Is(err, store.ErrNoDocument) {
			t.Errorf("Load: %v", err)
		}
		if _, err := s.Since(ctx, "none", 0); !errors.Is(err, store.ErrNoDocument) {
			t.Errorf("Since: %v", err)
		}
		if err := s.Compact(ctx, "none", []byte("x"), 1); !errors.Is(err, store.ErrNoDocument) {
			t.Errorf("Compact: %v", err)
		}
	})

	t.Run("append, load and since", func(t *testing.T) {
		s := newStore(t)
		_, _, err := s.Create(ctx, "Goal", "g1", "doc-1", []byte("snap"))
		must(t, err)
		snap, chunks, err := s.Load(ctx, "doc-1")
		must(t, err)
		if string(snap) != "snap" || len(chunks) != 0 {
			t.Fatalf("fresh document: %q, %d chunks", snap, len(chunks))
		}
		var seqs []int64
		for _, data := range []string{"a", "b", "c"} {
			seq, err := s.Append(ctx, "doc-1", []byte(data))
			must(t, err)
			seqs = append(seqs, seq)
		}
		if !(seqs[0] < seqs[1] && seqs[1] < seqs[2]) {
			t.Fatalf("sequence numbers do not rise: %v", seqs)
		}
		snap, chunks, err = s.Load(ctx, "doc-1")
		must(t, err)
		if string(snap) != "snap" || !sameChunks(chunks, seqs, "a", "b", "c") {
			t.Fatalf("load: %q, %+v", snap, chunks)
		}
		got, err := s.Since(ctx, "doc-1", seqs[0])
		must(t, err)
		if !sameChunks(got, seqs[1:], "b", "c") {
			t.Fatalf("since the first: %+v", got)
		}
		got, err = s.Since(ctx, "doc-1", seqs[2])
		must(t, err)
		if got == nil || len(got) != 0 {
			t.Fatalf("since the last is an empty, non-nil slice: %#v", got)
		}
		// Bytes are bytes: nothing is lost to an encoding.
		binary := []byte{0, 0xff, '\n', 0x80, 0}
		seq, err := s.Append(ctx, "doc-1", binary)
		must(t, err)
		got, err = s.Since(ctx, "doc-1", seqs[2])
		must(t, err)
		if len(got) != 1 || got[0].Seq != seq || !bytes.Equal(got[0].Data, binary) {
			t.Fatalf("binary chunk: %+v", got)
		}
	})

	t.Run("concurrent appends get distinct rising numbers", func(t *testing.T) {
		s := newStore(t)
		_, _, err := s.Create(ctx, "Goal", "g1", "doc-1", nil)
		must(t, err)
		const writers, each = 4, 25
		var wg sync.WaitGroup
		errs := make(chan error, writers)
		for w := range writers {
			wg.Add(1)
			go func(w int) {
				defer wg.Done()
				last := int64(0)
				for i := range each {
					seq, err := s.Append(ctx, "doc-1", fmt.Appendf(nil, "%d-%d", w, i))
					if err != nil {
						errs <- err
						return
					}
					if seq <= last {
						errs <- fmt.Errorf("writer %d: seq %d after %d", w, seq, last)
						return
					}
					last = seq
				}
			}(w)
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatal(err)
		}
		_, chunks, err := s.Load(ctx, "doc-1")
		must(t, err)
		if len(chunks) != writers*each {
			t.Fatalf("%d chunks, want %d", len(chunks), writers*each)
		}
		seen := map[string]bool{}
		for i, c := range chunks {
			if i > 0 && c.Seq <= chunks[i-1].Seq {
				t.Fatalf("load is not oldest first at %d: %d after %d", i, c.Seq, chunks[i-1].Seq)
			}
			seen[string(c.Data)] = true
		}
		if len(seen) != writers*each {
			t.Fatalf("%d distinct chunks, want %d", len(seen), writers*each)
		}
	})

	t.Run("compact keeps what came after", func(t *testing.T) {
		s := newStore(t)
		_, _, err := s.Create(ctx, "Goal", "g1", "doc-1", []byte("s0"))
		must(t, err)
		_, err = s.Append(ctx, "doc-1", []byte("a"))
		must(t, err)
		b, err := s.Append(ctx, "doc-1", []byte("b"))
		must(t, err)
		c, err := s.Append(ctx, "doc-1", []byte("c"))
		must(t, err)
		must(t, s.Compact(ctx, "doc-1", []byte("s0+a+b"), b))
		snap, chunks, err := s.Load(ctx, "doc-1")
		must(t, err)
		if string(snap) != "s0+a+b" || !sameChunks(chunks, []int64{c}, "c") {
			t.Fatalf("after compact: %q, %+v", snap, chunks)
		}
		d, err := s.Append(ctx, "doc-1", []byte("d"))
		must(t, err)
		if d <= c {
			t.Fatalf("numbering goes back after compaction: %d after %d", d, c)
		}
		got, err := s.Since(ctx, "doc-1", 0)
		must(t, err)
		if !sameChunks(got, []int64{c, d}, "c", "d") {
			t.Fatalf("since 0 after compact: %+v", got)
		}
	})

	t.Run("an older compaction does not undo a newer one", func(t *testing.T) {
		s := newStore(t)
		_, _, err := s.Create(ctx, "Goal", "g1", "doc-1", []byte("s0"))
		must(t, err)
		a, err := s.Append(ctx, "doc-1", []byte("a"))
		must(t, err)
		b, err := s.Append(ctx, "doc-1", []byte("b"))
		must(t, err)
		must(t, s.Compact(ctx, "doc-1", []byte("s0+a+b"), b))
		must(t, s.Compact(ctx, "doc-1", []byte("s0+a"), a))
		snap, chunks, err := s.Load(ctx, "doc-1")
		must(t, err)
		if string(snap) != "s0+a+b" || len(chunks) != 0 {
			t.Fatalf("a stale compaction won: %q, %+v", snap, chunks)
		}
	})

	t.Run("compact during concurrent appends loses nothing", func(t *testing.T) {
		s := newStore(t)
		_, _, err := s.Create(ctx, "Goal", "g1", "doc-1", nil)
		must(t, err)
		var seqs []int64
		for i := range 10 {
			seq, err := s.Append(ctx, "doc-1", fmt.Appendf(nil, "before-%d", i))
			must(t, err)
			seqs = append(seqs, seq)
		}
		upTo := seqs[len(seqs)-1]

		var wg sync.WaitGroup
		var mu sync.Mutex
		var after []int64
		errs := make(chan error, 3)
		for w := range 2 {
			wg.Add(1)
			go func(w int) {
				defer wg.Done()
				for i := range 20 {
					seq, err := s.Append(ctx, "doc-1", fmt.Appendf(nil, "after-%d-%d", w, i))
					if err != nil {
						errs <- err
						return
					}
					mu.Lock()
					after = append(after, seq)
					mu.Unlock()
				}
			}(w)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.Compact(ctx, "doc-1", []byte("compacted"), upTo); err != nil {
				errs <- err
			}
		}()
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatal(err)
		}

		snap, chunks, err := s.Load(ctx, "doc-1")
		must(t, err)
		if string(snap) != "compacted" {
			t.Fatalf("snapshot %q", snap)
		}
		sort.Slice(after, func(i, j int) bool { return after[i] < after[j] })
		if len(chunks) != len(after) {
			t.Fatalf("%d chunks kept, want the %d appended after upTo", len(chunks), len(after))
		}
		for i, c := range chunks {
			if c.Seq != after[i] || c.Seq <= upTo {
				t.Fatalf("chunk %d: seq %d, want %d (> %d)", i, c.Seq, after[i], upTo)
			}
		}
	})
}

func sameChunks(got []store.Chunk, seqs []int64, data ...string) bool {
	if len(got) != len(seqs) || len(got) != len(data) {
		return false
	}
	for i := range got {
		if got[i].Seq != seqs[i] || string(got[i].Data) != data[i] {
			return false
		}
	}
	return true
}
