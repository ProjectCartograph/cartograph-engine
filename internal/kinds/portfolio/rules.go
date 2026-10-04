// Package portfolio implements the Portfolio kind's rules beyond its JSON
// Schema (TAXONOMY.md D32).
package portfolio

import (
	"fmt"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/kinds/kit"
)

// Rules refuses a portfolio inside itself, and a strategic objective
// below the objective level: a portfolio is prioritised against what the
// organisation sets out to do, and an outcome is what is then true.
func Rules(doc map[string]any, ctx kit.RuleContext) []kit.Problem {
	problems := kit.ParentsCycleProblems(doc, ctx, "Portfolio", "portfolios", "portfolio")
	spec, _ := doc["spec"].(map[string]any)
	objectives, _ := spec["objectives"].([]any)
	if len(objectives) == 0 || ctx.Lookup == nil {
		return problems
	}
	goals, err := ctx.Lookup.Documents("Goal")
	if err != nil {
		return problems
	}
	for i, raw := range objectives {
		id, _ := raw.(string)
		g, ok := goals[id]
		if !ok {
			continue // the reference check reports a missing goal
		}
		gs, _ := g["spec"].(map[string]any)
		if level, _ := gs["level"].(string); level == "outcome" {
			problems = append(problems, kit.Problem{Path: fmt.Sprintf("/spec/objectives/%d", i), Message: fmt.Sprintf(
				"%q is an outcome: a portfolio serves goals and objectives, what the organisation sets out to do", id)})
		}
	}
	return problems
}
