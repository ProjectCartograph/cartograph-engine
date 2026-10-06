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

	// An entry names a resource or a beneficiary group, exactly one
	// (TAXONOMY.md D42), and each is scored once.
	entries, _ := spec["entries"].([]any)
	seen := map[string]int{}
	for i, e := range entries {
		em, ok := e.(map[string]any)
		if !ok {
			continue
		}
		path := fmt.Sprintf("/spec/entries/%d", i)
		resource, _ := em["resource"].(string)
		group, _ := em["group"].(string)
		switch {
		case resource != "" && group != "":
			problems = append(problems, kit.Problem{Path: path, Message: "an entry names a resource or a beneficiary group, not both"})
			continue
		case resource == "" && group == "":
			problems = append(problems, kit.Problem{Path: path, Message: "an entry names the resource or the beneficiary group it scores"})
			continue
		}
		// The map holds no roles of its own, so its owner is a catalogue
		// role or an external one, never the local form.
		if owner, ok := em["owner"].(map[string]any); ok {
			if local, _ := owner["local"].(string); local != "" {
				problems = append(problems, kit.Problem{Path: path + "/owner", Message: "name the owner from the Resource catalogue, or as an external role"})
			}
		}
		field, key := "resource", "Resource/"+resource
		if group != "" {
			field, key = "group", "BeneficiaryGroup/"+group
		}
		if first, dup := seen[key]; dup {
			problems = append(problems, kit.Problem{
				Path:    path + "/" + field,
				Message: fmt.Sprintf("%q is already scored at index %d; one score per stakeholder", resource+group, first),
			})
			continue
		}
		seen[key] = i
	}
	return problems
}
