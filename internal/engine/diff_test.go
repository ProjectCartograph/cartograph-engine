package engine_test

import (
	"context"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/internal/engine"
)

func changeAt(cs []engine.Change, path string) (engine.Change, bool) {
	for _, c := range cs {
		if c.Path == path {
			return c, true
		}
	}
	return engine.Change{}, false
}

func TestDiff(t *testing.T) {
	e := seededEngine(t)
	ctx := context.Background()

	v1 := "apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: t2\n  name: Team Two\nspec:\n  name: Team Two\n  description: original\n"
	if _, err := e.Commit(ctx, "Team", "t2", []byte(v1), "p1", "create"); err != nil {
		t.Fatal(err)
	}
	v2 := "apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: t2\n  name: Team Two Renamed\nspec:\n  name: Team Two Renamed\n  description: original\n  parent: t1\n"
	if _, err := e.Commit(ctx, "Team", "t2", []byte(v2), "p1", "rename and reparent"); err != nil {
		t.Fatal(err)
	}

	changes, err := e.Diff(ctx, "Team", "t2", 1, 2)
	if err != nil {
		t.Fatal(err)
	}

	if c, ok := changeAt(changes, "/metadata/name"); !ok || c.Op != "replace" {
		t.Fatalf("expected a replace at /metadata/name, got %+v (all: %+v)", c, changes)
	}
	if c, ok := changeAt(changes, "/spec/name"); !ok || c.Op != "replace" {
		t.Fatalf("expected a replace at /spec/name, got %+v", c)
	}
	if c, ok := changeAt(changes, "/spec/parent"); !ok || c.Op != "add" {
		t.Fatalf("expected an add at /spec/parent, got %+v", c)
	}
	if _, ok := changeAt(changes, "/spec/description"); ok {
		t.Fatal("did not expect a change for an untouched field")
	}
}

func TestDiffArraysByID(t *testing.T) {
	e := seededEngine(t)
	ctx := context.Background()

	v1 := "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: g5\n  name: Goal Five\nspec:\n  level: goal\n  objective: Original\n  keyResults:\n    - id: kr-1\n      metric: Original metric\n      direction: increase\n      kind: count\n      unit: crates\n    - id: kr-2\n      metric: Kept\n      direction: increase\n      kind: percent\n"
	if _, err := e.Commit(ctx, "Goal", "g5", []byte(v1), "p1", "create"); err != nil {
		t.Fatal(err)
	}
	v2 := "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: g5\n  name: Goal Five\nspec:\n  level: goal\n  objective: Original\n  keyResults:\n    - id: kr-2\n      metric: Kept\n      direction: increase\n      kind: percent\n    - id: kr-3\n      metric: New one\n      direction: increase\n      kind: count\n      unit: depots\n"
	if _, err := e.Commit(ctx, "Goal", "g5", []byte(v2), "p1", "swap key results"); err != nil {
		t.Fatal(err)
	}

	changes, err := e.Diff(ctx, "Goal", "g5", 1, 2)
	if err != nil {
		t.Fatal(err)
	}

	if c, ok := changeAt(changes, "/spec/keyResults/kr-1"); !ok || c.Op != "remove" {
		t.Fatalf("expected kr-1 removed by id, got %+v (all %+v)", c, changes)
	}
	if c, ok := changeAt(changes, "/spec/keyResults/kr-3"); !ok || c.Op != "add" {
		t.Fatalf("expected kr-3 added by id, got %+v", c)
	}
	if _, ok := changeAt(changes, "/spec/keyResults/kr-2"); ok {
		t.Fatal("kr-2 is unchanged and reordered only; id-based diff should not report it")
	}
}

func TestDiffArraysByIndex(t *testing.T) {
	e := seededEngine(t)
	ctx := context.Background()

	v1 := "apiVersion: cartograph/v1\nkind: KPI\nmetadata:\n  id: k5\n  name: KPI Five\nspec:\n  name: KPI Five\n  definition: A measure\n  unit: percent\n  direction: increase\n  source: d1\n  disaggregations: [seg-region, seg-gender]\n"
	if _, err := e.Commit(ctx, "KPI", "k5", []byte(v1), "p1", "create"); err != nil {
		t.Fatal(err)
	}
	v2 := "apiVersion: cartograph/v1\nkind: KPI\nmetadata:\n  id: k5\n  name: KPI Five\nspec:\n  name: KPI Five\n  definition: A measure\n  unit: percent\n  direction: increase\n  source: d1\n  disaggregations: [seg-region, seg-age]\n"
	if _, err := e.Commit(ctx, "KPI", "k5", []byte(v2), "p1", "swap disaggregation"); err != nil {
		t.Fatal(err)
	}

	changes, err := e.Diff(ctx, "KPI", "k5", 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if c, ok := changeAt(changes, "/spec/disaggregations/1"); !ok || c.Op != "replace" || c.From != "seg-gender" || c.To != "seg-age" {
		t.Fatalf("got %+v (all %+v)", c, changes)
	}
}
