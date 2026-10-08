package engine_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/structure"
)

// A port is the engine's use case, driven without any adapter: a
// document's pieces laid out with its register written, then every
// record written in one pass, with what each still needs.
func TestAPortIsAnEngineUseCase(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	if _, err := e.SeedStandardUnits(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx := actingAs(ada)
	cs, err := e.StartChangeSet(ctx, "Port the depot charter", "")
	if err != nil {
		t.Fatal(err)
	}
	doc := "Depot Checks Charter\n\nG1. Milestone Plan\n" +
		"    No.     Milestone                    Owner            Dependency     Completion Evidence\n\n" +
		"    M1      Checklist agreed             Quality team                    Completed - minutes on file\n\n" +
		"    M2      Graders trained              Quality team                    Planned\n\n" +
		"    M3      Every depot graded           Inspection unit  M2             Planned\n\n" +
		"    M4      Disputes reviewed            Quality team     M3             Planned\n"
	if err := engine.WholeFile(doc[:20], len(doc)); err == nil {
		t.Error("a part of the file was taken as the whole")
	}
	src, err := e.BringSource(ctx, cs.ID, "Depot Checks Charter", doc)
	if err != nil {
		t.Fatal(err)
	}
	layout, err := e.PortPieces(ctx, cs.ID, src, []structure.Piece{{Name: "Depot checks", None: true}})
	if err != nil || len(layout.Problems) > 0 {
		t.Fatalf("layout: %v %v", err, layout.Problems)
	}
	if len(layout.Registers) != 1 || layout.Registers[0].Added != 3 || len(layout.Registers[0].NotPorted) != 1 {
		t.Fatalf("registers %+v", layout.Registers)
	}
	project := layout.Structure.Work[0]
	results, still, err := e.PortRecords(ctx, cs.ID, []engine.PortRecord{{
		Record: project,
		Set:    map[string]any{"/spec/objectives/0/objective": "Produce is graded the same at every depot"},
		Open:   []engine.OpenReason{{Check: "resources-funding", Reason: "The charter names no funding"}},
	}})
	if err != nil || len(results) != 1 || results[0].Error != "" || strings.Join(results[0].Left, ",") != "resources-funding" {
		t.Fatalf("records %+v %v", results, err)
	}
	if len(still) == 0 || still[0].Record != project {
		t.Fatalf("still open %+v", still)
	}
}
