// Package stakeholdermap implements the StakeholderMap kind's rules beyond
// its JSON Schema: a map is scoped to a piece of work rather than to any
// manifest at all, and it scores each resource once.
package stakeholdermap

import (
	"fmt"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/kinds/kit"
)

// scopeKinds is what a map may be about. A stakeholder map belongs to a
// piece of work; scoping one to a Team or a DataSource would be a category
// error, and JSON Schema cannot say so because the Ref shape is shared by
// every link in Cartograph.
var scopeKinds = map[string]bool{"Project": true, "Programme": true, "Operation": true}

func Rules(doc map[string]any, _ kit.RuleContext) []kit.Problem {
	spec, _ := doc["spec"].(map[string]any)
	if spec == nil {
		return nil
	}
	var problems []kit.Problem

	if scope, ok := spec["scope"].(map[string]any); ok {
		kind, _ := scope["kind"].(string)
		switch {
		case kind == "":
			// local and external scopes name nothing Cartograph can resolve to a
			// piece of work; the map would have nothing to be about.
			problems = append(problems, kit.Problem{
				Path:    "/spec/scope",
				Message: "a map is scoped to a project, programme or operation, by kind and id",
			})
		case !scopeKinds[kind]:
			problems = append(problems, kit.Problem{
				Path:    "/spec/scope/kind",
				Message: fmt.Sprintf("a map is scoped to work, not to a %s", kind),
			})
		}
	}

	entries, _ := spec["entries"].([]any)
	seen := map[string]int{}
	for i, e := range entries {
		em, ok := e.(map[string]any)
		if !ok {
			continue
		}
		resource, _ := em["resource"].(string)
		if resource == "" {
			continue
		}
		if first, dup := seen[resource]; dup {
			problems = append(problems, kit.Problem{
				Path:    fmt.Sprintf("/spec/entries/%d/resource", i),
				Message: fmt.Sprintf("%q is already scored at index %d; one score per stakeholder", resource, first),
			})
			continue
		}
		seen[resource] = i
	}
	return problems
}
