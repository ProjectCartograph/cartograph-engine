package engine_test

import (
	"context"
	"testing"

	codecyaml "github.com/ProjectCartograph/cartograph-engine/v2/internal/codec/yaml"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/decide"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/decide/fake"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store/memory"
)

const pilot = "  milestones:\n" +
	"    - {id: start, name: Pilot starts, timing: {form: date, date: \"2026-10\"}}\n" +
	"    - {id: review, name: Pilot review, timing: {form: after, event: {on: {local: milestones, id: start}}, lagMonths: 2}}\n" +
	"  deliverables:\n" +
	"    - {id: report, name: Pilot report, due: {form: after, event: {on: {local: milestones, id: review}}, lagMonths: 1, risks: [r-models]}}\n" +
	"  risks:\n" +
	"    - {id: r-models, description: The food models for the survey cost more than budgeted, type: risk}\n"

// The items something can happen to, as the event log names what can
// happen to each (TAXONOMY.md D52, D59).
func TestAProjectsTriggerables(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	mustCommit(t, e, "Project", "pilot", "local", projectYAML("pilot", pilot))
	items, err := e.Triggerables(context.Background(), "pilot")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]engine.Triggerable{}
	for _, it := range items {
		got[it.Item] = it
	}
	if got["milestones/review"].Kind != "milestone" || got["deliverables/report"].Happens[0] != "accepted" || got["risks/r-models"].Happens[0] != "occurred" {
		t.Fatalf("triggerables: %+v", items)
	}
}

// What a person says happened is matched to the item by the decision
// model; without one every item is offered (docs/adr/0030).
func TestWhatHappenedIsMatchedByTheDecisionModel(t *testing.T) {
	t.Parallel()
	ms := memory.NewManifestStore()
	plain := seededEngineOver(t, ms)
	mustCommit(t, plain, "Project", "pilot", "local", projectYAML("pilot", pilot))
	said := "The food models for the survey came in over budget"
	none, err := plain.WhatHappened(context.Background(), "pilot", said)
	if err != nil {
		t.Fatal(err)
	}
	if none.Available || len(none.Matches) != 4 {
		t.Fatalf("without a model, every item, unranked: %+v", none)
	}
	model := fake.New()
	for i := 0; i < 4; i++ {
		name := "t" + string(rune('0'+i))
		model.On(name, func(_ string, q decide.Question) decide.Answer {
			if q.Options[0].Description == "about: The food models for the survey cost more than budgeted" {
				return fake.Pick(q, "about", 0.9)
			}
			return fake.Pick(q, "other", 0.9)
		})
	}
	e, err := engine.New(ms, memory.NewOperationalStore(), engine.WithCodec(codecyaml.New()), engine.WithDecider(model))
	if err != nil {
		t.Fatal(err)
	}
	ranked, err := e.WhatHappened(context.Background(), "pilot", said)
	if err != nil {
		t.Fatal(err)
	}
	if !ranked.Available || len(ranked.Matches) != 1 || ranked.Matches[0].Item != "risks/r-models" {
		t.Fatalf("the risk that names it: %+v", ranked)
	}
}

// From what happened, everything that waits on it; for a risk, every item
// whose timing names it, and what waits on those.
func TestWhatATriggerReaches(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	mustCommit(t, e, "Project", "pilot", "local", projectYAML("pilot", pilot))
	names := func(item string) []string {
		t.Helper()
		nodes, err := e.Affects(context.Background(), "pilot", item)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, n := range nodes {
			out = append(out, n.Name)
		}
		return out
	}
	if got := names("milestones/start"); len(got) != 2 || got[0] != "Pilot review" || got[1] != "Pilot report" {
		t.Errorf("the start reaches the review and the report: %v", got)
	}
	if got := names("risks/r-models"); len(got) != 1 || got[0] != "Pilot report" {
		t.Errorf("the risk reaches the report whose timing names it: %v", got)
	}
}
