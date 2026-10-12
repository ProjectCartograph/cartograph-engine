package engine

import (
	"slices"
	"testing"
)

// A source a key result is measured from is read by the project, though
// its data list does not repeat it.
func TestAKeyResultsSourceIsOneTheProjectReads(t *testing.T) {
	t.Parallel()
	spec := map[string]any{
		"data": map[string]any{"consumes": []any{map[string]any{"source": "intake-forms"}}},
		"objectives": []any{map[string]any{"keyResults": []any{
			map[string]any{"metric": "Faults found", "source": "check-records"},
			map[string]any{"metric": "Unmeasured"},
		}}},
	}
	uses, _ := dataOf(spec)
	if !slices.Equal(uses, []string{"intake-forms", "check-records"}) {
		t.Fatalf("uses: %v", uses)
	}
}
