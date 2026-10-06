package engine

import (
	"context"
	"fmt"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/kinds/reportingcycle"
)

// CyclePeriod is one period of a reporting cycle, as every interface
// lays out a KPI's readings: the month it ends (the key its reading is
// filed under), the month it starts, its name and label where the cycle
// names its periods, and the day its reading is due.
type CyclePeriod struct {
	End   string
	Start string
	Name  string
	Label string
	Due   string
}

// CyclePeriods derives a cycle's periods that overlap two months
// (YYYY-MM). Derived here, once, so a web page, a terminal and an agent
// lay out the same periods (TAXONOMY.md D8, D40).
func (e *Engine) CyclePeriods(ctx context.Context, id, from, to string) ([]CyclePeriod, error) {
	v, found, err := e.manifests.GetCurrent(ctx, "ReportingCycle", id)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("%w: ReportingCycle/%s", ErrNotFound, id)
	}
	var doc map[string]any
	if err := e.codec.DecodeInto(v.YAML, &doc); err != nil {
		return nil, err
	}
	spec, _ := doc["spec"].(map[string]any)
	periods, err := reportingcycle.Between(spec, from, to)
	if err != nil {
		return nil, &ValidationError{Problems: []Problem{{Message: err.Error()}}}
	}
	out := make([]CyclePeriod, len(periods))
	for i, p := range periods {
		out[i] = CyclePeriod(p)
	}
	return out, nil
}
