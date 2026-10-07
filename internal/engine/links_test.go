package engine_test

import (
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
)

// What a link may join is asked before it is drawn: a person dragging from
// a node can drop only where the link may be made, and is told why not
// everywhere else (TAXONOMY.md D24, D28, D45).
func TestALinkOffersOnlyWhatItMayJoin(t *testing.T) {
	t.Parallel()
	e := newTestEngine(t)
	ctx := actingAs(ada)
	if _, err := e.ImportDir(ctx, exampleDir(t), "alice-nkemah", "seed"); err != nil {
		t.Fatal(err)
	}
	find := func(cs []engine.LinkCandidate, id string) engine.LinkCandidate {
		t.Helper()
		for _, c := range cs {
			if c.ID == id {
				return c
			}
		}
		t.Fatalf("%s is not a candidate", id)
		return engine.LinkCandidate{}
	}

	// The first problem cites a gap that falls on delivering members and
	// retail partners: those are linked, seasonal workers are not offered.
	groups, err := e.LinkCandidates(ctx, engine.LinkProblemGroup, "quality-check-rollout", "problem-1")
	if err != nil {
		t.Fatal(err)
	}
	if c := find(groups, "delivering-members"); !c.Linked || c.Allowed {
		t.Fatalf("a named group: %+v", c)
	}
	if c := find(groups, "seasonal-workers"); c.Allowed || c.Reason == "" {
		t.Fatalf("a group no cited gap affects: %+v", c)
	}

	// A gap closes into an outcome, never an objective or a goal.
	outcomes, err := e.LinkCandidates(ctx, engine.LinkGapOutcome, "no-single-quality-standard", "")
	if err != nil {
		t.Fatal(err)
	}
	if c := find(outcomes, "depots-stay-open-through-the-season"); c.Allowed || c.Reason == "" {
		t.Fatalf("an objective offered to a gap: %+v", c)
	}
	if c := find(outcomes, "produce-meets-retail-entry-standards"); !c.Allowed && !c.Linked {
		t.Fatalf("an outcome refused: %+v", c)
	}

	// An outcome sits under an objective; a goal above it is refused, and
	// an objective may not move under its own outcome.
	parents, err := e.LinkCandidates(ctx, engine.LinkGoalParent, "depot-downtime-is-planned", "")
	if err != nil {
		t.Fatal(err)
	}
	if c := find(parents, "raise-produce-quality"); c.Allowed {
		t.Fatalf("a goal offered as an outcome's parent: %+v", c)
	}
	if c := find(parents, "produce-meets-one-standard"); !c.Allowed {
		t.Fatalf("an objective refused as an outcome's parent: %+v", c)
	}
	up, err := e.LinkCandidates(ctx, engine.LinkGoalParent, "depots-stay-open-through-the-season", "")
	if err != nil {
		t.Fatal(err)
	}
	if c := find(up, "depot-downtime-is-planned"); c.Allowed {
		t.Fatalf("an objective offered its own outcome as a parent: %+v", c)
	}
}
