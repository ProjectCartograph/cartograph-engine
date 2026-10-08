package engine

import (
	"context"
	"fmt"
	"sort"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/spc"
)

// A KPI's readings as a control chart (TAXONOMY.md D58): the individuals
// and moving range chart, the one that fits a measure read once a period,
// with its limits, the signals that the process has changed, and, where
// the KPI carries specification limits, how capable the process is of
// meeting them.

// ControlChartOf reads a KPI's readings as a control chart.
func (e *Engine) ControlChartOf(ctx context.Context, kpi string) (spc.Chart, error) {
	v, err := e.Get(ctx, "KPI", kpi)
	if err != nil {
		return spc.Chart{}, err
	}
	var doc map[string]any
	if err := e.codec.DecodeInto(v.YAML, &doc); err != nil {
		return spc.Chart{}, fmt.Errorf("KPI/%s: %w", kpi, err)
	}
	cc := spc.Chart{KPI: kpi, Points: []spc.Point{}}
	if lim, ok := specOf(doc)["specLimits"].(map[string]any); ok {
		if f, ok := number(lim["lower"]); ok {
			cc.SpecLower = &f
		}
		if f, ok := number(lim["upper"]); ok {
			cc.SpecUpper = &f
		}
	}
	readings, err := e.allInPlay(ctx, "KPIReadings")
	if err != nil {
		return spc.Chart{}, err
	}
	for _, d := range readings {
		sp := specOf(d.doc)
		if k, _ := sp["kpi"].(string); k != kpi {
			continue
		}
		for _, r := range listOf(sp["readings"]) {
			val, ok := number(r["value"])
			period, _ := r["period"].(string)
			if !ok || period == "" {
				continue
			}
			prov, _ := r["provisional"].(bool)
			cc.Points = append(cc.Points, spc.Point{Period: period, Value: val, Provisional: prov})
		}
	}
	sort.Slice(cc.Points, func(i, j int) bool { return cc.Points[i].Period < cc.Points[j].Period })
	spc.XmR(&cc)
	return cc, nil
}

func number(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint64:
		return float64(n), true
	}
	return 0, false
}
