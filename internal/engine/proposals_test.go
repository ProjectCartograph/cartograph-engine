package engine_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/identity"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

var (
	ada      = identity.Principal{Subject: "ada@example.org", Email: "ada@example.org", Name: "Ada"}
	adasBot  = identity.Principal{Subject: "ada@example.org", Email: "ada@example.org", Name: "Ada", Agent: "Claude"}
	lee      = identity.Principal{Subject: "lee@example.org", Email: "lee@example.org", Name: "Lee"}
	teamText = "apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: t1\n  name: Team One\nspec:\n  name: Team One\n  description: %s\n"
)

func actingAs(p identity.Principal) context.Context {
	return identity.WithPrincipal(context.Background(), p)
}

// An agent may edit a draft, and nothing that makes the record.
func TestAnAgentDoesNotMakeTheRecord(t *testing.T) {
	e := seedReadings(t)
	ctx := actingAs(adasBot)
	if err := e.PutWorking(ctx, "Team", "t1", []byte(strings.Replace(teamText, "%s", "a draft", 1))); err != nil {
		t.Fatalf("an agent's draft: %v", err)
	}
	refused := map[string]error{}
	_, refused["commit"] = e.Commit(ctx, "Team", "t1", []byte(strings.Replace(teamText, "%s", "saved", 1)), "x", "r")
	_, refused["reading"] = e.AppendSeriesItem(ctx, "KPIReadings", "k1-readings", "readings", map[string]any{"period": "2026-09", "value": 1}, "x", "r")
	_, refused["state"] = e.TransitionProjectState(ctx, "p1", "defined", "x", "r")
	refused["delete"] = e.Delete(ctx, "Team", "t1", "x", "r")
	refused["discard"] = e.DiscardWorking(ctx, "Team", "t1")
	_, refused["units"] = e.SeedStandardUnits(ctx)
	for what, err := range refused {
		if !errors.Is(err, identity.ErrAgentProposes) {
			t.Errorf("an agent's %s: %v, want ErrAgentProposes", what, err)
		}
	}
}

func TestProposalsAreDecidedByTheirPerson(t *testing.T) {
	e := seedReadings(t)
	saved := func() int {
		vs, _ := e.Versions(context.Background(), "Team", "t1")
		return vs[len(vs)-1].Number
	}
	before := saved()

	if _, err := e.ProposeSave(actingAs(ada), "Team", "t1", []byte(strings.Replace(teamText, "%s", "x", 1)), "r"); !errors.Is(err, engine.ErrNotAnAgent) {
		t.Fatalf("a person proposing: %v", err)
	}
	var invalid *engine.ValidationError
	if _, err := e.ProposeSave(actingAs(adasBot), "Team", "t1", []byte("apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: t1\n  name: T\nspec:\n  nope: 1\n"), "r"); !errors.As(err, &invalid) {
		t.Fatalf("an invalid proposal: %v", err)
	}
	p, err := e.ProposeSave(actingAs(adasBot), "Team", "t1", []byte(strings.Replace(teamText, "%s", "Tidied by an agent", 1)), "tidy the description")
	if err != nil {
		t.Fatal(err)
	}
	if p.Agent != "Claude" || p.For != "ada@example.org" || p.Base != before || saved() != before {
		t.Fatalf("proposed %+v; saved %d, was %d", p, saved(), before)
	}
	mine, _ := e.Proposals(actingAs(ada), store.ProposalFilter{Status: store.ProposalOpen})
	theirs, _ := e.Proposals(actingAs(lee), store.ProposalFilter{Status: store.ProposalOpen})
	onIt, _ := e.Proposals(actingAs(lee), store.ProposalFilter{Kind: "Team", ManifestID: "t1"})
	if len(mine) != 1 || len(theirs) != 0 || len(onIt) != 1 {
		t.Fatalf("Ada sees %d, Lee %d of hers, and %d on the team", len(mine), len(theirs), len(onIt))
	}

	if _, err := e.AcceptProposal(actingAs(adasBot), p.ID, "x", "", nil); !errors.Is(err, identity.ErrAgentProposes) {
		t.Fatalf("an agent accepting: %v", err)
	}
	if _, err := e.AcceptProposal(actingAs(lee), p.ID, "lee@example.org", "", nil); !errors.Is(err, engine.ErrNotTheirs) {
		t.Fatalf("someone else accepting: %v", err)
	}
	got, err := e.AcceptProposal(actingAs(ada), p.ID, "ada@example.org", "looks right", nil)
	if err != nil {
		t.Fatal(err)
	}
	v, _ := e.GetVersion(context.Background(), "Team", "t1", got.Version)
	if got.Status != store.ProposalAccepted || got.Version != before+1 || v.Actor != "ada@example.org" ||
		!strings.Contains(v.Reason, "proposed by Claude") || !strings.Contains(string(v.YAML), "Tidied by an agent") {
		t.Fatalf("accepted %+v as %+v", got, v)
	}
	if _, err := e.DeclineProposal(actingAs(ada), p.ID, "ada@example.org", ""); !errors.Is(err, store.ErrProposalDecided) {
		t.Fatalf("deciding twice: %v", err)
	}
}

