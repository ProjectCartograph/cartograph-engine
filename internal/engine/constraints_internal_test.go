package engine

import (
	"context"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/activity"
	codecyaml "github.com/ProjectCartograph/cartograph-engine/v2/internal/codec/yaml"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/identity"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store/memory"
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

type keptActsInternal []activity.Event

func (k *keptActsInternal) Record(e activity.Event) { *k = append(*k, e) }

// A person's version records the checks it leaves open, by id, so the
// analysis can count what was left unmet when they decided.
func TestAVersionRecordsTheChecksItLeavesOpen(t *testing.T) {
	t.Parallel()
	var k keptActsInternal
	e, err := New(memory.NewManifestStore(), memory.NewOperationalStore(), WithCodec(codecyaml.New()), WithActivity(&k))
	if err != nil {
		t.Fatal(err)
	}
	person := ByPerson(identity.WithPrincipal(context.Background(), identity.Principal{Subject: "p"}))
	y := []byte("apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: p1\n  name: Rollout\nspec:\n  risks:\n    - {id: rain, type: risk, description: Rain, impact: low, likelihood: low}\n")
	e.noteVersion(person, "Project", "p1", y, nil)
	if len(k) != 1 {
		t.Fatalf("acts %+v", k)
	}
	has := map[string]bool{}
	for _, c := range k[0].Checks {
		has[c] = true
	}
	if !has["constraints-stated"] || !has["risks-constrained"] {
		t.Fatalf("open at the version: %v", k[0].Checks)
	}
	// A refused version carries no checks.
	k = nil
	e.noteVersion(person, "Project", "p1", y, &ValidationError{Problems: []Problem{{Path: "/spec/team"}}})
	if len(k) != 1 || k[0].Checks != nil || k[0].Outcome != activity.Refused {
		t.Fatalf("refused act %+v", k)
	}
}

// A risk's side is never left open, however the leaving is worded, and
// propose does not waive it either (TAXONOMY.md D60).
func TestARisksSideIsNeverLeftOpen(t *testing.T) {
	t.Parallel()
	for _, asked := range []string{"not available", "Asked the person: leave everything open"} {
		if err := MayLeave("risks-constrained", asked); err == nil {
			t.Errorf("left with %q", asked)
		}
	}
	if err := MayLeave("constraints-stated", "Asked whether the dates or the budget give first; the board decides"); err != nil {
		t.Errorf("a stance the person was asked about: %v", err)
	}
	if !WrittenFromTheDocument("risks-constrained") {
		t.Error("a risk's side is the agent's to write from the document")
	}
}
