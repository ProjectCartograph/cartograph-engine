//go:build integration

package postgres_test

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store/postgres"
)

// A database the previous release wrote, and a replica of that release
// still writing to it after the upgrade (docs/adr/0012, expand then
// contract): the migration keeps what was there, and the triggers keep
// the manifests table right whichever release writes.
func TestUpgradeFromTextOnly(t *testing.T) {
	ctx := context.Background()
	url := freshSchema(t)
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := conn.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}

	// The schema as 2.2.1 left it.
	exec(`CREATE TABLE schema_migrations (name text PRIMARY KEY, applied_on timestamptz NOT NULL DEFAULT now())`)
	for _, name := range []string{"0001_init.sql", "0002_people.sql"} {
		body, err := os.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		exec(string(body))
		exec(`INSERT INTO schema_migrations (name) VALUES ($1)`, name)
	}

	// What 2.2.1's PutVersion and PutWorking send.
	at := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	oldPut := func(kind, id string, n int, name, text string) int64 {
		t.Helper()
		tag, err := conn.Exec(ctx, `
			INSERT INTO manifest_versions (kind, id, number, name, yaml, actor, reason, on_ts)
			SELECT $1, $2, $3, $4, $5, $6, $7, $8
			WHERE $3 = 1 + COALESCE((SELECT max(number) FROM manifest_versions WHERE kind = $1 AND id = $2), 0)`,
			kind, id, n, name, text, "old@example.org", "before", at)
		if err != nil {
			t.Fatalf("old put %s/%s %d: %v", kind, id, n, err)
		}
		return tag.RowsAffected()
	}
	oldPut("Goal", "g1", 1, "One", "metadata:\n  id: g1\n  name: One\nspec:\n  level: goal\n")
	oldPut("Goal", "g1", 2, "One again", "metadata:\n  id: g1\n  name: One again\nspec:\n  level: goal\n")
	oldPut("Goal", "g2", 1, "Two", `{"metadata":{"id":"g2","name":"Two","labels":{"area":"reading"}},"spec":{"level":"goal"}}`)
	exec(`INSERT INTO working_copies (kind, id, name, yaml, on_ts) VALUES ('Goal', 'g3', 'Three', $1, $2)`,
		"metadata:\n  id: g3\n  name: Three\n", at)
	exec(`INSERT INTO manifest_references (from_kind, from_id, path, to_kind, to_id) VALUES ('Goal', 'g2', '/spec/parent', 'Goal', 'g1')`)

	// This release opens it.
	pool, err := postgres.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	m := postgres.NewManifestStore(pool)

	if n, _ := m.LatestNumber(ctx, "Goal", "g1"); n != 2 {
		t.Errorf("g1 is at %d after the upgrade, want 2", n)
	}
	if cur, found, _ := m.GetCurrent(ctx, "Goal", "g3"); !found || cur.Number != 0 || cur.Reason != "working copy" {
		t.Errorf("g3's working copy is %+v, %v", cur, found)
	}
	sums, err := m.ListSummaries(ctx, "Goal", "", nil)
	if err != nil || len(sums) != 3 {
		t.Fatalf("summaries %+v, %v", sums, err)
	}
	if sums[1].ID != "g2" || sums[1].Labels["area"] != "reading" {
		t.Errorf("g2's labels came from its JSON text: got %+v", sums[1])
	}
	refs, err := m.ListReferencing(ctx, "Goal", "g1")
	if err != nil || len(refs) != 1 || refs[0].ID != "g2" {
		t.Errorf("who references g1: %+v, %v", refs, err)
	}

	// YAML waits for a replica to decode it; JSON was decoded in SQL.
	stale, err := m.StaleDocs(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 3 {
		t.Fatalf("stale: %+v, want g1 v1 and v2 and g3's working copy", stale)
	}

	// A replica of 2.2.1, still running, saves the next version and is
	// refused a wrong one, as it always was.
	if n := oldPut("Goal", "g1", 3, "One, third", "metadata:\n  id: g1\n  name: One, third\n"); n != 1 {
		t.Fatalf("the old replica's next version was not saved")
	}
	if n := oldPut("Goal", "g1", 5, "Skipped", "metadata:\n  id: g1\n"); n != 0 {
		t.Fatalf("the old replica saved a version out of order")
	}
	if n, _ := m.LatestNumber(ctx, "Goal", "g1"); n != 3 {
		t.Errorf("g1 is at %d after the old replica's save, want 3", n)
	}
	if err := m.PutVersion(ctx, store.Version{Kind: "Goal", ID: "g1", Number: 3, YAML: []byte("x: 1\n"), On: at}); err == nil {
		t.Error("this release saved a version number the old replica already took")
	}

	// The repair gives every stale row a document, and the current one
	// reaches the manifest's row.
	stale, _ = m.StaleDocs(ctx, 100)
	for i := range stale {
		stale[i].Doc = []byte(`{"metadata":{"id":"` + stale[i].ID + `","labels":{"repaired":"yes"}}}`)
	}
	if err := m.PutDocs(ctx, stale); err != nil {
		t.Fatal(err)
	}
	if again, _ := m.StaleDocs(ctx, 100); len(again) != 0 {
		t.Errorf("still stale after the repair: %+v", again)
	}
	sums, _ = m.ListSummaries(ctx, "Goal", "", nil)
	if sums[0].ID != "g1" || sums[0].Version != 3 || sums[0].Labels["repaired"] != "yes" {
		t.Errorf("g1 after the repair: %+v", sums[0])
	}
	// The repair changes nothing else: history is still append-only.
	if _, err := conn.Exec(ctx, `UPDATE manifest_versions SET reason = 'rewritten' WHERE kind = 'Goal' AND id = 'g1' AND number = 1`); err == nil {
		t.Error("a version's reason was rewritten")
	}
}

// Two writers racing for the same next version: one wins, the other is
// told its number is taken, and the manifest's row is the winner's.
func TestPutVersionRace(t *testing.T) {
	ctx := context.Background()
	m := postgres.NewManifestStore(openPool(t))
	if err := m.PutVersion(ctx, store.Version{Kind: "Goal", ID: "g", Number: 1, YAML: []byte("metadata:\n  id: g\n")}); err != nil {
		t.Fatal(err)
	}
	for _, number := range []int{2, 1} { // the next version, and a first version raced for again
		var wg sync.WaitGroup
		errs := make([]error, 8)
		for i := range errs {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				errs[i] = m.PutVersion(ctx, store.Version{Kind: "Goal", ID: "g", Number: number, YAML: []byte("metadata:\n  id: g\n")})
			}(i)
		}
		wg.Wait()
		won := 0
		for _, err := range errs {
			if err == nil {
				won++
			}
		}
		want := 1
		if number == 1 {
			want = 0 // version 1 exists already
		}
		if won != want {
			t.Errorf("version %d: %d writers won, want %d (%v)", number, won, want, errs)
		}
	}
	if n, _ := m.LatestNumber(ctx, "Goal", "g"); n != 2 {
		t.Errorf("latest is %d, want 2", n)
	}
}
