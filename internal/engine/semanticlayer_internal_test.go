package engine

import "testing"

// What a person said an indicator counts, and which rows count, travels
// to the export in their words, for an analyst to map (TAXONOMY.md D63).
func TestAFormulaInWordsReachesTheExport(t *testing.T) {
	t.Parallel()
	mt := map[string]any{
		"numerator":   map[string]any{"counts": "crates graded alike"},
		"denominator": map[string]any{"counts": "crates graded twice"},
		"where":       []any{map[string]any{"input": "the depot's region", "op": "is", "value": "north"}},
	}
	got := withFormula("Share of crates graded alike.", mt)
	want := "Share of crates graded alike. Counts: crates graded alike. Out of: crates graded twice. Only where the depot's region is north."
	if got != want {
		t.Fatalf("got %q", got)
	}
	if withFormula("As defined.", map[string]any{}) != "As defined." {
		t.Fatal("a metric with nothing said changed its description")
	}
}
