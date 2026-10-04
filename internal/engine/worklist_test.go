package engine_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
)

// A piece of work walks the order once, from the top (TAXONOMY.md D28):
// the objective, then the outcome under it, then the indicator measuring
// it, then the gap that names both. Each is finished in one visit (what it
// is, its figures, its links), and a check that a later manifest settles
// by naming an earlier one waits for that later one. Nobody has to tell an
// agent this; the work says it.
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
	if w.Tasks[0].Kind != "Goal" || w.Tasks[0].ID != "obj-sound" {
		t.Fatalf("the work starts with %+v, want the objective", w.Tasks[0])
	}
	// One visit per manifest: once the work moves on from one, it never
	// comes back to it.
	left := map[string]bool{}
	for i, task := range w.Tasks {
		key := task.Kind + "/" + task.ID
		if left[key] {
			t.Fatalf("task %d goes back to %s: %+v", i, key, task)
		}
		if i > 0 {
			if prev := w.Tasks[i-1].Kind + "/" + w.Tasks[i-1].ID; prev != key {
				left[prev] = true
			}
		}
	}
	statement, target, aligned := index("Goal", "smart-specific"), index("KPI", "kpi-target"), index("KPI", "kpi-aligned")
	states, measured := index("Gap", "gap-states"), index("Gap", "gap-measured")
	if !(statement < target && target < aligned && aligned < states && states < measured) {
		t.Fatalf("out of order: outcome statement %d, KPI target %d, KPI aligned %d, gap states %d, gap measured %d", statement, target, aligned, states, measured)
	}
	if len(w.Tasks[measured].Choices) == 0 || len(w.Tasks[index("Gap", "gap-segments")].Choices) == 0 {
		t.Fatalf("linking a gap offers no existing KPIs or segments: %+v", w.Tasks[measured])
	}
	if w.Tasks[target].Do == "" || w.Open["measure"] == 0 || w.Open["align"] == 0 {
		t.Fatalf("a task says what to do, and the phases are counted: %+v %v", w.Tasks[target], w.Open)
	}
}
