// Package kpireadings validates a KPI's series beyond its JSON Schema.
package kpireadings

import (
	"fmt"

	"github.com/ProjectCartograph/cartograph-engine/internal/kinds/kit"
)

// Rules refuses two readings for the same period.
//
// The schema cannot say this: uniqueItems compares whole objects, so two
// entries for 2026-03 with different values are different items and pass.
// Two numbers for one period is a disagreement rather than data, and
// nothing downstream can choose between them — a chart would draw both, a
// roll-up would count both.
func Rules(doc map[string]any, _ kit.RuleContext) []kit.Problem {
	spec, _ := doc["spec"].(map[string]any)
	if spec == nil {
		return nil
	}
	readings, _ := spec["readings"].([]any)
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
	}
	return problems
}
