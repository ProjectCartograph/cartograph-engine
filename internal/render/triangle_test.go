package render

import (
	"context"
	"testing"

	codecyaml "github.com/ProjectCartograph/cartograph-engine/v2/internal/codec/yaml"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store/memory"
)

// The charter draws the triple constraint, names the side that carries
// the most weighted risk, says what each key risk would move, and links
// both sections to the risks step.
func TestCharterDrawsTheTripleConstraint(t *testing.T) {
	ctx := context.Background()
	e, err := engine.New(memory.NewManifestStore(), memory.NewOperationalStore(), engine.WithCodec(codecyaml.New()))
	if err != nil {
		t.Fatal(err)
	}
	projectYAML := `apiVersion: cartograph/v1
kind: Project
metadata:
  id: test-project
  name: Test Project
spec:
  team: test-team
  constraints: {scope: adjust, schedule: hold, cost: concede}
  milestones:
    - {id: pilot, name: Pilot at two depots}
  risks:
    - id: board
      description: The board meets after the season opens
      type: risk
      impact: high
      likelihood: medium
      affects: [{constraint: schedule, impact: high, on: pilot}]
      response: mitigate
      mitigation: Put the results to the board a month early
    - {id: forms, description: Forms run short, type: risk, impact: low, likelihood: low, affects: [{constraint: cost, impact: low}]}
`
	if err := e.PutWorking(ctx, "Project", "test-project", []byte(projectYAML)); err != nil {
		t.Fatal(err)
	}
	html, _, err := Charter(ctx, e, "test-project", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`data-step="risks">Scope, schedule and cost</h2>`,
		`class="tri-node hold most"`,
		"Schedule carries the most weighted risk.",
		"It is also the side held",
		"<th>Moves</th>",
		"Schedule (high, Pilot at two depots)",
		"Mitigate: Put the results to the board a month early",
		`data-step="risks">Key risks</h2>`,
	} {
		if !contains(string(html), want) {
			t.Errorf("charter lacks %q", want)
		}
	}
}

// A project that has said nothing about the triangle gets no section.
func TestCharterLeavesOutAnUnsaidTriangle(t *testing.T) {
	spec := map[string]any{"risks": []any{map[string]any{"id": "r", "type": "risk", "description": "x"}}}
	var d doc
	d.constraintsBrief(spec)
	d.flush()
	if d.b.Len() != 0 {
		t.Fatalf("printed %q", d.b.String())
	}
}
