package engine_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
)

func seedReadings(t *testing.T) *engine.Engine {
	t.Helper()
	e := seededEngine(t)
	mustCommit(t, e, "KPIReadings", "k1-readings", "local",
		"apiVersion: cartograph/v1\nkind: KPIReadings\nmetadata:\n  id: k1-readings\n  name: K1 readings\nspec:\n  kpi: k1\n"+
			"  readings:\n    - {period: \"2026-03\", value: 61}\n    - {period: \"2026-06\", value: 64, provisional: true}\n")
	return e
}

func readingValues(t *testing.T, e *engine.Engine, at time.Time) string {
	t.Helper()
	got, err := e.SeriesAsOf(context.Background(), "KPIReadings", "readings", []string{"k1-readings"}, at)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, it := range got["k1-readings"] {
		out = append(out, it.Key+"="+string(it.Item))
	}
	return strings.Join(out, " ")
}

// Recording a reading sends one item, commits a version, and keeps the
// series in key order; recording a period again restates it.
func TestAppendSeriesItem(t *testing.T) {
	t.Parallel()
	e := seedReadings(t)
	ctx := context.Background()
	v, err := e.AppendSeriesItem(ctx, "KPIReadings", "k1-readings", "readings", map[string]any{"period": "2026-09", "value": 66}, "ada@example.org", "Q3 reading")
	if err != nil {
		t.Fatal(err)
	}
	if v.Number != 2 || v.Actor != "ada@example.org" {
		t.Fatalf("recorded as %+v", v)
	}
	// Out of order, it goes in its place.
	if _, err := e.AppendSeriesItem(ctx, "KPIReadings", "k1-readings", "readings", map[string]any{"period": "2025-12", "value": 58}, "ada@example.org", "late"); err != nil {
		t.Fatal(err)
	}
	// The provisional June figure, restated.
	if _, err := e.AppendSeriesItem(ctx, "KPIReadings", "k1-readings", "readings", map[string]any{"period": "2026-06", "value": 63}, "lee@example.org", "final"); err != nil {
		t.Fatal(err)
	}
	cur, err := e.Get(ctx, "KPIReadings", "k1-readings")
	if err != nil {
		t.Fatal(err)
	}
	text := string(cur.YAML)
	order := []string{"2025-12", "2026-03", "2026-06", "2026-09"}
	at := 0
	for _, p := range order {
		i := strings.Index(text[at:], p)
		if i < 0 {
			t.Fatalf("period %s missing or out of order in:\n%s", p, text)
		}
		at += i
	}
	if strings.Contains(text, "provisional") || !strings.Contains(text, "63") {
		t.Fatalf("June was not restated:\n%s", text)
	}

	// The rows: every value in force, and who recorded it.
	got, err := e.SeriesAsOf(ctx, "KPIReadings", "readings", []string{"k1-readings"}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]string{}
	for _, it := range got["k1-readings"] {
		by[it.Key] = it.RecordedBy
	}
	if by["2026-06"] != "lee@example.org" || by["2026-09"] != "ada@example.org" || by["2026-03"] != "local" {
		t.Fatalf("recorded by %v", by)
	}

	// Errors: an unknown series, an item without its key, a manifest
	// never saved, and a value the schema refuses.
	if _, err := e.AppendSeriesItem(ctx, "KPIReadings", "k1-readings", "nope", map[string]any{"period": "2026-01"}, "a", "r"); !errors.Is(err, engine.ErrNoSeries) {
		t.Errorf("unknown series: %v", err)
	}
	var ve *engine.ValidationError
	if _, err := e.AppendSeriesItem(ctx, "KPIReadings", "k1-readings", "readings", map[string]any{"value": 1}, "a", "r"); !errors.As(err, &ve) {
		t.Errorf("item without a period: %v", err)
	}
	if _, err := e.AppendSeriesItem(ctx, "KPIReadings", "never", "readings", map[string]any{"period": "2026-01", "value": 1}, "a", "r"); !errors.Is(err, engine.ErrNotFound) {
		t.Errorf("never saved: %v", err)
	}
	if _, err := e.AppendSeriesItem(ctx, "KPIReadings", "k1-readings", "readings", map[string]any{"period": "2026-13", "value": 1}, "a", "r"); !errors.As(err, &ve) {
		t.Errorf("month 13: %v", err)
	}
}

// A save records only the items it changes.
func TestSaveRecordsOnlyChangedItems(t *testing.T) {
	t.Parallel()
	e := seedReadings(t)
	ctx := context.Background()
	before, _ := e.Events(ctx, 0, 1000)
	mustCommit(t, e, "KPIReadings", "k1-readings", "local",
		"apiVersion: cartograph/v1\nkind: KPIReadings\nmetadata:\n  id: k1-readings\n  name: K1 readings renamed\nspec:\n  kpi: k1\n"+
			"  readings:\n    - {period: \"2026-03\", value: 61}\n    - {period: \"2026-06\", value: 64, provisional: true}\n")
	after, _ := e.Events(ctx, before[len(before)-1].Seq, 1000)
	for _, ev := range after {
		if ev.Type == "series" {
			t.Errorf("an unchanged reading was recorded again: %+v", ev)
		}
	}
	// Dropping a reading records its removal.
	mustCommit(t, e, "KPIReadings", "k1-readings", "local",
		"apiVersion: cartograph/v1\nkind: KPIReadings\nmetadata:\n  id: k1-readings\n  name: K1 readings renamed\nspec:\n  kpi: k1\n"+
			"  readings:\n    - {period: \"2026-03\", value: 61}\n")
	if got := readingValues(t, e, time.Time{}); strings.Contains(got, "2026-06") || !strings.Contains(got, "2026-03") {
		t.Errorf("after removing June: %s", got)
	}
}
