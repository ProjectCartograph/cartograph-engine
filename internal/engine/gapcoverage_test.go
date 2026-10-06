package engine_test

import (
	"context"
	"strings"
	"testing"
)

// The thing the design exists for: a gap observed in four slices, answered
// by work that reaches one of them, is recorded as one quarter of a claim.
func TestGapCoverage(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	ctx := context.Background()

	seg := func(id, name string) {
		t.Helper()
		mustCommit(t, e, "Segment", id, "local",
			"apiVersion: cartograph/v1\nkind: Segment\nmetadata:\n  id: "+id+"\n  name: "+name+
				"\nspec:\n  name: "+name+"\n")
	}
	for _, s := range [][2]string{{"early", "Early childhood"}, {"sen", "Special needs"},
		{"rural", "Rural"}, {"digital", "Digital access"}} {
		seg(s[0], s[1])
	}

	mustCommit(t, e, "Gap", "equity", "local",
		"apiVersion: cartograph/v1\nkind: Gap\nmetadata:\n  id: equity\n  name: Equity\n"+
			"spec:\n  statement: Equity gaps persist across the system\n  source: A report\n"+
			"  segments: [early, sen, rural, digital]\n")

	// One project reaches early childhood; a programme reaches digital.
	mustCommit(t, e, "Project", "early-years", "local",
		"apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: early-years\n  name: Early Years\n"+
			"spec:\n  team: t1\n  summary:\n    problems:\n      - problem: {situation: A gap}\n        change: {what: No more gap}\n"+
			"        gaps:\n          - {gap: equity, segments: [early]}\n")
	mustCommit(t, e, "Programme", "connectivity", "local",
		programmeYAML("connectivity", "  problems:\n    - problem: {situation: A gap}\n      change: {what: No more gap}\n"+
			"      gaps:\n        - {gap: equity, segments: [digital]}\n"))

	coverage, err := e.GapCoverageFor(ctx, "equity")
	if err != nil {
		t.Fatal(err)
	}
	if len(coverage.Segments) != 4 {
		t.Fatalf("expected the gap's four segments, got %+v", coverage.Segments)
	}
	// In the gap's own order, not the alphabet's.
	var order []string
	for _, s := range coverage.Segments {
		order = append(order, s.Segment)
	}
	if strings.Join(order, ",") != "early,sen,rural,digital" {
		t.Fatalf("expected the gap's own order, got %v", order)
	}
	by := map[string][]string{}
	for _, s := range coverage.Segments {
		for _, c := range s.Addressed {
			by[s.Segment] = append(by[s.Segment], c.Kind+"/"+c.ID)
		}
	}
	if len(by["early"]) != 1 || by["early"][0] != "Project/early-years" {
		t.Fatalf("expected the project on early, got %v", by["early"])
	}
	if len(by["digital"]) != 1 || by["digital"][0] != "Programme/connectivity" {
		t.Fatalf("expected the programme on digital, got %v", by["digital"])
	}
	// And the two nobody took, which is the question a register exists for.
	for _, empty := range []string{"sen", "rural"} {
		if len(by[empty]) != 0 {
			t.Fatalf("expected nobody on %s, got %v", empty, by[empty])
		}
	}
	if len(coverage.Whole) != 0 {
		t.Fatalf("no citation claimed the whole gap, got %+v", coverage.Whole)
	}

	checks := map[string]string{}
	got, err := e.GapChecks(ctx, "equity")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range got {
		checks[c.ID] = c.Message
	}
	if !strings.Contains(checks["gap-covered"], "Special needs") ||
		!strings.Contains(checks["gap-covered"], "Rural") {
		t.Fatalf("expected the unaddressed segments named, got %q", checks["gap-covered"])
	}
	// No KPI on this gap, and no current or desired state either.
	if !strings.Contains(checks["gap-measured"], "No indicator tracks this gap yet") {
		t.Fatalf("expected the measure check to say so, got %q", checks["gap-measured"])
	}
	if checks["gap-states"] != "No current state yet." {
		t.Fatalf("expected the states check to ask for the current state, got %q", checks["gap-states"])
	}
}

