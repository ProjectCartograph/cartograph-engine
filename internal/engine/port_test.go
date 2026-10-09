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
		Open:   []engine.OpenReason{{Check: "resources-funding", Reason: "The charter names no funding"}}, Asked: "not available",
	}})
	if err != nil || len(results) != 1 || results[0].Error != "" || strings.Join(results[0].Left, ",") != "resources-funding" {
		t.Fatalf("records %+v %v", results, err)
	}
	if len(still) == 0 || still[0].Record != project {
		t.Fatalf("still open %+v", still)
	}
	// With the person asked, a check is left with what they answered,
	// even one a document states; without, it says nobody was asked
	// (docs/adr/0032).
	asked := "Asked who mandated it; they will name the decision at review"
	if results, _, err = e.PortRecords(ctx, cs.ID, []engine.PortRecord{{
		Record: project, Asked: asked,
		Open: []engine.OpenReason{{Check: "aim-mandate", Reason: "The mandate is named at review"}},
	}}); err != nil || strings.Join(results[0].Left, ",") != "aim-mandate" {
		t.Fatalf("left with the person asked: %+v %v", results, err)
	}
	view, err := e.ViewChangeSet(ctx, cs.ID)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, w := range view.ChangeSet.Waivers {
		got[w.Check] = w.Asked
	}
	if got["resources-funding"] != "not available" || got["aim-mandate"] != asked {
		t.Fatalf("what was asked, by check: %v", got)
	}
	// Without asked a check is not left, never marked unasked by default;
	// an empty reason takes back one left before (eval run 002).
	results, _, err = e.PortRecords(ctx, cs.ID, []engine.PortRecord{{
		Record: project,
		Open:   []engine.OpenReason{{Check: "owner-role", Reason: "Nobody named"}, {Check: "resources-funding"}},
	}})
	if err != nil || len(results[0].Left) != 0 || len(results[0].NotLeft) != 1 || !strings.Contains(results[0].NotLeft[0], "pass asked") ||
		strings.Join(results[0].TakenBack, ",") != "resources-funding" {
		t.Fatalf("left without asked, taken back: %+v %v", results, err)
	}
	if view, _ = e.ViewChangeSet(ctx, cs.ID); len(view.ChangeSet.Waivers) != 1 || view.ChangeSet.Waivers[0].Check != "aim-mandate" {
		t.Fatalf("left after taking one back: %+v", view.ChangeSet.Waivers)
	}
}

// Records created in one call may name each other by id, in either
// order, and no duplicate is drafted under an id read as a name.
func TestPortRecordsNameEachOtherByID(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	ctx := actingAs(ada)
	cs, err := e.StartChangeSet(ctx, "Port", "")
	if err != nil {
		t.Fatal(err)
	}
	results, _, err := e.PortRecords(ctx, cs.ID, []engine.PortRecord{
		{Record: "Goal/goal-child", Set: map[string]any{"/metadata/name": "Every depot grades the same", "/spec/level": "outcome", "/spec/parent": "goal-root"}},
		{Record: "Goal/goal-root", Set: map[string]any{"/metadata/name": "Produce is graded fairly", "/spec/level": "strategic"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.Error != "" {
			t.Fatalf("%s: %s", r.Record, r.Error)
		}
	}
	view, err := e.ViewChangeSet(ctx, cs.ID)
	if err != nil {
		t.Fatal(err)
	}
	goals := 0
	for _, it := range view.Items {
		if it.Item.Kind == "Goal" {
			goals++
		}
	}
	if goals != 2 {
		t.Errorf("%d goals drafted, want 2: an id was read as a name", goals)
	}
}
