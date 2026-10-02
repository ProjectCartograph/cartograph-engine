package postgres_test

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store/postgres"
)

// series is a series manifest with n readings, as JSON text.
func series(n int, restated bool) string {
	rs := []map[string]any{}
	for i := 0; i < n; i++ {
		r := map[string]any{"period": fmt.Sprintf("%04d-%02d", 2020+i/12, 1+i%12), "value": 40 + i}
		if restated && i == 0 {
			r["value"], r["note"] = 39, "restated"
		}
		rs = append(rs, r)
	}
	b, _ := json.Marshal(map[string]any{"apiVersion": "cartograph/v1", "kind": "KPIReadings",
		"metadata": map[string]any{"id": "r1", "name": "Readings"}, "spec": map[string]any{"kpi": "k1", "readings": rs}})
	return string(b)
}

func sameJSON(t *testing.T, a, b []byte) bool {
	t.Helper()
	var x, y any
	if err := json.Unmarshal(a, &x); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &y); err != nil {
		t.Fatal(err)
	}
	return reflect.DeepEqual(x, y)
}

// Compacting a growing series keeps every version exactly, keeps the
// latest as it was, and shrinks the history.
func TestCompactHistory(t *testing.T) {
	ctx := context.Background()
	pool := openPool(t)
	m := postgres.NewManifestStore(pool)
	written := map[int]string{}
	old := time.Now().Add(-48 * time.Hour).UTC()
	const n = 40
	for i := 1; i <= n; i++ {
		text := series(i, i > 20)
		written[i] = text
		if err := m.PutVersion(ctx, store.Version{Kind: "KPIReadings", ID: "r1", Number: i, YAML: []byte(text), Actor: "a", Reason: "reading", On: old.Add(time.Duration(i) * time.Minute)}); err != nil {
			t.Fatal(err)
		}
	}
	size := func() int {
		var b int
		if err := pool.QueryRow(ctx, `SELECT sum(coalesce(octet_length(yaml), 0) + coalesce(octet_length(doc::text), 0) + coalesce(octet_length(patch::text), 0))
			FROM manifest_versions WHERE kind = 'KPIReadings'`).Scan(&b); err != nil {
			t.Fatal(err)
		}
		return b
	}
	before := size()

	compacted, err := m.CompactHistory(ctx, time.Now().Add(-24*time.Hour), 100)
	if err != nil {
		t.Fatal(err)
	}
	if compacted != n-1 {
		t.Fatalf("compacted %d versions, want %d (all but the latest)", compacted, n-1)
	}
	after := size()
	if after*3 > before {
		t.Errorf("history is %d bytes after compaction, %d before: expected a third or less", after, before)
	}
	t.Logf("history of %d versions: %d bytes before, %d after", n, before, after)

	check := func(v store.Version) {
		t.Helper()
		got := v.YAML
		if len(got) == 0 {
			got = v.Doc
		}
		if !sameJSON(t, got, []byte(written[v.Number])) {
			t.Fatalf("version %d rebuilt as %s, want %s", v.Number, got, written[v.Number])
		}
	}
	for i := 1; i <= n; i++ {
		v, found, err := m.GetVersion(ctx, "KPIReadings", "r1", i)
		if err != nil || !found {
			t.Fatalf("version %d: %v, %v", i, found, err)
		}
		check(v)
	}
	all, err := m.ListVersions(ctx, "KPIReadings", "r1")
	if err != nil || len(all) != n {
		t.Fatalf("list versions: %d, %v", len(all), err)
	}
	for _, v := range all {
		check(v)
	}
	page, _, err := m.ListAllVersions(ctx, 100, "")
	if err != nil || len(page) != n {
		t.Fatalf("all versions: %d, %v", len(page), err)
	}
	for _, v := range page {
		check(v)
	}

	// The latest keeps its text, for a replica of the previous release.
	var text *string
	if err := pool.QueryRow(ctx, `SELECT yaml FROM manifest_versions WHERE kind = 'KPIReadings' AND id = 'r1' AND number = $1`, n).Scan(&text); err != nil || text == nil {
		t.Fatalf("the latest version lost its text: %v", err)
	}
	// And the database refuses to compact it, whoever asks.
	if _, err := pool.Exec(ctx, `UPDATE manifest_versions SET yaml = NULL WHERE kind = 'KPIReadings' AND id = 'r1' AND number = $1`, n); err == nil {
		t.Error("the latest version was compacted")
	}
	// A version saved after compaction, and compacting again, still rebuild.
	if err := m.PutVersion(ctx, store.Version{Kind: "KPIReadings", ID: "r1", Number: n + 1, YAML: []byte(series(n+1, true)), Actor: "a", Reason: "reading", On: old.Add(time.Hour * 2)}); err != nil {
		t.Fatal(err)
	}
	written[n+1] = series(n+1, true)
	if again, err := m.CompactHistory(ctx, time.Now().Add(-24*time.Hour), 100); err != nil || again != 1 {
		t.Fatalf("second compaction: %d, %v", again, err)
	}
	for i := 1; i <= n+1; i++ {
		v, _, err := m.GetVersion(ctx, "KPIReadings", "r1", i)
		if err != nil {
			t.Fatal(err)
		}
		check(v)
	}
	// Nothing recent is compacted.
	if recent, _ := m.CompactHistory(ctx, old, 100); recent != 0 {
		t.Errorf("compacted %d versions newer than the grace period", recent)
	}
}
