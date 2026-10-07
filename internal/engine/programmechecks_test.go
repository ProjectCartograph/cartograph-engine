package engine_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store/memory"
)

func programmeChecksByID(t *testing.T, e *engine.Engine, id string) map[string]engine.ProgrammeCheck {
	t.Helper()
	checks, err := e.ProgrammeChecks(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]engine.ProgrammeCheck{}
	for _, c := range checks {
		out[c.ID] = c
	}
	return out
}

// The membership read-back is the check the whole programme surface rests
// on: a programme coordinating nothing is a heading. It is derived, so the
// programme is never edited to gain a member.
func TestProgrammeChecksMembership(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)

	mustCommit(t, e, "Programme", "p1", "local", programmeYAML("p1", ""))
	if got := programmeChecksByID(t, e, "p1")["components-present"]; got.State != "warn" {
		t.Fatalf("a programme nothing names should warn, got %+v", got)
	}

	// A project joins by naming it, on the project's own manifest.
	mustCommit(t, e, "Project", "proj", "local",
		projectYAML("proj", "  alignment:\n    goals: [g1-f]\n    programmes: [p1]\n"))
	got := programmeChecksByID(t, e, "p1")["components-present"]
	if got.State != "ok" || got.Message != "Components: 1 project." {
		t.Fatalf("unexpected membership check: %+v", got)
	}

	// And an operation the same way, one level up from its own spec.
	mustCommit(t, e, "Operation", "op", "local",
		"apiVersion: cartograph/v1\nkind: Operation\nmetadata:\n  id: op\n  name: Ops\n"+
			"spec:\n  name: Ops\n  purpose: Keeping the lights on\n  team: t1\n  programmes: [p1]\n")
	got = programmeChecksByID(t, e, "p1")["components-present"]
	if got.State != "ok" || got.Message != "Components: 1 project and 1 operation." {
		t.Fatalf("unexpected membership check: %+v", got)
	}

	// Naming a different programme is not membership in this one.
	mustCommit(t, e, "Programme", "p2", "local", programmeYAML("p2", ""))
	if got := programmeChecksByID(t, e, "p2")["components-present"]; got.State != "warn" {
		t.Fatalf("p2 has no members, got %+v", got)
	}
}

// A programme is judged on beneficial change, so the checks ask whether
// there is a change to judge and a way to judge it.
func TestProgrammeChecksJudgeable(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	mustCommit(t, e, "Programme", "bare", "local", programmeYAML("bare", ""))
	bare := programmeChecksByID(t, e, "bare")
	for _, id := range []string{"problems-stated", "alignment-goals", "alignment-kpis"} {
		if bare[id].State != "warn" {
			t.Fatalf("expected %s to warn on a bare programme, got %+v", id, bare[id])
		}
	}
	// Nothing is reported about the required fields: the schema already
	// refuses a programme without them.
	for _, id := range []string{"aim-present", "name-present", "teams-lead"} {
		if _, ok := bare[id]; ok {
			t.Fatalf("%s repeats a refusal the schema already makes", id)
		}
	}
	// No risks means no risk checks, rather than a reassurance about none.
	for _, id := range []string{"risks-mitigation", "risks-dependency-edges", "risks-dependency-cycle"} {
		if _, ok := bare[id]; ok {
			t.Fatalf("%s reported on a programme with no risks", id)
		}
	}

	// The gap has to exist before a problem may cite it.
	mustCommit(t, e, "Gap", "gap1", "local",
		"apiVersion: cartograph/v1\nkind: Gap\nmetadata:\n  id: gap1\n  name: Gap One\n"+
			"spec:\n  statement: Check results are not returned to depots\n")
	mustCommit(t, e, "Programme", "full", "local", programmeYAML("full",
		"  goals: [g1-f]\n  kpis: [k1]\n"+
			"  problems:\n    - problem: {situation: Results are not read back}\n      change: {what: Results reach the people who act on them}\n"+
			"      groups: [bg1]\n      gaps: [{gap: gap1}]\n"))
	full := programmeChecksByID(t, e, "full")
	for _, id := range []string{"problems-stated", "problems-groups", "problems-gaps", "alignment-goals", "alignment-kpis"} {
		if full[id].State != "ok" {
			t.Fatalf("expected %s to pass, got %+v", id, full[id])
		}
	}

	// A problem with neither citation nor group is reported as both, in
	// the singular.
	mustCommit(t, e, "Programme", "thin", "local", programmeYAML("thin",
		"  problems:\n    - problem: {situation: Something is wrong}\n      change: {what: It is put right}\n"))
	thin := programmeChecksByID(t, e, "thin")
	if thin["problems-groups"].Message != "1 problem does not say who it affects." {
		t.Fatalf("unexpected groups message: %q", thin["problems-groups"].Message)
	}
	if thin["problems-gaps"].Message != "1 problem points at no gap as evidence." {
		t.Fatalf("unexpected gaps message: %q", thin["problems-gaps"].Message)
	}
}

