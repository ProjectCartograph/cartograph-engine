// Package segment validates the slice tree beyond its JSON Schema.
package segment

import (
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/kinds/kit"
)

// Rules refuses a segment that is its own ancestor. Segments are a tree: a
// dimension is one with no parent, its values are its children.
func Rules(doc map[string]any, ctx kit.RuleContext) []kit.Problem {
	return kit.ParentCycleProblems(doc, ctx, "Segment", "segment")
}
