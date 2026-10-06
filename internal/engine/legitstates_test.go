package engine_test

import (
	"context"
	"strings"
	"testing"
)

// A check does not warn about a state its guidance calls complete: a
// constraint has nothing to mitigate, and manual re-entry with its reason
// noted is answered. A purpose says what it lacks itself. A gap with no
// segments is covered once anything works on it.
func TestChecksTakeWhatTheGuidanceCallsComplete(t *testing.T) {
	e := seededEngine(t)
	ctx := context.Background()
	extra := "  risks:\n    - {id: r1, description: Budget is fixed, type: constraint}\n    - {id: r2, description: Late forms, type: risk, mitigation: Train early}\n" +
		"  data:\n    consumes:\n      - {source: d1, purpose: Read results, handoff: manualReentry}\n  notes:\n    data: Ten figures a year; a feed would cost more than it saves.\n"
	if err := e.PutWorking(ctx, "Project", "legit", []byte(projectYAML("legit", extra))); err != nil {
		t.Fatal(err)
	}
	checks, err := e.ProjectChecks(ctx, "legit", false)
	if err != nil {
		t.Fatal(err)
	}
	by := checksByID(checks.Items)
	if c := by["risks-mitigation"]; c.State != "ok" {
		t.Errorf("a constraint without mitigation: %+v", c)
	}
	if c := by["data-handoff"]; c.State != "ok" || !strings.Contains(c.Message, "note says why") {
		t.Errorf("re-entry with its reason noted: %+v", c)
	}

	got, err := e.ChecksOf(ctx, "Purpose", "default", []byte("apiVersion: cartograph/v1\nkind: Purpose\nmetadata:\n  id: default\n  name: Purpose\nspec:\n  organisation: Example\n"))
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]string{}
	for _, c := range got {
		states[c.ID] = c.State
	}
	if states["purpose-organisation"] != "ok" || states["purpose-vision"] != "warn" || states["purpose-mission"] != "warn" {
		t.Errorf("a purpose without vision or mission: %+v", got)
	}

	mustCommit(t, e, "Gap", "plain-gap", "p1", "apiVersion: cartograph/v1\nkind: Gap\nmetadata:\n  id: plain-gap\n  name: Plain gap\nspec:\n  statement: Something falls short\n")
	gap := func() string {
		cs, _ := e.GapChecks(ctx, "plain-gap")
		for _, c := range cs {
			if c.ID == "gap-covered" {
				return c.State
			}
		}
		return ""
	}
	if s := gap(); s != "warn" {
		t.Errorf("an uncited gap with no segments: %s", s)
	}
	mustCommit(t, e, "Project", "citing", "p1", strings.Replace(projectYAML("citing", ""), "        change: {what: No more gap}\n", "        change: {what: No more gap}\n        gaps: [{gap: plain-gap}]\n", 1))
	if s := gap(); s != "ok" {
		t.Errorf("a cited gap with no segments: %s", s)
	}
}

// A value that fails a choice of forms is told what is wrong with the
// form it was nearest, in words: not every form's failures, and not the
// pattern it missed.
func TestASchemaProblemNamesTheNearestFormInWords(t *testing.T) {
	e := seededEngine(t)
	y := "apiVersion: cartograph/v1\nkind: KPI\nmetadata:\n  id: k1\n  name: K\nspec:\n  definition: Share of depots\n  unit: percent\n  direction: increase\n  sources: [d1]\n  baseline: {value: 74.6, date: \"2017\"}\n"
	problems, err := e.Validate(context.Background(), "KPI", []byte(y))
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 1 || problems[0].Path != "/spec/baseline/date" || !strings.Contains(problems[0].Message, "YYYY-MM") {
		t.Fatalf("a year-only baseline: %+v", problems)
	}
}

// A gap stated in words from a named source is an observation the
// guidance accepts; without a source it still asks for an indicator.
func TestAGapStatedFromItsSourceNeedsNoIndicator(t *testing.T) {
	e := seededEngine(t)
	measured := func(spec string) string {
		cs, err := e.ChecksOf(context.Background(), "Gap", "seen", []byte("apiVersion: cartograph/v1\nkind: Gap\nmetadata:\n  id: seen\n  name: Seen\nspec:\n  statement: Something falls short\n  current: Half the lots are graded\n  desired: Every lot is graded\n"+spec))
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range cs {
			if c.ID == "gap-measured" {
				return c.State
			}
		}
		return ""
	}
	if s := measured("  source: The intake audit\n"); s != "ok" {
		t.Errorf("a sourced observation: %s", s)
	}
	if s := measured(""); s != "warn" {
		t.Errorf("an unsourced gap: %s", s)
	}
}