// A loop between programmes is the fact a programme's own definition
// cannot show, and no check on a programme may ever refuse a save.
func TestProgrammeChecksCycle(t *testing.T) {
	t.Parallel()
	ms := memory.NewManifestStore()
	e := seededEngineOver(t, ms)
	named := func(id, name, extra string) string {
		return strings.Replace(programmeYAML(id, extra), "  name: "+id+"\n", "  name: "+name+"\n", 1)
	}
	for _, id := range []string{"alpha", "beta"} {
		mustCommit(t, e, "Programme", id, "local", programmeYAML(id, ""))
	}
	mustCommit(t, e, "Programme", "alpha", "local",
		named("alpha", "Assessment", dependsOn("Programme", "beta", "")))
	if got := programmeChecksByID(t, e, "alpha")["risks-dependency-cycle"]; got.State != "ok" {
		t.Fatalf("one edge is not a loop, got %+v", got)
	}
	// Closing it is refused now (TAXONOMY.md D28); a store written before
	// may hold the loop, and the check still reads it.
	writeBehind(t, ms, "Programme", "beta", named("beta", "Curriculum", dependsOn("Programme", "alpha", "")))

	got := programmeChecksByID(t, e, "alpha")["risks-dependency-cycle"]
	if got.State != "warn" {
		t.Fatalf("expected a cycle warning, got %+v", got)
	}
	// Read from the programme the reader is on, and closed.
	if got.Message != "A loop: Assessment → Curriculum → Assessment." {
		t.Fatalf("unexpected cycle message: %q", got.Message)
	}
	// The same loop, read from the other end, starts there instead.
	other := programmeChecksByID(t, e, "beta")["risks-dependency-cycle"]
	if other.Message != "A loop: Curriculum → Assessment → Curriculum." {
		t.Fatalf("unexpected cycle message from beta: %q", other.Message)
	}

	// Nothing a programme reports may stop a save: every one of these
	// reads a manifest other than this one.
	for _, c := range programmeChecksByID(t, e, "alpha") {
		if c.State != "ok" && c.State != "warn" {
			t.Fatalf("a programme check may only advise, got %+v", c)
		}
	}
}

// A note is free text Cartograph does not read, but the shape still has to hold:
// keyed by the step it belongs to, and nothing else.
func TestNotesShape(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	ctx := context.Background()

	mustCommit(t, e, "Programme", "noted", "local", programmeYAML("noted",
		"  notes:\n    risks: Waiting on the legal opinion before naming a mitigation.\n"))
	v, err := e.Get(ctx, "Programme", "noted")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(v.YAML), "Waiting on the legal opinion") {
		t.Fatalf("the note did not survive the round trip: %s", v.YAML)
	}

	// A key that is not a step name is not a note about a step.
	if _, err := e.Commit(ctx, "Programme", "bad", []byte(programmeYAML("bad",
		"  notes:\n    Not A Step: text\n")), "local", "seed"); err == nil {
		t.Fatal("expected a non-slug note key to be refused")
	}
	// And a note is a string, not a nested structure pretending to be a field.
	if _, err := e.Commit(ctx, "Programme", "bad2", []byte(programmeYAML("bad2",
		"  notes:\n    risks:\n      why: text\n")), "local", "seed"); err == nil {
		t.Fatal("expected a structured note to be refused")
	}
}

// A save promotes the draft, and only its own.
//
// This is the test that was missing when the promotion was written into the
// wrong method: Commit runs inside a transaction, so the store's own
// PutVersion is not the path the app takes. A unit test on the store passed
// while every real save left its draft behind.
func TestCommitPromotesOnlyItsOwnDraft(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	e, cleanup := seededEngineWithVault(t, dir)
	defer cleanup()
	ctx := context.Background()

	draft := func(id, aim string) []byte {
		return []byte(programmeYAML(id, "") + "  source: " + aim + "\n")
	}
	for _, id := range []string{"one", "two"} {
		if err := e.PutWorking(ctx, "Programme", id, draft(id, "a note")); err != nil {
			t.Fatal(err)
		}
	}
	staged := func() []string {
		t.Helper()
		var out []string
		for _, id := range []string{"one", "two"} {
			if _, err := os.Stat(filepath.Join(dir, ".cartograph", "staging", "Programme", id+".yaml")); err == nil {
				out = append(out, id)
			}
		}
		return out
	}
	if got := staged(); len(got) != 2 {
		t.Fatalf("expected two drafts, got %v", got)
	}
	// Neither is in the vault yet.
	for _, id := range []string{"one", "two"} {
		if _, err := os.Stat(filepath.Join(dir, "Programme", id+".yaml")); !os.IsNotExist(err) {
			t.Fatalf("a draft reached the vault tree: %s (%v)", id, err)
		}
	}

	if _, err := e.Commit(ctx, "Programme", "one", draft("one", "a note"), "local", "save"); err != nil {
		t.Fatal(err)
	}
	if got := staged(); len(got) != 1 || got[0] != "two" {
		t.Fatalf("a save promoted more than its own draft: %v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "Programme", "one.yaml")); err != nil {
		t.Fatalf("the saved programme is not in the vault: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Programme", "two.yaml")); !os.IsNotExist(err) {
		t.Fatalf("saving one draft promoted another: %v", err)
	}
}