// A proposal against a version that is no longer the latest is not
// accepted over what changed.
func TestAStaleProposalIsRefused(t *testing.T) {
	e := seedReadings(t)
	p, err := e.ProposeSave(actingAs(adasBot), "Team", "t1", []byte(strings.Replace(teamText, "%s", "proposed", 1)), "r")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Commit(actingAs(ada), "Team", "t1", []byte(strings.Replace(teamText, "%s", "changed meanwhile", 1)), "ada@example.org", "edit"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.AcceptProposal(actingAs(ada), p.ID, "ada@example.org", "", nil); !errors.Is(err, engine.ErrProposalStale) {
		t.Fatalf("a stale proposal: %v", err)
	}
	if got, _ := e.DeclineProposal(actingAs(ada), p.ID, "ada@example.org", "superseded"); got.Status != store.ProposalDeclined {
		t.Fatalf("declined as %+v", got)
	}
}

// A reading and a state change are proposed and accepted the same way.
func TestReadingsAndStatesAreProposed(t *testing.T) {
	e := seedReadings(t)
	seedGoalTreeFixture(t, e)
	r, err := e.ProposeAppend(actingAs(adasBot), "KPIReadings", "k1-readings", "readings", map[string]any{"period": "2026-09", "value": 66}, "Q3")
	if err != nil {
		t.Fatal(err)
	}
	var invalid *engine.ValidationError
	if _, err := e.ProposeAppend(actingAs(adasBot), "KPIReadings", "k1-readings", "readings", map[string]any{"period": "2026-13", "value": 1}, "r"); !errors.As(err, &invalid) {
		t.Fatalf("an invalid reading: %v", err)
	}
	if _, err := e.AcceptProposal(actingAs(ada), r.ID, "ada@example.org", "", nil); err != nil {
		t.Fatal(err)
	}
	cur, _ := e.Get(context.Background(), "KPIReadings", "k1-readings")
	if !strings.Contains(string(cur.YAML), "2026-09") {
		t.Fatalf("the reading was not recorded:\n%s", cur.YAML)
	}

	s, err := e.ProposeState(actingAs(adasBot), "proj-1", "cancelled", "")
	if !errors.As(err, &invalid) {
		t.Fatalf("a cancellation without a reason: %v, %+v", err, s)
	}
	s, err = e.ProposeState(actingAs(adasBot), "proj-1", "cancelled", "no longer needed")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.AcceptProposal(actingAs(ada), s.ID, "ada@example.org", "", nil); err != nil {
		t.Fatal(err)
	}
	st, _ := e.GetProjectState(context.Background(), "proj-1")
	if st.State != "cancelled" {
		t.Fatalf("the project is %s", st.State)
	}
}
