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
	t.Parallel()
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
	t.Parallel()
	e := seedReadings(t)
	saved := func() int {
		vs, _ := e.Versions(context.Background(), "Team", "t1")
		return vs[len(vs)-1].Number
	}
	before := saved()

	if _, err := e.ProposeSave(actingAs(ada), "Team", "t1", []byte(strings.Replace(teamText, "%s", "x", 1)), "r", nil); !errors.Is(err, engine.ErrNotAnAgent) {
		t.Fatalf("a person proposing: %v", err)
	}
	var invalid *engine.ValidationError
	if _, err := e.ProposeSave(actingAs(adasBot), "Team", "t1", []byte("apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: t1\n  name: T\nspec:\n  nope: 1\n"), "r", nil); !errors.As(err, &invalid) {
		t.Fatalf("an invalid proposal: %v", err)
	}
	p, err := e.ProposeSave(actingAs(adasBot), "Team", "t1", []byte(strings.Replace(teamText, "%s", "Tidied by an agent", 1)), "tidy the description", nil)
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
	t.Parallel()
	e := seedReadings(t)
	p, err := e.ProposeSave(actingAs(adasBot), "Team", "t1", []byte(strings.Replace(teamText, "%s", "proposed", 1)), "r", nil)
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
	t.Parallel()
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

// An agent proposes only what passes the checks a person sees in the
// editor, or names each it leaves open and why; the proposal becomes the
// draft, so its person opens it in the editor.
func TestProposalsMeetTheChecksOrSayWhy(t *testing.T) {
	t.Parallel()
	e := seedReadings(t)
	bare := []byte("apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: g-new\n  name: Members trust the grading\nspec:\n  level: goal\n  objective: Members accept the grade their produce is given.\n")
	_, err := e.ProposeSave(actingAs(adasBot), "Goal", "g-new", bare, "a new goal", nil)
	var open *engine.OpenChecksError
	if !errors.As(err, &open) || len(open.Open) == 0 {
		t.Fatalf("a bare goal: %v", err)
	}
	waive := map[string]string{}
	for _, c := range open.Open {
		waive[c.ID] = "Ada has not decided yet"
	}
	delete(waive, open.Open[0].ID)
	if _, err := e.ProposeSave(actingAs(adasBot), "Goal", "g-new", bare, "a new goal", waive); !errors.As(err, &open) || len(open.Open) != 1 {
		t.Fatalf("one check neither met nor waived: %v", err)
	}
	waive[open.Open[0].ID] = "Ada has not decided yet"
	p, err := e.ProposeSave(actingAs(adasBot), "Goal", "g-new", bare, "a new goal", waive)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Waivers) != len(waive) || p.Waivers[0].Reason != "Ada has not decided yet" || p.Waivers[0].Message == "" {
		t.Fatalf("the waivers on the proposal: %+v", p.Waivers)
	}
	if draft, found, _ := e.GetWorking(context.Background(), "Goal", "g-new"); !found || !strings.Contains(string(draft), "Members trust the grading") {
		t.Fatalf("the proposal as the draft: %v %s", found, draft)
	}
	if checks, err := e.DraftChecks(context.Background(), "Goal", "g-new"); err != nil || len(checks) == 0 {
		t.Fatalf("checks on a draft never saved: %v %v", checks, err)
	}
}

// A KPI, the gap it measures and nothing saved between them: proposed as
// one set, validated against each other, accepted in one transaction in
// the order the references need, or declined together.
func TestASetStandsOrFallsTogether(t *testing.T) {
	t.Parallel()
	e := seedReadings(t)
	kpi := []byte("apiVersion: cartograph/v1\nkind: KPI\nmetadata:\n  id: k-disputes\n  name: Grading disputes\nspec:\n  name: Grading disputes\n  definition: Share of graded deliveries a member disputes.\n  unit: percent\n  direction: decrease\n  source: d1\n  goals: [g1-f]\n")
	gap := []byte("apiVersion: cartograph/v1\nkind: Gap\nmetadata:\n  id: gap-disputes\n  name: Members dispute grades\nspec:\n  statement: One delivery in eight is disputed.\n  current: One delivery in eight is disputed.\n  desired: Disputes are rare.\n  measure: k-disputes\n  outcomes: [g1-f]\n")
	members := []engine.SetMember{{Kind: "Gap", ID: "gap-disputes", Text: gap}, {Kind: "KPI", ID: "k-disputes", Text: kpi}}

	// Alone, the gap references a KPI that does not exist.
	var invalid *engine.ValidationError
	if _, err := e.ProposeSet(actingAs(adasBot), members[:1], "r", nil); !errors.As(err, &invalid) {
		t.Fatalf("the gap without its KPI: %v", err)
	}
	waiveAll := func() map[string]map[string]string {
		w := map[string]map[string]string{}
		_, err := e.ProposeSet(actingAs(adasBot), members, "r", nil)
		var open *engine.OpenChecksError
		if errors.As(err, &open) {
			for _, c := range open.Open {
				key := c.Kind + "/" + c.ManifestID
				if w[key] == nil {
					w[key] = map[string]string{}
				}
				w[key][c.ID] = "for the test"
			}
		} else if err != nil {
			t.Fatal(err)
		}
		return w
	}
	set, err := e.ProposeSet(actingAs(adasBot), members, "the gap and its measure", waiveAll())
	if err != nil {
		t.Fatal(err)
	}
	if len(set) != 2 || set[0].Kind != "KPI" || set[1].Kind != "Gap" || set[0].Set == "" || set[0].Set != set[1].Set {
		t.Fatalf("the set, KPI first: %+v", set)
	}
	if _, err := e.AcceptProposal(actingAs(lee), set[1].ID, "lee@example.org", "", nil); !errors.Is(err, engine.ErrNotTheirs) {
		t.Fatalf("someone else accepting: %v", err)
	}
	if _, err := e.AcceptProposal(actingAs(ada), set[1].ID, "ada@example.org", "reads right", nil); err != nil {
		t.Fatal(err)
	}
	for _, m := range []struct{ kind, id string }{{"KPI", "k-disputes"}, {"Gap", "gap-disputes"}} {
		vs, err := e.Versions(context.Background(), m.kind, m.id)
		if err != nil || len(vs) != 1 || vs[0].Actor != "ada@example.org" || !strings.Contains(vs[0].Reason, "proposed by Claude") {
			t.Fatalf("%s/%s saved: %+v, %v", m.kind, m.id, vs, err)
		}
	}
	mine, _ := e.Proposals(actingAs(ada), store.ProposalFilter{Set: set[0].Set})
	for _, p := range mine {
		if p.Status != store.ProposalAccepted || p.Version != 1 {
			t.Fatalf("each member accepted with its version: %+v", p)
		}
	}

	// Declining one member declines the set.
	members[0].Text = []byte(strings.Replace(string(gap), "Disputes are rare.", "Disputes are very rare.", 1))
	members[1].Text = []byte(strings.Replace(string(kpi), "Share of", "The share of", 1))
	again, err := e.ProposeSet(actingAs(adasBot), members, "sharper", waiveAll())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.DeclineProposal(actingAs(ada), again[0].ID, "ada@example.org", "not now"); err != nil {
		t.Fatal(err)
	}
	if p, _ := e.Proposals(actingAs(ada), store.ProposalFilter{Set: again[0].Set}); p[0].Status != store.ProposalDeclined || p[1].Status != store.ProposalDeclined {
		t.Fatalf("the set after declining one: %+v", p)
	}
}
