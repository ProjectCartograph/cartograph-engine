package mcp

import (
	"context"
	"strings"
	"testing"

	codecyaml "github.com/ProjectCartograph/cartograph-engine/v2/internal/codec/yaml"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store/memory"
)

// A task settled by a later stage (an outcome's gap) waits while an
// earlier stage has no record yet (no KPI to measure the gap): next names
// that stage first, and the task after it.
func TestATaskWaitsForTheStageThatSettlesIt(t *testing.T) {
	ctx := context.Background()
	ms := memory.NewManifestStore()
	e, err := engine.New(ms, memory.NewOperationalStore().LogTo(ms), engine.WithCodec(codecyaml.New()))
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []struct{ kind, id, spec string }{
		{"Purpose", "default", "  organisation: Example Cooperative\n  vision: Every member's produce reaches a buyer sound.\n  mission: We check, store and move produce.\n"},
		{"Goal", "gg", "  level: goal\n  objective: Raise produce quality\n"},
		{"Goal", "oo", "  level: objective\n  parent: gg\n  objective: Hold every depot to one standard\n"},
		{"Goal", "cc", "  level: outcome\n  parent: oo\n  objective: Depot staff apply the standard alike\n"},
	} {
		text := "apiVersion: cartograph/v1\nkind: " + m.kind + "\nmetadata:\n  id: " + m.id + "\n  name: " + m.id + "\nspec:\n" + m.spec
		if _, err := e.Commit(ctx, m.kind, m.id, []byte(text), "seed", "seed"); err != nil {
			t.Fatalf("%s: %v", m.id, err)
		}
	}
	task := engine.Task{Phase: "align", Kind: "Goal", ID: "cc", Name: "cc", Check: "closes-gap", Message: "Closes no gap yet.", By: "Gap"}
	if got := firstNext(ctx, e, task); !strings.Contains(got, "the kpi stage") || !strings.Contains(got, "After it:") {
		t.Fatalf("next for a gap with no KPI yet: %s", got)
	}
	task.By = ""
	if got := firstNext(ctx, e, task); strings.Contains(got, "the kpi stage") {
		t.Fatalf("a task of its own manifest waited on a stage: %s", got)
	}
}
