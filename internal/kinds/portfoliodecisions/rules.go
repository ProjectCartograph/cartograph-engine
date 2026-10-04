// Package portfoliodecisions implements the PortfolioDecisions kind's
// rules beyond its JSON Schema (TAXONOMY.md D32).
package portfoliodecisions

import (
	"fmt"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/kinds/kit"
)

// Rules refuses two decisions about the same thing: two answers to
// "invest, hold or stop?" for one project is a disagreement, not a
// decision. Whether each names something the portfolio holds is advice,
// on the portfolio's checks, since membership is declared elsewhere.
func Rules(doc map[string]any, _ kit.RuleContext) []kit.Problem {
	spec, _ := doc["spec"].(map[string]any)
	decisions, _ := spec["decisions"].([]any)
	seen := map[string]int{}
	var problems []kit.Problem
	for i, raw := range decisions {
		d, _ := raw.(map[string]any)
		kind, _ := d["kind"].(string)
		id, _ := d["id"].(string)
		if id == "" {
			continue
		}
		key := kind + "/" + id
		if first, dup := seen[key]; dup {
			problems = append(problems, kit.Problem{Path: fmt.Sprintf("/spec/decisions/%d", i), Message: fmt.Sprintf(
				"%s %q already has a decision (entry %d); one decision each", kind, id, first+1)})
			continue
		}
		seen[key] = i
	}
	return problems
}
