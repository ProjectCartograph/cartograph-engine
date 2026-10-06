// Package kpireadings validates a KPI's series beyond its JSON Schema.
package kpireadings

import (
	"fmt"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/kinds/kit"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/kinds/reportingcycle"
)

// Rules refuses two readings for the same period.
//
// The schema cannot say this: uniqueItems compares whole objects, so two
// entries for 2026-03 with different values are different items and pass.
// Two numbers for one period is a disagreement rather than data, and
// nothing downstream can choose between them — a chart would draw both, a
// roll-up would count both.
//
// A KPI read on named periods (TAXONOMY.md D40) is read only at their
// ends: terms ending in December, April and July have no reading for
// March. A KPI on equal periods is not held to them, as it never was.
func Rules(doc map[string]any, ctx kit.RuleContext) []kit.Problem {
	spec, _ := doc["spec"].(map[string]any)
	if spec == nil {
		return nil
	}
	readings, _ := spec["readings"].([]any)
	cycle := cycleOf(spec, ctx.Lookup)
	seen := map[string]int{}
	var problems []kit.Problem
	for i, r := range readings {
		rm, ok := r.(map[string]any)
		if !ok {
			continue
		}
		period, _ := rm["period"].(string)
		if period == "" {
			continue
		}
		if first, dup := seen[period]; dup {
			problems = append(problems, kit.Problem{
				Path: fmt.Sprintf("/spec/readings/%d/period", i),
				Message: fmt.Sprintf(
					"%s already has a reading (entry %d); one reading per period", period, first+1),
			})
			continue
		}
		seen[period] = i
		if cycle != nil && !reportingcycle.Ends(cycle, period) {
			problems = append(problems, kit.Problem{
				Path:    fmt.Sprintf("/spec/readings/%d/period", i),
				Message: fmt.Sprintf("%s is not the end of one of the cycle's periods; file a reading by the month its period ends", period),
			})
		}
	}
	return problems
}

// cycleOf returns the spec of the cycle the series' KPI is read on, or nil
// when there is none to hold readings to.
func cycleOf(spec map[string]any, lookup kit.Lookup) map[string]any {
	if lookup == nil {
		return nil
	}
	kpi, _ := spec["kpi"].(string)
	kpis, err := lookup.Documents("KPI")
	if err != nil || kpis[kpi] == nil {
		return nil
	}
	kspec, _ := kpis[kpi]["spec"].(map[string]any)
	id, _ := kspec["cycle"].(string)
	if id == "" {
		return nil
	}
	cycles, err := lookup.Documents("ReportingCycle")
	if err != nil || cycles[id] == nil {
		return nil
	}
	cspec, _ := cycles[id]["spec"].(map[string]any)
	return cspec
}
