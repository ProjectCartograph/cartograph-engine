// Package conformance is the one test suite every store adapter must
// pass, proving the SQLite, in-memory and Postgres adapters are
// interchangeable behind the store ports.
package conformance

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/internal/store"
)

// RunManifestStore exercises every store.ManifestStore method against a
// freshly constructed adapter.
func RunManifestStore(t *testing.T, newStore func(t *testing.T) store.ManifestStore) {
	t.Helper()
	ctx := context.Background()

	t.Run("put and get current", func(t *testing.T) {
		s := newStore(t)
		_, found, err := s.GetCurrent(ctx, "Team", "does-not-exist")
		must(t, err)
		if found {
			t.Fatal("expected not found")
		}

		v1 := store.Version{Kind: "Team", ID: "t1", Number: 1, YAML: []byte("metadata:\n  id: t1\n  name: Team One\n"), Actor: "a1", Reason: "seed", On: time.Now().UTC()}
		must(t, s.PutVersion(ctx, v1))

		cur, found, err := s.GetCurrent(ctx, "Team", "t1")
		must(t, err)
		if !found {
			t.Fatal("expected found")
		}
		if cur.Number != 1 || string(cur.YAML) != string(v1.YAML) {
			t.Fatalf("got %+v", cur)
		}

		v2 := v1
		v2.Number = 2
		v2.YAML = []byte("metadata:\n  id: t1\n  name: Team One Renamed\n")
		must(t, s.PutVersion(ctx, v2))

		cur, found, err = s.GetCurrent(ctx, "Team", "t1")
		must(t, err)
		if !found || cur.Number != 2 {
			t.Fatalf("expected version 2 current, got %+v", cur)
		}
	})

	t.Run("put version rejects a wrong number", func(t *testing.T) {
		s := newStore(t)
		v := store.Version{Kind: "Team", ID: "t1", Number: 5, YAML: []byte("metadata:\n  id: t1\n  name: X\n")}
		if err := s.PutVersion(ctx, v); err == nil {
			t.Fatal("expected an error for a non-sequential version number")
		}
	})

	t.Run("get version and list versions", func(t *testing.T) {
		s := newStore(t)
		must(t, s.PutVersion(ctx, store.Version{Kind: "Team", ID: "t1", Number: 1, YAML: []byte("metadata:\n  id: t1\n  name: One\n")}))
		must(t, s.PutVersion(ctx, store.Version{Kind: "Team", ID: "t1", Number: 2, YAML: []byte("metadata:\n  id: t1\n  name: Two\n")}))

		v1, found, err := s.GetVersion(ctx, "Team", "t1", 1)
		must(t, err)
		if !found || v1.Number != 1 {
			t.Fatalf("got %+v", v1)
		}

		_, found, err = s.GetVersion(ctx, "Team", "t1", 99)
		must(t, err)
		if found {
			t.Fatal("expected not found for a version that does not exist")
		}

		vs, err := s.ListVersions(ctx, "Team", "t1")
		must(t, err)
		if len(vs) != 2 || vs[0].Number != 1 || vs[1].Number != 2 {
			t.Fatalf("got %+v", vs)
		}
	})

	t.Run("list summaries filters by query and always returns a slice", func(t *testing.T) {
		s := newStore(t)
		out, err := s.ListSummaries(ctx, "Team", "", nil)
		must(t, err)
		if out == nil || len(out) != 0 {
			t.Fatalf("expected an empty, non-nil slice, got %#v", out)
		}

		must(t, s.PutVersion(ctx, store.Version{Kind: "Team", ID: "curriculum", Number: 1, YAML: []byte("metadata:\n  id: curriculum\n  name: Curriculum team\n")}))
		must(t, s.PutVersion(ctx, store.Version{Kind: "Team", ID: "operations", Number: 1, YAML: []byte("metadata:\n  id: operations\n  name: Operations team\n")}))

		out, err = s.ListSummaries(ctx, "Team", "", nil)
		must(t, err)
		if len(out) != 2 {
			t.Fatalf("expected 2 summaries, got %d", len(out))
		}

		out, err = s.ListSummaries(ctx, "Team", "curric", nil)
		must(t, err)
		if len(out) != 1 || out[0].ID != "curriculum" {
			t.Fatalf("query filter failed: %+v", out)
		}

		out, err = s.ListSummaries(ctx, "Team", "TEAM", nil)
		must(t, err)
		if len(out) != 2 {
			t.Fatalf("expected case-insensitive match on name, got %+v", out)
		}
	})

	t.Run("list ids and counts", func(t *testing.T) {
		s := newStore(t)
		must(t, s.PutVersion(ctx, store.Version{Kind: "Team", ID: "a", Number: 1, YAML: []byte("metadata:\n  id: a\n  name: A\n")}))
		must(t, s.PutVersion(ctx, store.Version{Kind: "Team", ID: "b", Number: 1, YAML: []byte("metadata:\n  id: b\n  name: B\n")}))
		must(t, s.PutVersion(ctx, store.Version{Kind: "Goal", ID: "g1", Number: 1, YAML: []byte("metadata:\n  id: g1\n  name: G\n")}))

		ids, err := s.ListIDs(ctx, "Team")
		must(t, err)
		if len(ids) != 2 {
			t.Fatalf("got %v", ids)
		}

		counts, err := s.Counts(ctx)
		must(t, err)
		if counts["Team"] != 2 || counts["Goal"] != 1 {
			t.Fatalf("got %v", counts)
		}
	})

	t.Run("index and list references", func(t *testing.T) {
		s := newStore(t)
		must(t, s.PutVersion(ctx, store.Version{Kind: "Team", ID: "t1", Number: 1, YAML: []byte("metadata:\n  id: t1\n  name: T1\n")}))
		must(t, s.PutVersion(ctx, store.Version{Kind: "Team", ID: "p1", Number: 1, YAML: []byte("metadata:\n  id: p1\n  name: P1\n")}))
		must(t, s.IndexReferences(ctx, "Team", "p1", []store.Ref{{Path: "/spec/team", ToKind: "Team", ToID: "t1"}}))

		out, err := s.ListReferencedBy(ctx, "Team", "p1")
		must(t, err)
		if len(out) != 1 || out[0].ToKind != "Team" || out[0].ToID != "t1" {
			t.Fatalf("got %+v", out)
		}

		in, err := s.ListReferencing(ctx, "Team", "t1")
		must(t, err)
		if len(in) != 1 || in[0].Kind != "Team" || in[0].ID != "p1" {
			t.Fatalf("got %+v", in)
		}

		// Re-indexing replaces the previous set.
		must(t, s.IndexReferences(ctx, "Team", "p1", nil))
		out, err = s.ListReferencedBy(ctx, "Team", "p1")
		must(t, err)
		if len(out) != 0 {
			t.Fatalf("expected references cleared, got %+v", out)
		}
		in, err = s.ListReferencing(ctx, "Team", "t1")
		must(t, err)
		if len(in) != 0 {
			t.Fatalf("expected no more incoming references, got %+v", in)
		}
	})

	t.Run("list summaries with ref filters", func(t *testing.T) {
		s := newStore(t)
		must(t, s.PutVersion(ctx, store.Version{Kind: "Goal", ID: "g1", Number: 1, YAML: []byte("metadata:\n  id: g1\n  name: G1\n")}))
		must(t, s.PutVersion(ctx, store.Version{Kind: "KPI", ID: "k1", Number: 1, YAML: []byte("metadata:\n  id: k1\n  name: K1\n")}))
		must(t, s.PutVersion(ctx, store.Version{Kind: "KPI", ID: "k2", Number: 1, YAML: []byte("metadata:\n  id: k2\n  name: K2\n")}))
		must(t, s.IndexReferences(ctx, "KPI", "k1", []store.Ref{{Path: "/spec/goals/0", ToKind: "Goal", ToID: "g1"}}))

		out, err := s.ListSummaries(ctx, "KPI", "", []store.RefFilter{{Kind: "Goal", ID: "g1"}})
		must(t, err)
		if len(out) != 1 || out[0].ID != "k1" {
			t.Fatalf("expected only k1, got %+v", out)
		}

		out, err = s.ListSummaries(ctx, "KPI", "", []store.RefFilter{{Kind: "Goal", ID: "does-not-exist"}})
		must(t, err)
		if len(out) != 0 {
			t.Fatalf("expected no matches, got %+v", out)
		}
	})

	t.Run("transaction commits all writes together", func(t *testing.T) {
		s := newStore(t)
		err := s.WithinTransaction(ctx, func(ctx context.Context, tx store.ManifestStore) error {
			if err := tx.PutVersion(ctx, store.Version{Kind: "Team", ID: "a", Number: 1, YAML: []byte("metadata:\n  id: a\n  name: A\n")}); err != nil {
				return err
			}
			return tx.PutVersion(ctx, store.Version{Kind: "Team", ID: "b", Number: 1, YAML: []byte("metadata:\n  id: b\n  name: B\n")})
		})
		must(t, err)
		ids, err := s.ListIDs(ctx, "Team")
		must(t, err)
		if len(ids) != 2 {
			t.Fatalf("expected both writes committed, got %v", ids)
		}
	})

	t.Run("transaction rolls back none of its writes on error", func(t *testing.T) {
		s := newStore(t)
		sentinel := errors.New("boom")
		err := s.WithinTransaction(ctx, func(ctx context.Context, tx store.ManifestStore) error {
			if err := tx.PutVersion(ctx, store.Version{Kind: "Team", ID: "a", Number: 1, YAML: []byte("metadata:\n  id: a\n  name: A\n")}); err != nil {
				return err
			}
			return sentinel
		})
		if !errors.Is(err, sentinel) {
			t.Fatalf("expected the sentinel error back, got %v", err)
		}
		ids, err := s.ListIDs(ctx, "Team")
		must(t, err)
		if len(ids) != 0 {
			t.Fatalf("expected the transaction rolled back, got %v", ids)
		}
	})

	t.Run("put working copy and get current returns working bytes", func(t *testing.T) {
		s := newStore(t)
		workingYAML := []byte("metadata:\n  id: t1\n  name: Working Team\n")
		must(t, s.PutWorking(ctx, "Team", "t1", workingYAML))

		cur, found, err := s.GetCurrent(ctx, "Team", "t1")
		must(t, err)
		if !found {
			t.Fatal("expected found")
		}
		if string(cur.YAML) != string(workingYAML) {
			t.Fatalf("expected working copy YAML, got %q", cur.YAML)
		}
		if cur.Number != 0 {
			t.Fatalf("expected working copy to have version 0, got %d", cur.Number)
		}
	})

	t.Run("list summaries includes working copy", func(t *testing.T) {
		s := newStore(t)
		workingYAML := []byte("metadata:\n  id: t1\n  name: Working Team\n")
		must(t, s.PutWorking(ctx, "Team", "t1", workingYAML))

		summaries, err := s.ListSummaries(ctx, "Team", "", nil)
		must(t, err)
		if len(summaries) != 1 || summaries[0].ID != "t1" {
			t.Fatalf("expected summary for t1, got %+v", summaries)
		}
	})

	t.Run("put version makes it current when no working copy", func(t *testing.T) {
		s := newStore(t)

		// Put a version
		v1YAML := []byte("metadata:\n  id: t1\n  name: Team One\n")
		v1 := store.Version{Kind: "Team", ID: "t1", Number: 1, YAML: v1YAML, Actor: "a1", Reason: "seed", On: time.Now().UTC()}
		must(t, s.PutVersion(ctx, v1))

		// GetCurrent should return the version
		cur, found, err := s.GetCurrent(ctx, "Team", "t1")
		must(t, err)
		if !found || string(cur.YAML) != string(v1YAML) || cur.Number != 1 {
			t.Fatalf("expected version 1, got %+v", cur)
		}

		// Now put a working copy
		workingYAML := []byte("metadata:\n  id: t1\n  name: Team One Working\n")
		must(t, s.PutWorking(ctx, "Team", "t1", workingYAML))

		// GetCurrent should now return the working copy (working copy preferred over version)
		cur, found, err = s.GetCurrent(ctx, "Team", "t1")
		must(t, err)
		if !found || string(cur.YAML) != string(workingYAML) || cur.Number != 0 {
			t.Fatalf("expected working copy (version 0), got %+v", cur)
		}

		// Put a new working copy (overwrite old one)
		workingYAML2 := []byte("metadata:\n  id: t1\n  name: Team One Working Updated\n")
		must(t, s.PutWorking(ctx, "Team", "t1", workingYAML2))

		// GetCurrent should return the updated working copy
		cur, found, err = s.GetCurrent(ctx, "Team", "t1")
		must(t, err)
		if !found || string(cur.YAML) != string(workingYAML2) || cur.Number != 0 {
			t.Fatalf("expected updated working copy, got %+v", cur)
		}
	})
}

