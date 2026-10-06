package engine_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/render"
)

// A pathway is the programme's theory of how the change happens, and the
// one thing it cannot be is a loop.
func TestProgrammePathway(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	ctx := context.Background()

	mustCommit(t, e, "Assumption", "staff-released", "local",
		"apiVersion: cartograph/v1\nkind: Assumption\nmetadata:\n  id: staff-released\n  name: Staff released\n"+
			"spec:\n  statement: Depots release staff for the training days\n  confidence: medium\n"+
			"  ifFalse: The training reaches half the depots and the standard is applied unevenly.\n")

	// g1 is the pillar, g1-s strategic, g1-f functional: a plausible
	// three-step theory reading up the tree.
	ok := "  pathway:\n" +
		"    - {outcome: g1-f, because: The components do this directly, assumes: [staff-released]}\n" +
		"    - {outcome: g1-s, from: [g1-f], because: Doing that first makes this reachable}\n" +
		"    - {outcome: g1, from: [g1-s], because: And that is what the pillar asks for}\n"
	mustCommit(t, e, "Programme", "theory", "local", programmeYAML("theory", "  goals: [g1-f]\n"+ok))

	v, err := e.Get(ctx, "Programme", "theory")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"pathway:", "staff-released", "because:"} {
		if !strings.Contains(string(v.YAML), want) {
			t.Fatalf("expected %q to survive the round trip:\n%s", want, v.YAML)
		}
	}

	// A step that waits on an outcome which waits back on it has no first
	// step, and everything that walks it would walk it forever.
	_, err = e.Commit(ctx, "Programme", "loop", []byte(programmeYAML("loop",
		"  pathway:\n"+
			"    - {outcome: g1-f, from: [g1-s]}\n"+
			"    - {outcome: g1-s, from: [g1-f]}\n")), "local", "seed")
	if err == nil {
		t.Fatal("expected a pathway that closes on itself to be refused")
	}
	if !strings.Contains(err.Error(), "no first step") {
		t.Fatalf("expected the reason to be named, got %v", err)
	}

	// One step per outcome, so a reader knows which reasoning applies.
	_, err = e.Commit(ctx, "Programme", "twice", []byte(programmeYAML("twice",
		"  pathway:\n    - {outcome: g1-f}\n    - {outcome: g1-f, from: [g1-s]}\n")), "local", "seed")
	if err == nil || !strings.Contains(err.Error(), "reached twice") {
		t.Fatalf("expected one step per outcome, got %v", err)
	}

	// A goal the vault does not hold is refused by the reference walker,
	// which is what makes an intermediate outcome a real thing.
	if _, err := e.Commit(ctx, "Programme", "ghost", []byte(programmeYAML("ghost",
		"  pathway:\n    - {outcome: no-such-goal}\n")), "local", "seed"); err == nil {
		t.Fatal("expected a pathway step naming no real goal to be refused")
	}
}

// A success criterion may name the outputs behind it, and needs none.
func TestSuccessCriterionLinksAreOptional(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	ctx := context.Background()

	mustCommit(t, e, "Assumption", "uptake", "local",
		"apiVersion: cartograph/v1\nkind: Assumption\nmetadata:\n  id: uptake\n  name: Uptake\n"+
			"spec:\n  statement: Depots use the tool they are given\n")

	// One criterion produced by a deliverable, one standing on its own.
	// The second is the case the seven dimensions exist for: a compliance
	// line at closing that no deliverable produces (DESIGN_RULES.md).
	body := projectYAML("linked",
		"  deliverables:\n    - {id: dl-tool, name: The tool}\n"+
			"  successCriteria:\n"+
			"    - id: sc-1\n      statement: Faults are found at intake\n"+
			"      from: [{local: deliverables, id: dl-tool}]\n      assumes: [uptake]\n"+
			"      metric: compliance\n      when: atClosing\n      confirmedBy: {external: The board}\n"+
			"    - id: sc-2\n      statement: The review is closed with no open actions\n"+
			"      metric: compliance\n      when: atClosing\n      confirmedBy: {external: The board}\n")
	mustCommit(t, e, "Project", "linked", "local", body)

	v, err := e.Get(ctx, "Project", "linked")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(v.YAML), "dl-tool") {
		t.Fatalf("expected the link to survive:\n%s", v.YAML)
	}

	// A deliverable this project does not have is refused: the link is
	// optional, and a wrong one is still wrong.
	if _, err := e.Commit(ctx, "Project", "bad-link", []byte(projectYAML("bad-link",
		"  successCriteria:\n    - id: sc-1\n      statement: Something\n"+
			"      from: [{local: deliverables, id: no-such}]\n"+
			"      metric: compliance\n      when: atClosing\n      confirmedBy: {external: The board}\n")),
		"local", "seed"); err == nil {
		t.Fatal("expected a link to a deliverable this project does not have to be refused")
	}
}

