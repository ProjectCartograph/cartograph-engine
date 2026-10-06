package engine_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
)

// Defining an objective leads to what comes after it, in the order of
// work (TAXONOMY.md D28): the outcomes under it, the indicator measuring
// each, then the gaps they close, each found somewhere. Everything is
// written after what it names, so nothing is opened again to be linked;
// the links come from the flows.
func TestThePlanForAnObjective(t *testing.T) {
	t.Parallel()
	e := newTestEngine(t)
	ctx := context.Background()
	if _, err := e.ImportDir(ctx, exampleDir(t), "alice-nkemah", "seed"); err != nil {
		t.Fatal(err)
	}
	plan, err := e.Plan(ctx, "Goal", "objective", "en")
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	at := map[string]engine.PlanItem{}
	for _, p := range plan {
		name := p.Kind
		if p.Level != "" {
			name += "/" + p.Level
		}
		order = append(order, name)
		at[name] = p
	}
	got := strings.Join(order, " ")
	for _, want := range []string{"Goal/outcome", "KPI", "Segment Gap"} {
		if !strings.Contains(got, want) {
			t.Fatalf("plan %q lacks %q", got, want)
		}
	}
	if !(strings.Index(got, "Goal/outcome") < strings.Index(got, "KPI") && strings.Index(got, "KPI") < strings.Index(got, "Gap")) {
		t.Fatalf("plan %q: the outcome comes first, then the indicator, then the gap that names both", got)
	}
	for _, p := range plan {
		if p.When != "after" {
			t.Fatalf("an objective names nothing the plan holds, so all of it comes after: %+v", p)
		}
	}
	if g := at["Gap"]; g.Check != "closes-gap" || g.Ask == "" || len(g.Existing) == 0 || g.For != "Goal (outcome)" {
		t.Fatalf("the gap step says too little: %+v", g)
	}
	if k := at["KPI"]; k.Check != "gap-measured" || k.For != "Gap" {
		t.Fatalf("the indicator step: %+v", k)
	}
}

// Whoever works on an outcome is told what the gap closing it still lacks,
// though both are drafts nobody has saved.
func TestTheWorkAroundAnOutcome(t *testing.T) {
	t.Parallel()
	e := newTestEngine(t)
	ctx := context.Background()
	if _, err := e.ImportDir(ctx, exampleDir(t), "alice-nkemah", "seed"); err != nil {
		t.Fatal(err)
	}
	outcome := []byte("apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: o-sound\n  name: Fruit arrives sound\nspec:\n  level: outcome\n  objective: Fruit arrives sound at every depot\n")
	gap := []byte("apiVersion: cartograph/v1\nkind: Gap\nmetadata:\n  id: gap-bruising\n  name: Bruised on arrival\nspec:\n  current: One crate in five arrives bruised\n  desired: Fewer than one in fifty arrive bruised\n  outcomes: [o-sound]\n")
	if err := e.SaveWorking(ctx, "Goal", "o-sound", outcome, "agent"); err != nil {
		t.Fatal(err)
	}
	if err := e.SaveWorking(ctx, "Gap", "gap-bruising", gap, "agent"); err != nil {
		t.Fatal(err)
	}
	around, err := e.WorkAround(ctx, "Goal", "o-sound", []engine.Ref{{Kind: "Gap", ID: "gap-bruising"}})
	if err != nil {
		t.Fatal(err)
	}
	var open []string
	for _, a := range around {
		if a.Kind == "Gap" && a.ID == "gap-bruising" {
			for _, c := range a.Open {
				open = append(open, c.ID)
			}
		}
	}
	got := strings.Join(open, " ")
	if !strings.Contains(got, "gap-measured") || !strings.Contains(got, "gap-segments") {
		t.Fatalf("the gap's open checks %q do not say it is unmeasured and unscoped (around: %+v)", got, around)
	}
}

// What a new thing names comes first, and the plan says which of those do
// not exist yet, so they are defined before it (TAXONOMY.md D31).
func TestThePlanSaysWhatIsMissing(t *testing.T) {
	t.Parallel()
	plan, err := newTestEngine(t).Plan(context.Background(), "KPI", "", "en")
	if err != nil {
		t.Fatal(err)
	}
	before := 0
	for _, p := range plan {
		if p.When != "before" {
			continue
		}
		before++
		if !p.Missing {
			t.Errorf("%s is not missing in an empty workspace: %+v", p.Kind, p)
		}
	}
	if before == 0 {
		t.Fatalf("a KPI names nothing before it: %+v", plan)
	}
}