// RunOperationalStore exercises every store.OperationalStore method.
func RunOperationalStore(t *testing.T, newStore func(t *testing.T) store.OperationalStore) {
	t.Helper()
	ctx := context.Background()

	t.Run("project state history is append-only and ordered", func(t *testing.T) {
		s := newStore(t)
		hist, err := s.ListProjectStateHistory(ctx, "proj1")
		must(t, err)
		if hist == nil || len(hist) != 0 {
			t.Fatalf("expected an empty, non-nil slice, got %#v", hist)
		}

		base := time.Now().UTC()
		must(t, s.PutProjectStateTransition(ctx, store.ProjectStateEntry{
			ProjectID: "proj1", State: "in review", Actor: "a1", On: base,
		}))
		must(t, s.PutProjectStateTransition(ctx, store.ProjectStateEntry{
			ProjectID: "proj1", State: "approved", Actor: "a1", On: base.Add(time.Minute),
		}))
		must(t, s.PutProjectStateTransition(ctx, store.ProjectStateEntry{
			ProjectID: "proj2", State: "in review", Actor: "a1", On: base,
		}))

		hist, err = s.ListProjectStateHistory(ctx, "proj1")
		must(t, err)
		if len(hist) != 2 || hist[0].State != "in review" || hist[1].State != "approved" {
			t.Fatalf("got %+v", hist)
		}

		hist, err = s.ListProjectStateHistory(ctx, "proj2")
		must(t, err)
		if len(hist) != 1 {
			t.Fatalf("got %+v", hist)
		}
	})

}

