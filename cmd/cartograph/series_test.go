//go:build integration

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/reporting"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/reporting/computed"
	reportconformance "github.com/ProjectCartograph/cartograph-engine/v2/internal/reporting/conformance"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store/postgres"
)

// exampleCopy copies the example into a directory of the test's own.
func exampleCopy(t *testing.T, from string) string {
	t.Helper()
	to := t.TempDir()
	if err := copyDir(from, to); err != nil {
		t.Fatal(err)
	}
	return to
}

// sansProvenance is a reporter whose rows leave out what depends on when
// and by whom the record was loaded, so two stores loaded at different
// times compare.
type sansProvenance struct{ reporting.Reporter }

func (r sansProvenance) Run(ctx context.Context, name string) (reporting.Table, error) {
	t, err := r.Reporter.Run(ctx, name)
	if err != nil {
		return t, err
	}
	skip := map[string]bool{"version": true, "updated_on": true, "recorded_at": true, "recorded_by": true}
	out := reporting.Table{Name: t.Name, Rows: [][]any{}}
	var keep []int
	for i, c := range t.Columns {
		if !skip[c] {
			keep = append(keep, i)
			out.Columns = append(out.Columns, c)
		}
	}
	for _, row := range t.Rows {
		var r []any
		for _, i := range keep {
			r = append(r, row[i])
		}
		out.Rows = append(out.Rows, r)
	}
	return out, nil
}

// The same record gives the same reports on a vault and on Postgres;
// readings recorded one at a time on Postgres, and the history compacted,
// still give every version back as it was saved, series included.
func TestSeriesAndReportsAcrossStores(t *testing.T) {
	base := os.Getenv("CARTOGRAPH_TEST_POSTGRES")
	if base == "" {
		t.Skip("CARTOGRAPH_TEST_POSTGRES is unset; `just test-postgres` runs this against a throwaway server")
	}
	ctx := context.Background()
	example, err := filepath.Abs("../../examples/minimal")
	if err != nil {
		t.Fatal(err)
	}

	vault, err := compose(ctx, storeOptions{Target: exampleCopy(t, example)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { vault.Close() })
	target := schemaURL(t, base)
	pg, err := compose(ctx, storeOptions{Target: target})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pg.Close() })
	if rep, err := pg.Engine.ImportDir(ctx, example, "seed@example.org", "the example"); err != nil || len(rep.Problems) > 0 {
		t.Fatalf("import: %v %+v", err, rep.Problems)
	}

	reportconformance.Run(t, sansProvenance{pg.Reports}, sansProvenance{vault.Reports})

	// Readings recorded one at a time.
	const id = "collection-turnaround-readings"
	texts := map[int]map[string]any{}
	keep := func() {
		cur, err := pg.Engine.Get(ctx, "KPIReadings", id)
		if err != nil {
			t.Fatal(err)
		}
		doc, err := pg.Engine.Codec().Decode(cur.YAML)
		if err != nil {
			t.Fatal(err)
		}
		latest, _ := pg.Engine.Versions(ctx, "KPIReadings", id)
		texts[latest[len(latest)-1].Number] = doc
	}
	keep()
	for i := 0; i < 20; i++ {
		period := fmt.Sprintf("%04d-%02d", 2026+(5+i)/12, 1+(5+i)%12)
		if _, err := pg.Engine.AppendSeriesItem(ctx, "KPIReadings", id, "readings", map[string]any{"period": period, "value": 30 - i%5}, "lee@example.org", "monthly"); err != nil {
			t.Fatalf("reading %s: %v", period, err)
		}
		keep()
	}
	// Ten years of monthly readings on a second KPI, to see what a long
	// series costs. Its series starts empty.
	const long = "quality-pass-rate-readings"
	for i := 0; i < 120; i++ {
		period := fmt.Sprintf("%04d-%02d", 2030+i/12, 1+i%12)
		if _, err := pg.Engine.AppendSeriesItem(ctx, "KPIReadings", long, "readings", map[string]any{"period": period, "value": 80 + i%7, "note": "read at the depot"}, "lee@example.org", "monthly"); err != nil {
			t.Fatalf("reading %s: %v", period, err)
		}
	}
	pool := poolOf(t, target)
	stored := func() (history, items int) {
		t.Helper()
		if err := pool.QueryRow(ctx, `SELECT sum(coalesce(octet_length(yaml), 0) + coalesce(octet_length(doc::text), 0) + coalesce(octet_length(patch::text), 0))
			FROM manifest_versions WHERE kind = 'KPIReadings' AND id = $1`, long).Scan(&history); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `SELECT coalesce(sum(octet_length(item::text)), 0) FROM series_items WHERE kind = 'KPIReadings' AND id = $1`, long).Scan(&items); err != nil {
			t.Fatal(err)
		}
		return history, items
	}
	historyBefore, items := stored()

	// Compact everything but the latest, as a replica would a day on.
	m := postgres.NewManifestStore(pool)
	if n, err := m.CompactHistory(ctx, time.Now().Add(time.Hour), 1000); err != nil || n == 0 {
		t.Fatalf("compacted %d: %v", n, err)
	}
	historyAfter, _ := stored()
	t.Logf("120 readings, one at a time: history %d bytes before compaction, %d after, plus %d in series items", historyBefore, historyAfter, items)
	if historyAfter*10 > historyBefore {
		t.Errorf("compaction kept %d of %d bytes: a long series should keep a tenth or less", historyAfter, historyBefore)
	}
	for n, want := range texts {
		v, err := pg.Engine.GetVersion(ctx, "KPIReadings", id, n)
		if err != nil {
			t.Fatal(err)
		}
		got, err := pg.Engine.Codec().Decode(v.YAML)
		if err != nil {
			t.Fatal(err)
		}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("version %d after compaction:\n%v\nwas saved as\n%v", n, got, want)
		}
	}
	// The diff between two compacted versions is one reading.
	changes, err := pg.Engine.Diff(ctx, "KPIReadings", id, 5, 6)
	if err != nil || len(changes) != 1 {
		t.Fatalf("diff 5 to 6: %+v, %v", changes, err)
	}
}

