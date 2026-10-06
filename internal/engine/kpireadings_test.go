package engine_test

import (
	"context"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A KPI's readings are their own file, and the one thing the schema cannot
// say about them is that a period may be read once.
func TestKPIReadings(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	ctx := context.Background()

	readings := func(body string) []byte {
		return []byte("apiVersion: cartograph/v1\nkind: KPIReadings\nmetadata:\n  id: k1-readings\n  name: K1 readings\n" +
			"spec:\n  kpi: k1\n" + body)
	}

	mustCommit(t, e, "KPIReadings", "k1-readings", "local", string(readings(
		"  readings:\n    - {period: \"2026-03\", value: 61}\n    - {period: \"2026-06\", value: 64, provisional: true}\n")))

	// Two numbers for one period is a disagreement, not data: nothing
	// downstream could choose between them.
	_, err := e.Commit(ctx, "KPIReadings", "k1-dupe", []byte(
		"apiVersion: cartograph/v1\nkind: KPIReadings\nmetadata:\n  id: k1-dupe\n  name: Dupe\nspec:\n  kpi: k1\n"+
			"  readings:\n    - {period: \"2026-03\", value: 61}\n    - {period: \"2026-03\", value: 62}\n"),
		"local", "seed")
	if err == nil {
		t.Fatal("expected two readings for one period to be refused")
	}
	if !strings.Contains(err.Error(), "one reading per period") {
		t.Fatalf("expected the reason to be named, got %v", err)
	}

	// A KPI the vault does not hold is refused: a series of numbers about
	// nothing is not a series.
	if _, err := e.Commit(ctx, "KPIReadings", "orphan", []byte(
		"apiVersion: cartograph/v1\nkind: KPIReadings\nmetadata:\n  id: orphan\n  name: Orphan\nspec:\n  kpi: no-such-kpi\n"),
		"local", "seed"); err == nil {
		t.Fatal("expected readings of a KPI that does not exist to be refused")
	}

	// A period outside the YearMonth shape is refused, and so is a reading
	// with no number: an entry that says nothing is not a reading.
	for _, bad := range []string{
		"  readings:\n    - {period: \"2026-3\", value: 61}\n",
		"  readings:\n    - {period: \"2026-03\"}\n",
		"  readings:\n    - {period: \"2026-13\", value: 61}\n",
	} {
		if _, err := e.Commit(ctx, "KPIReadings", "k1-bad", readings(bad), "local", "seed"); err == nil {
			t.Fatalf("expected %q to be refused", bad)
		}
	}

	// A series with no readings at all is fine: declaring that a KPI is
	// tracked comes before the first number arrives.
	mustCommit(t, e, "KPIReadings", "k1-empty", "local",
		"apiVersion: cartograph/v1\nkind: KPIReadings\nmetadata:\n  id: k1-empty\n  name: Empty\nspec:\n  kpi: k1\n")
}

// The standard units the interface offers and the files the example ships
// have to be the same set, or a first run picks a unit the example vault
// cannot resolve.
func TestExampleShipsTheStandardUnits(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(exampleDir(t), "Unit")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	onDisk := map[string]bool{}
	for _, e := range entries {
		onDisk[strings.TrimSuffix(e.Name(), ".yaml")] = true
	}
	for _, u := range engine.StandardUnitIDs() {
		if !onDisk[u] {
			t.Fatalf("standard unit %q is offered but the example does not ship it", u)
		}
	}
}
