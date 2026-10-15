package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	codecyaml "github.com/ProjectCartograph/cartograph-engine/v2/internal/codec/yaml"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/kinds"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/render"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/reporting/computed"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store/memory"
)

func memoryEngine(t *testing.T) *engine.Engine {
	t.Helper()
	e, err := engine.New(memory.NewManifestStore(), memory.NewOperationalStore(), engine.WithCodec(codecyaml.New()), engine.WithLayout(graphLayout("layered")))
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// A version may be saved with fields not filled in yet (docs/adr/0035),
// so everything that reads versions reads one: every kind saved with an
// empty spec renders its charter, draws in the graph, counts in the
// reports, says what it lacks in its checks, and exports and imports as
// it was saved.
func TestEveryReaderReadsARecordNotFilledIn(t *testing.T) {
	ctx := context.Background()
	e := memoryEngine(t)
	if _, err := e.ImportDir(ctx, "../../examples/minimal", "test", "example"); err != nil {
		t.Fatal(err)
	}
	saved := map[string]string{}
	for _, kind := range kinds.Names() {
		id := "unfilled-" + strings.ToLower(kind)
		text := fmt.Appendf(nil, "apiVersion: cartograph/v1\nkind: %s\nmetadata:\n  id: %s\n  name: Not filled in\nspec: {}\n", kind, id)
		if _, err := e.Commit(ctx, kind, id, text, "test", "a start"); err != nil {
			var ve *engine.ValidationError
			if errors.As(err, &ve) {
				// What a kind refuses for a reason of its own (one of a
				// kind, a name it holds to) is not a field to fill.
				for _, p := range ve.Problems {
					if p.Incomplete() {
						t.Errorf("%s: an unfilled field refused the version: %+v", kind, p)
					}
				}
				t.Logf("%s: not saved empty, for its own rules: %v", kind, err)
				continue
			}
			t.Fatalf("%s: %v", kind, err)
		}
		saved[kind] = id
		if _, err := e.ChecksOf(ctx, kind, id, text); err != nil {
			t.Fatalf("%s checks: %v", kind, err)
		}
	}
	for _, k := range []string{"Project", "Programme", "Operation", "Goal", "KPI"} {
		if saved[k] == "" {
			t.Fatalf("%s was not saved empty", k)
		}
	}
	if _, _, err := render.Charter(ctx, e, saved["Project"], 0); err != nil {
		t.Errorf("project charter: %v", err)
	}
	if _, err := render.CharterParts(ctx, e, saved["Project"]); err != nil {
		t.Errorf("project charter parts: %v", err)
	}
	if _, err := render.CharterDiff(ctx, ctx, e, saved["Project"]); err != nil {
		t.Errorf("project charter diff: %v", err)
	}
	if _, err := render.ProgrammeCharter(ctx, e, saved["Programme"]); err != nil {
		t.Errorf("programme charter: %v", err)
	}
	if _, err := render.OperationCharter(ctx, e, saved["Operation"]); err != nil {
		t.Errorf("operation charter: %v", err)
	}
	if _, err := e.Graph(ctx, nil); err != nil {
		t.Errorf("graph: %v", err)
	}
	if _, err := e.Graph(ctx, &engine.Ref{Kind: "Project", ID: saved["Project"]}); err != nil {
		t.Errorf("graph about the project: %v", err)
	}
	r := computed.Reporter{E: e}
	for _, name := range r.Names() {
		if _, err := r.Run(ctx, name); err != nil {
			t.Errorf("report %s: %v", name, err)
		}
	}
	// Each kind's checks for people say what is not filled in.
	said := func(kind string, ids []string) {
		t.Helper()
		for _, id := range ids {
			if strings.HasPrefix(id, "required:") {
				return
			}
		}
		t.Errorf("%s: its checks do not say what is not filled in: %v", kind, ids)
	}
	idsOf := func(cs []engine.ProgrammeCheck, err error) []string {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, c := range cs {
			out = append(out, c.ID)
		}
		return out
	}
	said("Programme", idsOf(e.ProgrammeChecks(ctx, saved["Programme"])))
	said("Operation", idsOf(e.OperationChecks(ctx, saved["Operation"])))
	said("Portfolio", idsOf(e.PortfolioChecks(ctx, saved["Portfolio"])))
	goal, err := e.GoalChecks(ctx, saved["Goal"])
	if err != nil {
		t.Fatal(err)
	}
	var goalIDs []string
	for _, c := range goal {
		goalIDs = append(goalIDs, c.ID)
	}
	said("Goal", goalIDs)
	pc, err := e.ProjectChecks(ctx, saved["Project"], false)
	if err != nil {
		t.Fatal(err)
	}
	team := false
	for _, it := range pc.Items {
		team = team || (it.ID == "required:/spec/team" && it.State == "block")
	}
	if !team {
		t.Errorf("the project's checks do not hold its handoff for its team: %+v", pc.Items)
	}
	if _, err := e.TransitionProjectState(ctx, saved["Project"], engine.ProjectStateHandedOff, "test", "too soon"); err == nil {
		t.Error("a project with fields not filled in was handed off")
	}
	dir := t.TempDir()
	if err := e.ExportDir(ctx, dir); err != nil {
		t.Fatal(err)
	}
	again := memoryEngine(t)
	report, err := again.ImportDir(ctx, dir, "test", "round trip")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Problems) > 0 {
		t.Fatalf("an export with records not filled in does not import: %+v", report.Problems)
	}
	for kind, id := range saved {
		if _, err := again.Get(ctx, kind, id); err != nil {
			t.Errorf("%s/%s not imported: %v", kind, id, err)
		}
	}
}

// A malformed record is still refused as a version.
func TestAMalformedVersionIsRefused(t *testing.T) {
	ctx := context.Background()
	e := memoryEngine(t)
	text := []byte("apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: t-odd\n  name: Odd\nspec:\n  nonsense: not a field\n")
	if _, err := e.Commit(ctx, "Team", "t-odd", text, "test", "odd"); err == nil {
		t.Fatal("a malformed version was saved")
	}
	noName := []byte("apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: t-blank\n  name: \"\"\nspec: {}\n")
	if _, err := e.Commit(ctx, "Team", "t-blank", noName, "test", "blank"); err == nil {
		t.Fatal("a record with no name was saved")
	}
}