// A citation that names no segments claims the whole gap, which is what
// every citation written before 2026-09-29 means.
func TestGapCitedWhole(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	ctx := context.Background()

	mustCommit(t, e, "Segment", "north", "local",
		"apiVersion: cartograph/v1\nkind: Segment\nmetadata:\n  id: north\n  name: North\nspec:\n  name: North\n")
	mustCommit(t, e, "Gap", "one-gap", "local",
		"apiVersion: cartograph/v1\nkind: Gap\nmetadata:\n  id: one-gap\n  name: One gap\n"+
			"spec:\n  statement: Something is short\n  segments: [north]\n")
	mustCommit(t, e, "Project", "whole", "local",
		"apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: whole\n  name: Whole\n"+
			"spec:\n  team: t1\n  summary:\n    problems:\n      - problem: {situation: A gap}\n        change: {what: No more gap}\n"+
			"        gaps:\n          - {gap: one-gap}\n")

	coverage, err := e.GapCoverageFor(ctx, "one-gap")
	if err != nil {
		t.Fatal(err)
	}
	if len(coverage.Whole) != 1 || coverage.Whole[0].ID != "whole" {
		t.Fatalf("expected one whole-gap claim, got %+v", coverage.Whole)
	}
	// A whole claim does not fill the segments: it is a claim about all of
	// them, and the check is what says the two disagree.
	if len(coverage.Segments[0].Addressed) != 0 {
		t.Fatalf("a whole claim should not silently fill a segment: %+v", coverage.Segments[0])
	}
	msg := ""
	got, _ := e.GapChecks(ctx, "one-gap")
	for _, c := range got {
		if c.ID == "gap-covered" {
			msg = c.Message
		}
	}
	if !strings.Contains(msg, "claims the whole gap") {
		t.Fatalf("expected the whole-gap claim to be called out, got %q", msg)
	}
}

// Claiming a gap somewhere it was never observed is a different claim, not
// a narrower one, so it is refused.
func TestGapCitationMustBeWithinScope(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	ctx := context.Background()

	for _, s := range [][2]string{{"early", "Early childhood"}, {"rural", "Rural"}} {
		mustCommit(t, e, "Segment", s[0], "local",
			"apiVersion: cartograph/v1\nkind: Segment\nmetadata:\n  id: "+s[0]+"\n  name: "+s[1]+
				"\nspec:\n  name: "+s[1]+"\n")
	}
	mustCommit(t, e, "Gap", "narrow", "local",
		"apiVersion: cartograph/v1\nkind: Gap\nmetadata:\n  id: narrow\n  name: Narrow\n"+
			"spec:\n  statement: Short in one place\n  segments: [early]\n")

	_, err := e.Commit(ctx, "Project", "overreach", []byte(
		"apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: overreach\n  name: Overreach\n"+
			"spec:\n  team: t1\n  summary:\n    problems:\n      - problem: {situation: A gap}\n        change: {what: No more gap}\n"+
			"        gaps:\n          - {gap: narrow, segments: [rural]}\n"), "local", "seed")
	if err == nil {
		t.Fatal("expected a segment outside the gap's scope to be refused")
	}
	if !strings.Contains(err.Error(), "was not observed in") {
		t.Fatalf("expected the reason to be named, got %v", err)
	}

	// A gap with no segments can only be cited whole, and says so.
	mustCommit(t, e, "Gap", "unscoped", "local",
		"apiVersion: cartograph/v1\nkind: Gap\nmetadata:\n  id: unscoped\n  name: Unscoped\n"+
			"spec:\n  statement: Short somewhere\n")
	_, err = e.Commit(ctx, "Project", "precise", []byte(
		"apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: precise\n  name: Precise\n"+
			"spec:\n  team: t1\n  summary:\n    problems:\n      - problem: {situation: A gap}\n        change: {what: No more gap}\n"+
			"        gaps:\n          - {gap: unscoped, segments: [early]}\n"), "local", "seed")
	if err == nil || !strings.Contains(err.Error(), "can only be cited whole") {
		t.Fatalf("expected a gap with no segments to refuse a scoped citation, got %v", err)
	}
}

// A segment tree that closes on itself has no top, and everything that
// walks it would walk it forever.
func TestSegmentTreeRefusesACycle(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	ctx := context.Background()

	mustCommit(t, e, "Segment", "seg-a", "local",
		"apiVersion: cartograph/v1\nkind: Segment\nmetadata:\n  id: seg-a\n  name: A one\nspec:\n  name: A one\n")
	mustCommit(t, e, "Segment", "seg-b", "local",
		"apiVersion: cartograph/v1\nkind: Segment\nmetadata:\n  id: seg-b\n  name: B one\nspec:\n  name: B one\n  parent: seg-a\n")

	if _, err := e.Commit(ctx, "Segment", "seg-a", []byte(
		"apiVersion: cartograph/v1\nkind: Segment\nmetadata:\n  id: seg-a\n  name: A one\nspec:\n  name: A one\n  parent: seg-b\n"),
		"local", "seed"); err == nil {
		t.Fatal("expected a cycle through b to be refused")
	}
	if _, err := e.Commit(ctx, "Segment", "seg-a", []byte(
		"apiVersion: cartograph/v1\nkind: Segment\nmetadata:\n  id: seg-a\n  name: A one\nspec:\n  name: A one\n  parent: seg-a\n"),
		"local", "seed"); err == nil {
		t.Fatal("expected a segment to be refused as its own parent")
	}
}
