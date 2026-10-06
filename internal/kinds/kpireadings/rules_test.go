package kpireadings

import (
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/kinds/kit"
)

type docs map[string]map[string]map[string]any

func (d docs) Documents(kind string) (map[string]map[string]any, error) { return d[kind], nil }
func (d docs) HasSnapshots(string, string) (bool, error)                { return false, nil }

// A KPI read by term is read at each term's end and at no other month;
// one on equal periods is held to nothing new (TAXONOMY.md D40).
func TestReadingsFallOnTheCyclesPeriods(t *testing.T) {
	lookup := docs{
		"KPI": {
			"by-term":    {"spec": map[string]any{"cycle": "termly"}},
			"by-quarter": {"spec": map[string]any{"cycle": "quarterly"}},
		},
		"ReportingCycle": {
			"termly": {"spec": map[string]any{"periods": []any{
				map[string]any{"name": "Term I", "endMonth": 12},
				map[string]any{"name": "Term II", "endMonth": 4},
				map[string]any{"name": "Term III", "endMonth": 7},
			}}},
			"quarterly": {"spec": map[string]any{"periodMonths": 3, "startMonth": 1}},
		},
	}
	readings := func(kpi string, periods ...string) map[string]any {
		var rs []any
		for _, p := range periods {
			rs = append(rs, map[string]any{"period": p, "value": 1})
		}
		return map[string]any{"spec": map[string]any{"kpi": kpi, "readings": rs}}
	}
	ctx := kit.RuleContext{Lookup: lookup}
	if p := Rules(readings("by-term", "2026-12", "2027-04"), ctx); len(p) != 0 {
		t.Fatalf("term ends refused: %+v", p)
	}
	if p := Rules(readings("by-term", "2026-12", "2027-03"), ctx); len(p) != 1 || p[0].Path != "/spec/readings/1/period" {
		t.Fatalf("March under terms: %+v", p)
	}
	if p := Rules(readings("by-quarter", "2026-02"), ctx); len(p) != 0 {
		t.Fatalf("a quarterly KPI is held to its periods: %+v", p)
	}
}
