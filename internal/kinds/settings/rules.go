// Package settings implements the Settings kind's one rule beyond its JSON
// Schema: it is the only singleton kind, so its metadata.id must always be
// "default".
package settings

import "github.com/ProjectCartograph/cartograph-engine/internal/kinds/kit"

func Rules(doc map[string]any, _ kit.RuleContext) []kit.Problem {
	meta, _ := doc["metadata"].(map[string]any)
	id, _ := meta["id"].(string)
	if id != "default" {
		return []kit.Problem{{
			Path:    "/metadata/id",
			Message: "Settings is a singleton; its id must be \"default\"",
		}}
	}
	return nil
}
