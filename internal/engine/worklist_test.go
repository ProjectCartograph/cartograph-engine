package engine_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
)

// A piece of work is settled in phases, and within each, what a thing
// answers to before the thing: every gap's states before the outcomes, the
// indicators' figures after everything is defined, and the links last. Nobody
// has to tell an agent this; the work says it.
func TestTheOrderOfWork(t *testing.T) {
	e := newTestEngine(t)
	ctx := context.Background()
	if _, err := e.ImportDir(ctx, exampleDir(t), "alice-nkemah", "seed"); err != nil {
		t.Fatal(err)
	}
	drafts := map[string]string{
		"Goal/obj-sound": "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: obj-sound\n  name: Deliver sound fruit\nspec:\n  level: objective\n  objective: Deliver sound fruit to every buyer\n",
		"Goal/o-sound":   "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: o-sound\n  name: Fruit arrives sound\nspec:\n  level: outcome\n  parent: obj-sound\n",
		"Gap/gap-bruise": "apiVersion: cartograph/v1\nkind: Gap\nmetadata:\n  id: gap-bruise\n  name: Bruised on arrival\nspec:\n  outcomes: [o-sound]\n",
		"KPI/k-sound":    "apiVersion: cartograph/v1\nkind: KPI\nmetadata:\n  id: k-sound\n  name: Sound on arrival\nspec:\n  definition: Share of crates sound at intake\n  unit: percent\n  direction: increase\n  source: depot-intake-log\n",
	}
	var work []engine.Ref
	for key, text := range drafts {
		kind, id, _ := strings.Cut(key, "/")
		if err := e.SaveWorking(ctx, kind, id, []byte(text), "agent"); err != nil {
			t.Fatal(err)
		}
		work = append(work, engine.Ref{Kind: kind, ID: id})
	}
	w, err := e.Work(ctx, work, "en")
	if err != nil {
		t.Fatal(err)
	}
	index := func(kind, check string) int {
		for i, task := range w.Tasks {
			if task.Kind == kind && task.Check == check {
				return i
			}
		}
		var all []string
		for _, task := range w.Tasks {
			all = append(all, fmt.Sprintf("%s:%s/%s:%s", task.Phase, task.Kind, task.ID, task.Check))
		}
		t.Fatalf("no %s %s in %v", kind, check, all)
		return -1
	}
	if testing.Verbose() {
		for i, task := range w.Tasks {
			fmt.Printf("%2d %-7s %-5s %-18s %-22s %s\n", i+1, task.Phase, task.Kind, task.Name, task.Check, task.Message)
		}
	}
	if w.Tasks[0].Kind != "Gap" || w.Tasks[0].Phase != "define" {
		t.Fatalf("the work starts with %+v, want the gap's definition", w.Tasks[0])
	}
	states, statement := index("Gap", "gap-states"), index("Goal", "smart-specific")
	target, measured, aligned := index("KPI", "kpi-target"), index("Gap", "gap-measured"), index("KPI", "kpi-aligned")
	if !(states < statement && statement < target && target < measured && target < aligned) {
		t.Fatalf("out of order: gap states %d, outcome statement %d, KPI target %d, gap measured %d, KPI aligned %d", states, statement, target, measured, aligned)
	}
	if w.Tasks[target].Do == "" || w.Open["measure"] == 0 || w.Open["align"] == 0 {
		t.Fatalf("a task says what to do, and the phases are counted: %+v %v", w.Tasks[target], w.Open)
	}
}
