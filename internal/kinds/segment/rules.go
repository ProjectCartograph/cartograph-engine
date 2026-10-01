// Package segment validates the slice tree beyond its JSON Schema.
package segment

import (
	"fmt"

	"github.com/ProjectCartograph/cartograph-engine/internal/kinds/kit"
)

// Rules refuses a segment that is its own ancestor.
//
// Segments are a tree — a dimension is one with no parent, its values are
// its children — and a cycle in it is not a deep hierarchy, it is a
// hierarchy with no top. Everything that walks the tree, to label a gap or
// to roll a measure up, would walk it forever.
func Rules(doc map[string]any, ctx kit.RuleContext) []kit.Problem {
	spec, _ := doc["spec"].(map[string]any)
	if spec == nil {
		return nil
	}
	parent, _ := spec["parent"].(string)
	if parent == "" || ctx.Lookup == nil {
		return nil
	}
	if parent == ctx.ID {
		return []kit.Problem{{Path: "/spec/parent", Message: "a segment cannot be its own parent"}}
	}

	others, err := ctx.Lookup.Documents("Segment")
	if err != nil {
		return nil
	}
	parentOf := func(id string) string {
		d, ok := others[id]
		if !ok {
			return ""
		}
		s, _ := d["spec"].(map[string]any)
		p, _ := s["parent"].(string)
		return p
	}
	// Walk up from the proposed parent. Bounded by the number of segments,
	// so a cycle that already exists in the store cannot hang this.
	seen := map[string]bool{ctx.ID: true}
	for at := parent; at != ""; at = parentOf(at) {
		if seen[at] {
			return []kit.Problem{{
				Path:    "/spec/parent",
				Message: fmt.Sprintf("this would make %q its own ancestor", ctx.ID),
			}}
		}
		seen[at] = true
		if len(seen) > len(others)+1 {
			break
		}
	}
	return nil
}
