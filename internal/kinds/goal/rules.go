// Package goal implements the Goal kind's rules beyond its JSON Schema:
// parent presence by level (a goal has no parent; an objective
// requires a goal as parent; an outcome requires an objective
// goal as parent), unique key result ids within the goal, the key result
// unit-by-kind rule, and the objective must-be-qualitative rule. The goal
// is the root of the dependency tree: nothing here ever checks a
// downward reference, since Goal itself carries none.
package goal

import (
	"fmt"

	"github.com/ProjectCartograph/cartograph-engine/internal/kinds/kit"
)

func Rules(doc map[string]any, ctx kit.RuleContext) []kit.Problem {
	var problems []kit.Problem

	spec, _ := doc["spec"].(map[string]any)
	if spec == nil {
		return problems
	}

	if objective, ok := spec["objective"].(string); ok {
		if p := kit.ObjectiveDigitProblem(objective, "/spec/objective"); p != nil {
			problems = append(problems, *p)
		}
	}

	level, _ := spec["level"].(string)
	_, hasParent := spec["parent"]
	switch level {
	case "goal":
		if hasParent {
			problems = append(problems, kit.Problem{
				Path:    "/spec/parent",
				Message: "a goal must not have a parent",
			})
		}
	case "objective":
		if !hasParent {
			problems = append(problems, kit.Problem{
				Path:    "/spec/parent",
				Message: "an objective requires a goal as its parent",
			})
		} else {
			// Verify parent is a goal
			if p := checkParentLevel(ctx, spec, "goal"); p != nil {
				problems = append(problems, *p)
			}
		}
	case "outcome":
		if !hasParent {
			problems = append(problems, kit.Problem{
				Path:    "/spec/parent",
				Message: "an outcome requires an objective as its parent",
			})
		} else {
			// Verify parent is an objective
			if p := checkParentLevel(ctx, spec, "objective"); p != nil {
				problems = append(problems, *p)
			}
		}
	}

	// Check that level doesn't change once goal has snapshots
	if p := checkLevelImmutable(ctx, level); p != nil {
		problems = append(problems, *p)
	}

	if krs, ok := spec["keyResults"].([]any); ok {
		seen := map[string]int{}
		for i, kr := range krs {
			m, ok := kr.(map[string]any)
			if !ok {
				continue
			}
			id, _ := m["id"].(string)
			if id == "" {
				continue
			}
			if first, dup := seen[id]; dup {
				problems = append(problems, kit.Problem{
					Path:    fmt.Sprintf("/spec/keyResults/%d/id", i),
					Message: fmt.Sprintf("key result id %q duplicates the one at index %d", id, first),
				})
			} else {
				seen[id] = i
			}
			if p := kit.KeyResultUnitProblem(m, fmt.Sprintf("/spec/keyResults/%d", i)); p != nil {
				problems = append(problems, *p)
			}
		}
	}

	return problems
}

// checkParentLevel verifies that the parent goal has the expected level.
func checkParentLevel(ctx kit.RuleContext, spec map[string]any, expectedLevel string) *kit.Problem {
	parentID, ok := spec["parent"].(string)
	if !ok || parentID == "" {
		return nil
	}

	docs, err := ctx.Lookup.Documents("Goal")
	if err != nil {
		return nil
	}

	parentDoc, ok := docs[parentID]
	if !ok {
		// Parent doesn't exist; reference check will catch this
		return nil
	}

	parentSpec, _ := parentDoc["spec"].(map[string]any)
	if parentSpec == nil {
		return nil
	}

	parentLevel, _ := parentSpec["level"].(string)
	if parentLevel != expectedLevel {
		return &kit.Problem{
			Path:    "/spec/parent",
			Message: fmt.Sprintf("parent goal %q is level %q, not %q", parentID, parentLevel, expectedLevel),
		}
	}
	return nil
}

// checkLevelImmutable verifies that a goal's level doesn't change once it has snapshots.
func checkLevelImmutable(ctx kit.RuleContext, newLevel string) *kit.Problem {
	if ctx.ID == "" || newLevel == "" {
		return nil
	}

	hasSnapshots, err := ctx.Lookup.HasSnapshots("Goal", ctx.ID)
	if err != nil {
		return nil
	}

	if !hasSnapshots {
		// No snapshots yet, level can change
		return nil
	}

	// Goal has snapshots; check if level is changing
	docs, err := ctx.Lookup.Documents("Goal")
	if err != nil {
		return nil
	}

	currentDoc, ok := docs[ctx.ID]
	if !ok {
		// Goal not found; shouldn't happen during update
		return nil
	}

	currentSpec, _ := currentDoc["spec"].(map[string]any)
	if currentSpec == nil {
		return nil
	}

	currentLevel, _ := currentSpec["level"].(string)
	if currentLevel != "" && currentLevel != newLevel {
		return &kit.Problem{
			Path:    "/spec/level",
			Message: fmt.Sprintf("a goal keeps its level: %q is level %q and cannot change to %q", ctx.ID, currentLevel, newLevel),
		}
	}
	return nil
}