// poolOf opens a pool of its own on a test's database.
func poolOf(t *testing.T, target string) *pgxpool.Pool {
	t.Helper()
	pool, err := postgres.Open(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// The postgres reporter's views give the computed reporter's rows.
func TestReportViewsMatchTheEngine(t *testing.T) {
	base := os.Getenv("CARTOGRAPH_TEST_POSTGRES")
	if base == "" {
		t.Skip("CARTOGRAPH_TEST_POSTGRES is unset; `just test-postgres` runs this against a throwaway server")
	}
	ctx := context.Background()
	example, err := filepath.Abs("../../examples/minimal")
	if err != nil {
		t.Fatal(err)
	}
	target := schemaURL(t, base)
	pg, err := compose(ctx, storeOptions{Target: target, Reports: "postgres"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pg.Close() })
	if _, err := pg.Engine.ImportDir(ctx, example, "seed@example.org", "the example"); err != nil {
		t.Fatal(err)
	}
	e := pg.Engine
	// A restated reading, a draft of a series, and the series items made
	// whole, as a replica's repair makes them.
	if _, err := e.AppendSeriesItem(ctx, "KPIReadings", "collection-turnaround-readings", "readings", map[string]any{"period": "2026-04", "value": 30}, "lee@example.org", "final"); err != nil {
		t.Fatal(err)
	}
	draft, _ := e.Get(ctx, "KPIReadings", "quality-pass-rate-readings")
	if err := e.PutWorking(ctx, "KPIReadings", "quality-pass-rate-readings", draft.YAML); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ReconcileSeries(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := pg.Reports.(computed.Reporter); ok {
		t.Fatal("CARTOGRAPH_REPORTS=postgres composed the computed reporter")
	}
	reportconformance.Run(t, pg.Reports, computed.Reporter{E: e})
}

// With reporting off there is no reporter, and the store has no views.
func TestReportsOff(t *testing.T) {
	c, err := compose(context.Background(), storeOptions{Target: t.TempDir(), Reports: "off"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.Reports != nil {
		t.Fatalf("reporting off composed %T", c.Reports)
	}
}
