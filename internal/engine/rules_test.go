package engine_test

import (
	"testing"
)

func TestGoalParentPresenceByLevel(t *testing.T) {
	e := seededEngine(t)
	runSchemaCases(t, e, []schemaCase{
		{
			name: "pillar must not have a parent",
			kind: "Goal", wantProblem: true, wantSubstr: "must not have a parent",
			yaml: "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: g3\n  name: Goal Three\nspec:\n  level: goal\n  parent: g1\n  objective: Something\n",
		},
		{
			name: "strategic goal requires a pillar parent",
			kind: "Goal", wantProblem: true, wantSubstr: "requires a goal",
			yaml: "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: g3\n  name: Goal Three\nspec:\n  level: objective\n  objective: Something\n",
		},
	})
}
