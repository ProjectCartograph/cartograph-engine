package engine_test

//lint:file-ignore SA1019 the merge-based shared draft is deprecated in 1.1.0 and removed in 2.0.0 (docs/adr/0007); until then this file still carries it.

import (
	"context"
	codecyaml "github.com/ProjectCartograph/cartograph-engine/internal/codec/yaml"
	"strings"
	"testing"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/internal/store/memory"
	"github.com/ProjectCartograph/cartograph-engine/pkg/merge"
)

func draftsEngine(t *testing.T) (*engine.Engine, *memory.ManifestStore) {
	t.Helper()
	ms := memory.NewManifestStore()
	e, err := engine.New(ms, memory.NewOperationalStore(), engine.WithOpLog(memory.NewOpLog()), engine.WithCodec(codecyaml.New()))
	if err != nil {
		t.Fatal(err)
	}
	return e, ms
}

func TestTwoActorsEditOneDraftAndTheWorkingCopyFollows(t *testing.T) {
	e, _ := draftsEngine(t)
	ctx := context.Background()
	mustCommit(t, e, "Goal", "g1", "seed", `apiVersion: cartograph/v1
kind: Goal
metadata:
  id: g1
  name: Raise quality
spec:
  level: goal
  objective: Deliveries meet grade.
  keyResults:
    - id: kr-1
      metric: graded A
      direction: increase
      kind: percent
`)
	d := e.Drafts()
	if d == nil {
		t.Fatal("an engine with an op log has drafts")
	}
	sub := e.Bus().Subscribe(ctx, "Goal", "g1")
	defer sub.Close()

	// Jo sets a target on the key result; Sam types inside the objective.
	_, err := d.Edit(ctx, "Goal", "g1", "jo", []merge.Op{{Path: "/spec/keyResults/{kr-1}/target", Value: 90}})
	if err != nil {
		t.Fatal(err)
	}
	st, _, err := d.Open(ctx, "Goal", "g1")
	if err != nil {
		t.Fatal(err)
	}
	h := &merge.HLC{Actor: "sam"}
	h.Observe(merge.Clock{Wall: time.Now().UnixMilli()})
	textOps := st.TextEdit("/spec/objective", "Deliveries meet the grade.", h)
	res, err := d.Edit(ctx, "Goal", "g1", "sam", textOps)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Conflicts) != 0 {
		t.Fatalf("different fields do not conflict: %+v", res.Conflicts)
	}

	// The working copy everything else reads holds both edits.
	text, found, err := e.GetWorking(ctx, "Goal", "g1")
	if err != nil || !found {
		t.Fatalf("working copy: %v %v", found, err)
	}
	if !strings.Contains(string(text), "target: 90") || !strings.Contains(string(text), "Deliveries meet the grade.") {
		t.Fatalf("working copy lacks an edit:\n%s", text)
	}

	// Both edits went out on the bus, in order.
	var seen int
	for seen < 2 {
		select {
		case ev := <-sub.C:
			if ev.Type == "ops" {
				seen++
			}
		case <-time.After(time.Second):
			t.Fatalf("expected two ops events, saw %d", seen)
		}
	}

	// A late client catches up from the log and lands on the same document.
	ops, err := d.Since(ctx, "Goal", "g1", 0)
	if err != nil || len(ops) < 2 {
		t.Fatalf("since: %d %v", len(ops), err)
	}
	late, _, _ := d.Open(ctx, "Goal", "g1")
	if late.Document()["spec"].(map[string]any)["objective"] != "Deliveries meet the grade." {
		t.Fatal("late open differs")
	}
}

func TestSameFieldEditIsANoteNotARefusal(t *testing.T) {
	e, _ := draftsEngine(t)
	ctx := context.Background()
	mustCommit(t, e, "Team", "t1", "seed", "apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: t1\n  name: Ops\nspec:\n  name: Ops\n  description: old\n")
	d := e.Drafts()
	st, _, _ := d.Open(ctx, "Team", "t1")
	base, _ := st.ClockAt("/spec/description")
	now := time.Now().UnixMilli()
	_, err := d.Edit(ctx, "Team", "t1", "jo", []merge.Op{{Path: "/spec/description", Value: "jo's", Clock: merge.Clock{Wall: now, Actor: "jo"}, Base: base}})
	if err != nil {
		t.Fatal(err)
	}
	res, err := d.Edit(ctx, "Team", "t1", "sam", []merge.Op{{Path: "/spec/description", Value: "sam's", Clock: merge.Clock{Wall: now + 1, Actor: "sam"}, Base: base}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Conflicts) != 1 || res.Conflicts[0].Loser.Value != "jo's" {
		t.Fatalf("expected a conflict note keeping jo's value: %+v", res.Conflicts)
	}
	if notes := d.Notes("Team", "t1"); len(notes) != 1 {
		t.Fatalf("notes: %d", len(notes))
	}
	text, _, _ := e.GetWorking(ctx, "Team", "t1")
	if !strings.Contains(string(text), "sam's") {
		t.Fatalf("the later write shows: %s", text)
	}
	// Saving a version takes the shared draft, clears the notes.
	v, err := e.Commit(ctx, "Team", "t1", text, "sam", "settled")
	if err != nil {
		t.Fatal(err)
	}
	if v.Number != 2 {
		t.Fatalf("version %d", v.Number)
	}
	if notes := d.Notes("Team", "t1"); len(notes) != 0 {
		t.Fatalf("notes should clear at a version: %d", len(notes))
	}
	if ops, _ := d.Since(ctx, "Team", "t1", 0); len(ops) != 0 {
		t.Fatalf("the log compacts at a version: %d left", len(ops))
	}
}
