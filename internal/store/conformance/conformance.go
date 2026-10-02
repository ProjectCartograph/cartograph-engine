// Package conformance is the one test suite every store adapter must
// pass, proving the SQLite, in-memory and Postgres adapters are
// interchangeable behind the store ports.
package conformance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
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

	t.Run("series items merge into their value at any time", func(t *testing.T) {
		s := newStore(t)
		ss, ok := s.(store.SeriesStore)
		if !ok {
			t.Skip("the adapter keeps no series items")
		}
		t0 := time.Date(2026, 1, 31, 12, 0, 0, 0, time.UTC)
		item := func(id, key, value string, at time.Time) store.SeriesItem {
			it := store.SeriesItem{Kind: "KPIReadings", ID: id, Series: "/spec/readings", Key: key, RecordedAt: at, RecordedBy: "a@example.org", Reason: "r"}
			if value != "" {
				it.Item = []byte(`{"period":"` + key + `","value":` + value + `}`)
			}
			return it
		}
		must(t, ss.RecordSeries(ctx, []store.SeriesItem{
			item("r1", "2026-01", "40", t0),
			item("r1", "2026-02", "41", t0.Add(time.Hour)),
			item("r2", "2026-01", "7", t0),
		}))
		must(t, ss.RecordSeries(ctx, []store.SeriesItem{
			item("r1", "2026-01", "39", t0.Add(2*time.Hour)), // a restatement
			item("r1", "2026-02", "", t0.Add(3*time.Hour)),   // a removal
		}))
		values := func(at time.Time, ids []string) map[string]string {
			got, err := ss.SeriesAsOf(ctx, "KPIReadings", "/spec/readings", ids, at)
			must(t, err)
			out := map[string]string{}
			for id, items := range got {
				for _, it := range items {
					out[id+" "+it.Key] = string(it.Item)
				}
			}
			return out
		}
		at := func(h int) time.Time { return t0.Add(time.Duration(h) * time.Hour) }
		cases := []struct {
			at   time.Time
			ids  []string
			want map[string]string
		}{
			{at(0), nil, map[string]string{"r1 2026-01": `{"period":"2026-01","value":40}`, "r2 2026-01": `{"period":"2026-01","value":7}`}},
			{at(1), []string{"r1"}, map[string]string{"r1 2026-01": `{"period":"2026-01","value":40}`, "r1 2026-02": `{"period":"2026-02","value":41}`}},
			{at(2), []string{"r1"}, map[string]string{"r1 2026-01": `{"period":"2026-01","value":39}`, "r1 2026-02": `{"period":"2026-02","value":41}`}},
			{at(3), []string{"r1"}, map[string]string{"r1 2026-01": `{"period":"2026-01","value":39}`}},
			{t0.Add(-time.Second), nil, map[string]string{}},
		}
		for _, c := range cases {
			got := values(c.at, c.ids)
			if len(got) != len(c.want) {
				t.Fatalf("as of %s %v: %v, want %v", c.at, c.ids, got, c.want)
			}
			for k, v := range c.want {
				if !sameJSON(got[k], v) {
					t.Fatalf("as of %s: %s is %s, want %s", c.at, k, got[k], v)
				}
			}
		}
	})

	t.Run("every save writes its event, in order", func(t *testing.T) {
		s := newStore(t)
		el, ok := s.(store.EventLog)
		if !ok {
			t.Skip("the adapter keeps no event log")
		}
		before, err := el.Events(ctx, 0, 1000)
		must(t, err)
		cursor := int64(0)
		if len(before) > 0 {
			cursor = before[len(before)-1].Seq
		}
		now := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
		must(t, s.PutVersion(ctx, store.Version{Kind: "Goal", ID: "g1", Number: 1, YAML: []byte("metadata:\n  id: g1\n"), Actor: "a@example.org", Reason: "r", On: now}))
		must(t, s.PutVersion(ctx, store.Version{Kind: "Goal", ID: "g1", Number: 2, YAML: []byte("metadata:\n  id: g1\n"), Actor: "b@example.org", Reason: "r", On: now}))
		if ss, ok := s.(store.SeriesStore); ok {
			must(t, ss.RecordSeries(ctx, []store.SeriesItem{{Kind: "KPIReadings", ID: "r1", Series: "/spec/readings", Key: "2026-01", Item: []byte(`{"period":"2026-01","value":1}`), RecordedAt: now, RecordedBy: "c@example.org"}}))
		}
		// A transaction rolled back writes no event.
		_ = s.WithinTransaction(ctx, func(ctx context.Context, tx store.ManifestStore) error {
			must(t, tx.PutVersion(ctx, store.Version{Kind: "Goal", ID: "g2", Number: 1, YAML: []byte("metadata:\n  id: g2\n"), Actor: "a", Reason: "r", On: now}))
			return errRollback
		})
		got, err := el.Events(ctx, cursor, 1000)
		must(t, err)
		want := []string{"version Goal/g1 1 a@example.org", "version Goal/g1 2 b@example.org"}
		if _, ok := s.(store.SeriesStore); ok {
			want = append(want, "series KPIReadings/r1 2026-01 c@example.org")
		}
		if len(got) != len(want) {
			t.Fatalf("events after %d: %+v, want %v", cursor, got, want)
		}
		for i, e := range got {
			line := fmt.Sprintf("%s %s/%s %d %s", e.Type, e.Kind, e.ID, e.Number, e.Actor)
			if e.Type == "series" {
				line = fmt.Sprintf("%s %s/%s %s %s", e.Type, e.Kind, e.ID, e.Detail, e.Actor)
			}
			if line != want[i] {
				t.Errorf("event %d is %q, want %q", i, line, want[i])
			}
			if i > 0 && e.Seq <= got[i-1].Seq {
				t.Errorf("event %d does not follow the one before", i)
			}
		}
		// Reading from the last one gives nothing more.
		if more, _ := el.Events(ctx, got[len(got)-1].Seq, 10); len(more) != 0 {
			t.Errorf("events after the last: %+v", more)
		}
	})

	t.Run("pages of summaries are the list in pieces", func(t *testing.T) {
		s := newStore(t)
		p, ok := s.(store.SummaryPager)
		if !ok {
			t.Skip("the adapter lists a kind whole")
		}
		for _, id := range []string{"e", "a", "d", "b", "c"} {
			must(t, s.PutVersion(ctx, store.Version{Kind: "Team", ID: id, Number: 1, YAML: []byte("metadata:\n  id: " + id + "\n  name: Team " + id + "\n")}))
		}
		for _, q := range []string{"", "team"} {
			all, err := s.ListSummaries(ctx, "Team", q, nil)
			must(t, err)
			var paged []store.Summary
			after := ""
			for {
				page, err := p.ListSummariesAfter(ctx, "Team", q, nil, after, 2)
				must(t, err)
				paged = append(paged, page...)
				if len(page) < 2 {
					break
				}
				after = page[len(page)-1].ID
			}
			if len(paged) != len(all) {
				t.Fatalf("query %q: pages gave %d, the list %d", q, len(paged), len(all))
			}
			for i := range all {
				if paged[i].ID != all[i].ID {
					t.Fatalf("query %q: page item %d is %s, list has %s", q, i, paged[i].ID, all[i].ID)
				}
			}
		}
	})

	t.Run("set reads answer as the per-manifest reads do", func(t *testing.T) {
		s := newStore(t)
		sr, isSet := s.(store.SetReader)
		vc, isCounter := s.(store.VersionCounter)
		if !isSet && !isCounter {
			t.Skip("the adapter reads one manifest at a time")
		}
		now := time.Now().UTC()
		put := func(kind, id string, n int, name string) {
			text := []byte("metadata:\n  id: " + id + "\n  name: " + name + "\n")
			doc := []byte(`{"metadata":{"id":"` + id + `","name":"` + name + `"}}`)
			must(t, s.PutVersion(ctx, store.Version{Kind: kind, ID: id, Number: n, YAML: text, Actor: "a", Reason: "r", On: now, Doc: doc}))
		}
		put("Goal", "g1", 1, "One")
		put("Goal", "g1", 2, "One again")
		put("Goal", "g2", 1, "Two")
		put("Project", "p1", 1, "Project")
		put("KPI", "k1", 1, "Indicator")
		must(t, s.PutWorking(ctx, "Goal", "g2", []byte("metadata:\n  id: g2\n  name: Two, unsaved\n")))
		must(t, s.PutWorking(ctx, "Goal", "g3", []byte("metadata:\n  id: g3\n  name: Three, never saved\n")))
		must(t, s.IndexReferences(ctx, "Project", "p1", []store.Ref{{Path: "/spec/goal", ToKind: "Goal", ToID: "g1"}}))
		must(t, s.IndexReferences(ctx, "KPI", "k1", []store.Ref{{Path: "/spec/goal", ToKind: "Goal", ToID: "g1"}, {Path: "/spec/also", ToKind: "Goal", ToID: "g2"}}))

		if isCounter {
			for id, want := range map[string]int{"g1": 2, "g2": 1, "g3": 0, "missing": 0} {
				got, err := vc.LatestNumber(ctx, "Goal", id)
				must(t, err)
				if got != want {
					t.Errorf("LatestNumber(Goal/%s) = %d, want %d", id, got, want)
				}
			}
		}
		if !isSet {
			return
		}
		all, err := sr.CurrentOfKind(ctx, "Goal")
		must(t, err)
		ids, err := s.ListIDs(ctx, "Goal")
		must(t, err)
		if len(all) != len(ids) {
			t.Fatalf("CurrentOfKind gave %d manifests, ListIDs %d", len(all), len(ids))
		}
		for i, v := range all {
			if v.ID != ids[i] {
				t.Fatalf("CurrentOfKind is not in id order: %s at %d, want %s", v.ID, i, ids[i])
			}
			one, _, err := s.GetCurrent(ctx, "Goal", v.ID)
			must(t, err)
			if v.Number != one.Number || string(v.YAML) != string(one.YAML) {
				t.Errorf("CurrentOfKind(Goal/%s) = %d %q, GetCurrent = %d %q", v.ID, v.Number, v.YAML, one.Number, one.YAML)
			}
		}
		some, err := sr.CurrentMany(ctx, "Goal", []string{"g3", "missing", "g1"})
		must(t, err)
		if len(some) != 2 || some[0].ID != "g1" || some[1].ID != "g3" || some[1].Number != 0 {
			t.Fatalf("CurrentMany(g3, missing, g1) = %+v, want g1 then g3's working copy", some)
		}
		byGoal, err := sr.ReferencingKind(ctx, "Goal")
		must(t, err)
		for _, id := range []string{"g1", "g2", "g3"} {
			one, err := s.ListReferencing(ctx, "Goal", id)
			must(t, err)
			got := byGoal[id]
			if len(got) != len(one) {
				t.Fatalf("ReferencingKind(Goal)[%s] has %d, ListReferencing %d", id, len(got), len(one))
			}
			for i := range one {
				if got[i].Kind != one[i].Kind || got[i].ID != one[i].ID || got[i].Version != one[i].Version || got[i].Name != one[i].Name {
					t.Errorf("ReferencingKind(Goal)[%s][%d] = %+v, ListReferencing = %+v", id, i, got[i], one[i])
				}
			}
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

	t.Run("current states answer as the histories do", func(t *testing.T) {
		s := newStore(t)
		sr, ok := s.(store.StateSetReader)
		if !ok {
			t.Skip("the adapter reads one project at a time")
		}
		at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		for i, st := range []string{"defined", "handed off"} {
			must(t, s.PutProjectStateTransition(ctx, store.ProjectStateEntry{ProjectID: "p1", State: st, Actor: "a", On: at.Add(time.Duration(i) * time.Hour)}))
		}
		// Two moves at the same instant: the later one written is current.
		must(t, s.PutProjectStateTransition(ctx, store.ProjectStateEntry{ProjectID: "p2", State: "defined", Actor: "a", On: at}))
		must(t, s.PutProjectStateTransition(ctx, store.ProjectStateEntry{ProjectID: "p2", State: "cancelled", Actor: "a", On: at}))
		got, err := sr.CurrentStates(ctx, []string{"p1", "p2", "never-moved"})
		must(t, err)
		want := map[string]string{"p1": "handed off", "p2": "cancelled"}
		if len(got) != len(want) || got["p1"] != want["p1"] || got["p2"] != want["p2"] {
			t.Fatalf("CurrentStates = %v, want %v", got, want)
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
		if idx.Access() == nil {
			t.Fatal("expected a non-nil AccessStore")
		}
	})

	t.Run("docs", func(t *testing.T) {
		RunDocStore(t, func(t *testing.T) store.DocStore { return newIndex(t).Docs() })
	})

	t.Run("access", func(t *testing.T) {
		RunAccessStore(t, func(t *testing.T) store.AccessStore { return newIndex(t).Access() })
	})
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

var errRollback = errors.New("roll back")

// sameJSON reports whether two JSON texts hold the same value.
func sameJSON(a, b string) bool {
	var x, y any
	if json.Unmarshal([]byte(a), &x) != nil || json.Unmarshal([]byte(b), &y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}
