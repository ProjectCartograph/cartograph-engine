package reportingcycle

import (
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/kinds/kit"
)

func TestACycleHasOneForm(t *testing.T) {
	term := func(name string, end int) any { return map[string]any{"name": name, "endMonth": end} }
	for _, tc := range []struct {
		name string
		spec map[string]any
		bad  bool
	}{
		{"regular", map[string]any{"periodMonths": 3, "startMonth": 1}, false},
		{"regular without a start", map[string]any{"periodMonths": 3}, true},
		{"neither", map[string]any{}, true},
		{"both", map[string]any{"periodMonths": 3, "startMonth": 1, "periods": []any{term("A", 6)}}, true},
		{"terms in order", map[string]any{"periods": []any{term("I", 12), term("II", 4), term("III", 7)}}, false},
		{"terms out of order", map[string]any{"periods": []any{term("I", 12), term("III", 7), term("II", 4)}}, true},
		{"a name twice", map[string]any{"periods": []any{term("I", 6), term("I", 12)}}, true},
		{"a mix", map[string]any{"periods": []any{term("I", 6), map[string]any{"name": "W", "end": "2026-12"}}}, true},
		{"waves", map[string]any{"periods": []any{map[string]any{"name": "B", "end": "2026-12"}, map[string]any{"name": "F", "end": "2027-12"}}}, false},
		{"two waves one month", map[string]any{"periods": []any{map[string]any{"name": "B", "end": "2026-12"}, map[string]any{"name": "F", "end": "2026-12"}}}, true},
	} {
		got := Rules(map[string]any{"spec": tc.spec}, kit.RuleContext{})
		if (len(got) > 0) != tc.bad {
			t.Errorf("%s: problems %+v", tc.name, got)
		}
	}
}