// RunVaultIndex exercises every store.VaultIndex method against a freshly
// constructed adapter, on top of the full store.ManifestStore suite the
// index embeds. Journal() and Operational() need only be non-nil and
// usable on a fresh adapter: surviving a close and reopen is not asserted
// here, since an in-memory adapter (if one is ever written) may have
// neither to persist.
func RunVaultIndex(t *testing.T, newIndex func(t *testing.T) store.VaultIndex) {
	t.Helper()
	ctx := context.Background()

	RunManifestStore(t, func(t *testing.T) store.ManifestStore {
		return newIndex(t)
	})

	t.Run("file hash put, get, delete, missing", func(t *testing.T) {
		idx := newIndex(t)

		_, found, err := idx.GetFileHash(ctx, "Goal", "g1")
		must(t, err)
		if found {
			t.Fatal("expected not found for a hash never recorded")
		}

		must(t, idx.PutFileHash(ctx, "Goal", "g1", "abc123", "2026-01-01T00:00:00Z"))
		hash, found, err := idx.GetFileHash(ctx, "Goal", "g1")
		must(t, err)
		if !found || hash != "abc123" {
			t.Fatalf("got hash=%q found=%v", hash, found)
		}

		// A second put overwrites rather than failing or duplicating.
		must(t, idx.PutFileHash(ctx, "Goal", "g1", "def456", "2026-01-02T00:00:00Z"))
		hash, found, err = idx.GetFileHash(ctx, "Goal", "g1")
		must(t, err)
		if !found || hash != "def456" {
			t.Fatalf("expected the overwritten hash, got %q", hash)
		}

		must(t, idx.DeleteFileHash(ctx, "Goal", "g1"))
		_, found, err = idx.GetFileHash(ctx, "Goal", "g1")
		must(t, err)
		if found {
			t.Fatal("expected not found after delete")
		}

		// Deleting a hash that was never recorded is quiet, not an error.
		must(t, idx.DeleteFileHash(ctx, "Goal", "does-not-exist"))
	})

	t.Run("meta put, get, overwrite, missing", func(t *testing.T) {
		idx := newIndex(t)

		_, found, err := idx.GetMeta(ctx, "vault.yaml.hash")
		must(t, err)
		if found {
			t.Fatal("expected not found for a key never recorded")
		}

		must(t, idx.PutMeta(ctx, "vault.yaml.hash", "hash1"))
		value, found, err := idx.GetMeta(ctx, "vault.yaml.hash")
		must(t, err)
		if !found || value != "hash1" {
			t.Fatalf("got value=%q found=%v", value, found)
		}

		must(t, idx.PutMeta(ctx, "vault.yaml.hash", "hash2"))
		value, found, err = idx.GetMeta(ctx, "vault.yaml.hash")
		must(t, err)
		if !found || value != "hash2" {
			t.Fatalf("expected the overwritten value, got %q", value)
		}
	})

	t.Run("journal and operational are reachable", func(t *testing.T) {
		idx := newIndex(t)
		if idx.Journal() == nil {
			t.Fatal("expected a non-nil ApplyJournal")
		}
		if idx.Operational() == nil {
			t.Fatal("expected a non-nil OperationalStore")
		}
		if idx.Docs() == nil {
			t.Fatal("expected a non-nil DocStore")
		}
	})

	t.Run("docs", func(t *testing.T) {
		RunDocStore(t, func(t *testing.T) store.DocStore { return newIndex(t).Docs() })
	})
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// RunOpLog exercises every store.OpLog method against a freshly
// constructed adapter: dense sequence numbers per manifest, reads by
// position, and compaction that keeps what came after.
func RunOpLog(t *testing.T, newLog func(t *testing.T) store.OpLog) {
	t.Helper()
	ctx := context.Background()

	op := func(kind, id, path string, v any) store.Op {
		o := store.Op{Kind: kind, ID: id}
		o.Path = path
		o.Value = v
		o.Clock.Wall = 1
		o.Clock.Actor = "a"
		return o
	}

	t.Run("append numbers densely per manifest", func(t *testing.T) {
		l := newLog(t)
		first, err := l.Append(ctx, []store.Op{op("Goal", "g1", "/spec/a", 1), op("Goal", "g1", "/spec/b", 2)})
		must(t, err)
		if first != 1 {
			t.Fatalf("first seq %d", first)
		}
		first, err = l.Append(ctx, []store.Op{op("Goal", "g1", "/spec/c", 3)})
		must(t, err)
		if first != 3 {
			t.Fatalf("third op seq %d", first)
		}
		first, err = l.Append(ctx, []store.Op{op("Goal", "g2", "/spec/a", 1)})
		must(t, err)
		if first != 1 {
			t.Fatalf("another manifest starts at 1, got %d", first)
		}
		latest, err := l.Latest(ctx, "Goal", "g1")
		must(t, err)
		if latest != 3 {
			t.Fatalf("latest %d", latest)
		}
		if latest, _ := l.Latest(ctx, "Goal", "none"); latest != 0 {
			t.Fatalf("no ops means 0, got %d", latest)
		}
	})

	t.Run("since reads by position and limit", func(t *testing.T) {
		l := newLog(t)
		_, err := l.Append(ctx, []store.Op{op("Goal", "g1", "/a", 1), op("Goal", "g1", "/b", 2), op("Goal", "g1", "/c", 3)})
		must(t, err)
		ops, err := l.Since(ctx, "Goal", "g1", 1, 0)
		must(t, err)
		if len(ops) != 2 || ops[0].Seq != 2 || ops[1].Path != "/c" {
			t.Fatalf("got %+v", ops)
		}
		ops, err = l.Since(ctx, "Goal", "g1", 0, 2)
		must(t, err)
		if len(ops) != 2 {
			t.Fatalf("limit: got %d", len(ops))
		}
		ops, err = l.Since(ctx, "Goal", "g1", 99, 0)
		must(t, err)
		if ops == nil || len(ops) != 0 {
			t.Fatalf("past the end is an empty, non-nil slice: %#v", ops)
		}
	})

	t.Run("compact keeps what came after", func(t *testing.T) {
		l := newLog(t)
		_, err := l.Append(ctx, []store.Op{op("Goal", "g1", "/a", 1), op("Goal", "g1", "/b", 2), op("Goal", "g1", "/c", 3)})
		must(t, err)
		must(t, l.Compact(ctx, "Goal", "g1", 2))
		ops, err := l.Since(ctx, "Goal", "g1", 0, 0)
		must(t, err)
		if len(ops) != 1 || ops[0].Seq != 3 {
			t.Fatalf("got %+v", ops)
		}
		first, err := l.Append(ctx, []store.Op{op("Goal", "g1", "/d", 4)})
		must(t, err)
		if first != 4 {
			t.Fatalf("numbering continues after compaction, got %d", first)
		}
	})

	t.Run("a batch names one manifest", func(t *testing.T) {
		l := newLog(t)
		if _, err := l.Append(ctx, []store.Op{op("Goal", "g1", "/a", 1), op("Goal", "g2", "/a", 1)}); err == nil {
			t.Fatal("expected an error")
		}
	})
}
