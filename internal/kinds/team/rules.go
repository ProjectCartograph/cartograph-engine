// Package team holds the Team kind's rules.
package team

import (
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/kinds/kit"
)

// Rules refuses a team that is its own ancestor. Teams are the structure
// (TAXONOMY.md D27): a contributor reaches the work of their teams and of
// every team beneath them, which a loop would make endless.
func Rules(doc map[string]any, ctx kit.RuleContext) []kit.Problem {
	return kit.ParentCycleProblems(doc, ctx, "Team", "team")
}
