// Package purpose implements the Purpose kind's one rule beyond its JSON
// Schema: a workspace states one purpose, so its metadata.id is always
// "default".
package purpose

import "github.com/ProjectCartograph/cartograph-engine/v2/internal/kinds/kit"

func Rules(doc map[string]any, _ kit.RuleContext) []kit.Problem {
	meta, _ := doc["metadata"].(map[string]any)
	if id, _ := meta["id"].(string); id != "default" {
		return []kit.Problem{{Path: "/metadata/id", Message: "a workspace states one purpose; its id must be \"default\""}}
	}
	return nil
}
