// Package operation implements the Operation kind's rules beyond its JSON
// Schema: a service's recurring funding lines (TAXONOMY.md D39) and its
// mandate's issuer (D43).
package operation

import (
	"fmt"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/kinds/kit"
)

// Rules allows at most one funding line per currency and period, the
// project's rule with the period added: a service may be paid monthly
// from one budget and yearly from another, in the same currency.
func Rules(doc map[string]any, _ kit.RuleContext) []kit.Problem {
	spec, _ := doc["spec"].(map[string]any)
	funding, _ := spec["funding"].([]any)
	var problems []kit.Problem
	seen := map[string]int{}
	for i, f := range funding {
		fm, ok := f.(map[string]any)
		if !ok {
			continue
		}
		currency, _ := fm["currency"].(string)
		per, _ := fm["per"].(string)
		if currency == "" || per == "" {
			continue
		}
		key := currency + "/" + per
		if first, dup := seen[key]; dup {
			problems = append(problems, kit.Problem{
				Path: fmt.Sprintf("/spec/funding/%d/currency", i),
				Message: fmt.Sprintf(
					"funding in %s per %s duplicates the line at index %d; at most one funding line per currency and period", currency, per, first),
			})
		} else {
			seen[key] = i
		}
	}
	return append(problems, kit.MandateProblems(spec)...)
}