// An assumption is not a risk, and the value that said otherwise is gone.
func TestAssumptionIsNotARiskType(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	ctx := context.Background()
	_, err := e.Commit(ctx, "Project", "old-risk", []byte(projectYAML("old-risk",
		"  risks:\n    - {description: Staff will be released, type: assumption}\n")), "local", "seed")
	if err == nil {
		t.Fatal("expected assumption to be gone from the risk types")
	}
	if !strings.Contains(err.Error(), "value must be one of") {
		t.Fatalf("expected a schema refusal, got %v", err)
	}
}

// A label is what a register of a hundred measures is cut by later, so it
// has to survive the list: reading it off the manifest behind every row
// is what the summary exists to avoid.
func TestLabelsReachTheList(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	ctx := context.Background()

	mustCommit(t, e, "Assumption", "labelled", "local",
		"apiVersion: cartograph/v1\nkind: Assumption\nmetadata:\n  id: labelled\n  name: Labelled\n"+
			"  labels: {pillar: quality, pack: board}\nspec:\n  statement: The label survives the list\n")
	mustCommit(t, e, "Assumption", "bare", "local",
		"apiVersion: cartograph/v1\nkind: Assumption\nmetadata:\n  id: bare\n  name: Bare\n"+
			"spec:\n  statement: This one carries no labels at all\n")

	list, err := e.List(ctx, "Assumption", engine.Filter{}, false)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]map[string]string{}
	for _, s := range list {
		found[s.ID] = s.Labels
	}
	if got := found["labelled"]["pillar"]; got != "quality" {
		t.Fatalf("pillar label on the summary = %q, want quality (%v)", got, found["labelled"])
	}
	if found["bare"] != nil {
		t.Fatalf("a manifest with no labels should carry none, got %v", found["bare"])
	}
}

// A problem and a change are stored in the parts they are written in, and
// a file still holding the composed sentence is read into those parts
// rather than refused: a vault is a directory people also edit by hand.
func TestStoredSentencesBecomeParts(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	ctx := context.Background()

	// Written the old way, straight into the store, the way a file on
	// disk from last release arrives.
	old := "apiVersion: cartograph/v1\nkind: Programme\nmetadata:\n  id: legacy-prose\n  name: Legacy\n" +
		"spec:\n  aim: Raise quality, so that buyers can rely on us\n  leadTeam: t1\n" +
		"  problems:\n    - problem: Depot staff wait a season, because checks happen late\n" +
		"      change: Checks run at intake, so depot staff learn the same day\n"
	if err := e.PutWorking(ctx, "Programme", "legacy-prose", []byte(old)); err != nil {
		t.Fatal(err)
	}

	v, err := e.Get(ctx, "Programme", "legacy-prose")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"change: Raise quality",
		"gain: buyers can rely on us",
		"situation: Depot staff wait a season",
		"cause: checks happen late",
		"what: Checks run at intake",
		"gain: depot staff learn the same day",
	} {
		if !strings.Contains(string(v.YAML), want) {
			t.Fatalf("expected %q in the read-back manifest:\n%s", want, v.YAML)
		}
	}
}

// Seeding defaults is not a licence to rewrite what is there. A vault
// opened without its generated index lists nothing, and the units on disk
// are still the vault's own.
func TestSeedingNeverOverwritesAUnitThatExists(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	ctx := context.Background()

	hand := "apiVersion: cartograph/v1\nkind: Unit\nmetadata:\n  id: percent\n  name: \"Percent\"\n" +
		"spec:\n  symbol: \"%\"\n  dimension: percent\n"
	mustCommit(t, e, "Unit", "percent", "local", hand)

	if _, err := e.SeedStandardUnits(ctx); err != nil {
		t.Fatal(err)
	}

	v, err := e.Get(ctx, "Unit", "percent")
	if err != nil {
		t.Fatal(err)
	}
	if string(v.YAML) != hand {
		t.Fatalf("seeding rewrote a unit that was already there:\n%s", v.YAML)
	}
}

// The charter is the document a definition adds up to, and a programme's
// is the same document a project's is with its own sections in it.
func TestProgrammeCharterReadsTheDefinition(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	ctx := context.Background()

	mustCommit(t, e, "Assumption", "staff-released", "local",
		"apiVersion: cartograph/v1\nkind: Assumption\nmetadata:\n  id: staff-released\n  name: Staff released\n"+
			"spec:\n  statement: Depots release staff for the training days\n")
	mustCommit(t, e, "Programme", "charter-prog", "local", programmeYAML("charter-prog",
		"  pathway:\n"+
			"    - {outcome: g1-f, because: The components do this directly, assumes: [staff-released]}\n"+
			"    - {outcome: g1, from: [g1-f], because: And that is what the pillar asks for}\n"))

	html, err := render.ProgrammeCharter(ctx, e, "charter-prog")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(html)
	for _, want := range []string{
		"<h2>Theory of change</h2>",
		"And that is what the pillar asks for",
		"Preconditions",
		"Staff released",
		"</html>",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("expected %q in the charter:\n%s", want, doc)
		}
	}

	// The pathway reads the way it is authored: the outcome first.
	outcome := strings.Index(doc, "And that is what the pillar asks for")
	first := strings.Index(doc, "The components do this directly")
	if outcome > first {
		t.Fatalf("the pathway should read from the outcome down, got the earliest step first:\n%s", doc)
	}
}
