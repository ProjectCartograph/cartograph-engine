package engine

import (
	"testing"
)

// A project that holds its dates and adjusts its cost: a late board on
// the pilot milestone, a supplier that may fail the app, and a cost risk
// read from a milestone's timing alone.
func triangleSpec() map[string]any {
	return map[string]any{
		"constraints":  map[string]any{"scope": "concede", "schedule": "hold", "cost": "adjust"},
		"milestones":   []any{map[string]any{"id": "pilot", "timing": map[string]any{"risks": []any{"fuel"}}}},
		"deliverables": []any{map[string]any{"id": "app"}},
		"risks": []any{
			map[string]any{"id": "board", "type": "risk", "description": "late board", "likelihood": "medium", "response": "accept",
				"affects": []any{map[string]any{"constraint": "schedule", "impact": "high", "on": "pilot"}}},
			map[string]any{"id": "supplier", "type": "risk", "description": "supplier fails", "likelihood": "low", "mitigation": "second supplier", "spends": "schedule",
				"affects": []any{map[string]any{"constraint": "scope", "impact": "medium", "on": "app"}, map[string]any{"constraint": "cost", "impact": "high"}}},
			map[string]any{"id": "fuel", "type": "issue", "description": "fuel prices", "impact": "low"},
			map[string]any{"id": "rain", "type": "risk", "description": "rain"},
		},
	}
}

func TestConstraintsWeighEachSide(t *testing.T) {
	t.Parallel()
	tri := ConstraintsOf(triangleSpec())
	got := map[string]Side{}
	for _, s := range tri.Sides {
		got[s.Constraint] = s
	}
	// schedule: board 2x3=6, fuel (an issue, so likely; read from the
	// milestone, low impact) 3x1=3; scope: supplier 1x2=2; cost 1x3=3.
	if got["schedule"].Exposure != 9 || got["scope"].Exposure != 2 || got["cost"].Exposure != 3 {
		t.Fatalf("exposure %+v", tri.Sides)
	}
	if tri.MostConstrained != "schedule" || got["schedule"].Stance != "hold" {
		t.Fatalf("most constrained %q, schedule %+v", tri.MostConstrained, got["schedule"])
	}
	if r := got["schedule"].Risks; len(r) != 2 || r[0].ID != "board" || !r[1].Implied || r[1].On != "pilot" {
		t.Fatalf("schedule risks %+v", r)
	}
	if got["schedule"].Unanswered != 1 { // fuel: neither response nor mitigation
		t.Fatalf("unanswered %+v", got["schedule"])
	}
	if len(tri.Unplaced) != 1 || tri.Unplaced[0] != "rain" {
		t.Fatalf("unplaced %v", tri.Unplaced)
	}
	if share := got["schedule"].Share; share != 0.643 {
		t.Fatalf("share %v", share)
	}
}

func TestConstraintChecksHoldTheStances(t *testing.T) {
	t.Parallel()
	pc := ProjectChecks{}
	addConstraintChecks(checkAdder{&pc}, triangleSpec())
	state := map[string]string{}
	for _, it := range pc.Items {
		state[it.ID] = it.State
	}
	want := map[string]string{
		"constraints-stated":   checkOK,
		"constraints-all-held": checkOK,
		"risks-constrained":    checkWarn, // rain names no side
		"risks-held-accepted":  checkWarn, // board is accepted on the held schedule
		"risks-spend-held":     checkWarn, // supplier's response spends the held schedule
	}
	for id, s := range want {
		if state[id] != s {
			t.Errorf("%s: %q, want %q (all: %v)", id, state[id], s, state)
		}
	}

	all := map[string]any{"constraints": map[string]any{"scope": "hold", "schedule": "hold", "cost": "hold"}}
	pc = ProjectChecks{}
	addConstraintChecks(checkAdder{&pc}, all)
	for _, it := range pc.Items {
		if it.ID == "constraints-all-held" && it.State != checkWarn {
			t.Errorf("all three held should warn: %+v", it)
		}
	}
}
