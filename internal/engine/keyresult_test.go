package engine_test

import "testing"

// KeyResult (common.schema.json) is a shared, typed definition embedded in
// both Goal.spec.keyResults and Project.spec.objectives[].keyResults.
// These cases exercise it through Goal, which is the simpler host, per the
// second I0 contract delta (2026-09-16): direction and kind are now
// required, unit is conditionally required by kind (a kind rule, not
// schema), and baseline is one of two typed shapes (known value, or an
// admitted unknown).
func TestKeyResultKindAndUnit(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	base := "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: g4\n  name: Goal Four\nspec:\n  level: goal\n  objective: Something measurable\n  keyResults:\n"

	runSchemaCases(t, e, []schemaCase{
		{
			name: "valid, kind count requires and has a unit",
			kind: "Goal",
			yaml: base + "    - id: kr-1\n      metric: Deliveries checked\n      direction: increase\n      kind: count\n      unit: deliveries\n",
		},
		{
			name: "valid, kind percent forbids and lacks a unit",
			kind: "Goal",
			yaml: base + "    - id: kr-1\n      metric: Quality rate\n      direction: increase\n      kind: percent\n",
		},
		{
			name: "missing direction", kind: "Goal", wantProblem: true, wantSubstr: "direction",
			yaml: base + "    - id: kr-1\n      metric: Deliveries checked\n      kind: count\n      unit: deliveries\n",
		},
		{
			name: "missing kind", kind: "Goal", wantProblem: true, wantSubstr: "kind",
			yaml: base + "    - id: kr-1\n      metric: Deliveries checked\n      direction: increase\n      unit: deliveries\n",
		},
		{
			name: "wrong enum direction", kind: "Goal", wantProblem: true,
			yaml: base + "    - id: kr-1\n      metric: Deliveries checked\n      direction: sideways\n      kind: count\n      unit: deliveries\n",
		},
		{
			name: "wrong enum kind", kind: "Goal", wantProblem: true,
			yaml: base + "    - id: kr-1\n      metric: Deliveries checked\n      direction: increase\n      kind: distance\n      unit: deliveries\n",
		},
		{
			name: "unit missing when kind is count", kind: "Goal", wantProblem: true, wantSubstr: "unit is required",
			yaml: base + "    - id: kr-1\n      metric: Deliveries checked\n      direction: increase\n      kind: count\n",
		},
		{
			name: "unit missing when kind is money", kind: "Goal", wantProblem: true, wantSubstr: "unit is required",
			yaml: base + "    - id: kr-1\n      metric: Spend\n      direction: decrease\n      kind: money\n",
		},
		{
			name: "unit missing when kind is duration", kind: "Goal", wantProblem: true, wantSubstr: "unit is required",
			yaml: base + "    - id: kr-1\n      metric: Time to place\n      direction: decrease\n      kind: duration\n",
		},
		{
			name: "unit present when kind is percent", kind: "Goal", wantProblem: true, wantSubstr: "unit is not allowed",
			yaml: base + "    - id: kr-1\n      metric: Quality rate\n      direction: increase\n      kind: percent\n      unit: percent\n",
		},
		{
			name: "unit present when kind is ratio", kind: "Goal", wantProblem: true, wantSubstr: "unit is not allowed",
			yaml: base + "    - id: kr-1\n      metric: Ratio of pass to fail\n      direction: reach\n      kind: ratio\n      unit: ratio\n",
		},
		{
			name: "baseline as a known value", kind: "Goal",
			yaml: base + "    - id: kr-1\n      metric: Quality rate\n      direction: increase\n      kind: percent\n      baseline: {value: 62, date: \"2025-09\"}\n",
		},
		{
			name: "baseline as an admitted unknown", kind: "Goal",
			yaml: base + "    - id: kr-1\n      metric: Quality rate\n      direction: increase\n      kind: percent\n      baseline: {unknownReason: not measured before this programme, expectedBy: \"2025-12\"}\n",
		},
		{
			name: "baseline mixing both shapes is invalid", kind: "Goal", wantProblem: true,
			yaml: base + "    - id: kr-1\n      metric: Quality rate\n      direction: increase\n      kind: percent\n      baseline: {value: 62, date: \"2025-09\", unknownReason: also unknown}\n",
		},
		{
			name: "baseline missing date is invalid", kind: "Goal", wantProblem: true,
			yaml: base + "    - id: kr-1\n      metric: Quality rate\n      direction: increase\n      kind: percent\n      baseline: {value: 62}\n",
		},
	})
}
