package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	codecyaml "github.com/ProjectCartograph/cartograph-engine/v2/internal/codec/yaml"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/crdt/automerge"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	fanoutmemory "github.com/ProjectCartograph/cartograph-engine/v2/internal/fanout/memory"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/identity"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/mcp"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store/memory"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/trace"
)

var ada = identity.Principal{Subject: "ada@example.org", Email: "ada@example.org", Name: "Ada"}

const team = "apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: t1\n  name: Team One\nspec:\n  name: Team One\n"

type presence struct {
	mu    sync.Mutex
	seen  []string
	steps []announced
}

type announced struct {
	doc, focus string
	agent      map[string]any
}

func (p *presence) AnnounceAgent(_ context.Context, docID, actor, name, focus string, agent map[string]any) {
	p.mu.Lock()
	p.seen = append(p.seen, actor+" | "+name)
	p.steps = append(p.steps, announced{doc: docID, focus: focus, agent: agent})
	p.mu.Unlock()
}

// refuseAgents is a policy under which Ada may not use an agent.
type refuseAgents struct{}

func (refuseAgents) Authorize(_ context.Context, p identity.Principal, a identity.Action) error {
	if a.Resource == identity.ResourceAgent || p.Agent != "" {
		return identity.ErrForbidden
	}
	return nil
}

func setup(t *testing.T, authz identity.Authorizer) (*engine.Engine, *presence, *sdk.ClientSession) {
	t.Helper()
	return setupTraced(t, authz, nil)
}

// setupTraced is setup with a trace recorder.
func setupTraced(t *testing.T, authz identity.Authorizer, rec trace.Recorder) (*engine.Engine, *presence, *sdk.ClientSession) {
	t.Helper()
	ms := memory.NewManifestStore()
	am, err := automerge.New(1)
	if err != nil {
		t.Fatal(err)
	}
	e, err := engine.New(ms, memory.NewOperationalStore().LogTo(ms), engine.WithCodec(codecyaml.New()),
		engine.WithCRDT(am), engine.WithDocStore(memory.NewDocStore()), engine.WithFanout(fanoutmemory.New()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Commit(context.Background(), "Team", "t1", []byte(team), "seed", "seed"); err != nil {
		t.Fatal(err)
	}
	pr := &presence{}
	h := mcp.Handler(mcp.Options{Engine: e, Authz: authz, Presence: pr, Version: "test", Trace: rec})
	// As the authentication middleware does: Ada is who the agent acts for.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r.WithContext(identity.WithPrincipal(r.Context(), ada)))
	}))
	t.Cleanup(srv.Close)
	client := sdk.NewClient(&sdk.Implementation{Name: "Claude", Version: "1"}, nil)
	cs, err := client.Connect(context.Background(), &sdk.StreamableClientTransport{Endpoint: srv.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return e, pr, cs
}

func callTool(t *testing.T, cs *sdk.ClientSession, name string, args map[string]any) (*sdk.CallToolResult, string) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return res, b.String()
}

// An agent reads, drafts in a change set of its own, and proposes it;
// nothing it does makes the record or touches the shared drafts.
func TestAnAgentReadsDraftsAndProposes(t *testing.T) {
	t.Parallel()
	e, pr, cs := setup(t, nil)
	ctx := context.Background()

	tools, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tl := range tools.Tools {
		readOnly := tl.Annotations != nil && tl.Annotations.ReadOnlyHint
		if !readOnly && tl.Name != "save_draft" && tl.Name != "save_drafts" && tl.Name != "settle" && tl.Name != "bring_document" && tl.Name != "port" && tl.Name != "settle_register" && tl.Name != "edit_draft" && tl.Name != "discard_draft" && tl.Name != "leave_open" && tl.Name != "start_work" && tl.Name != "propose" && !strings.HasPrefix(tl.Name, "propose_") {
			t.Errorf("tool %s may change something and is neither a draft nor a proposal", tl.Name)
		}
	}

	if res, text := callTool(t, cs, "get", map[string]any{"kind": "Team", "id": "t1"}); res.IsError || !strings.Contains(text, "Team One") {
		t.Fatalf("get: %s", text)
	}
	draft := map[string]any{"apiVersion": "cartograph/v1", "kind": "Team", "metadata": map[string]any{"id": "t1", "name": "Team One"},
		"spec": map[string]any{"description": "Drafted by an agent"}}
	if res, text := callTool(t, cs, "save_draft", map[string]any{"kind": "Team", "id": "t1", "manifest": draft}); res.IsError {
		t.Fatalf("save_draft: %s", text)
	}
	if _, found, _ := e.GetWorking(ctx, "Team", "t1"); found {
		t.Fatal("the agent's draft went into the shared draft")
	}
	agentCtx := identity.WithPrincipal(ctx, identity.Principal{Subject: "ada@example.org", Email: "ada@example.org", Name: "Ada", Agent: "Claude"})
	set, found, err := e.CurrentChangeSet(agentCtx, "")
	if err != nil || !found {
		t.Fatalf("no change set for the agent: %v", err)
	}
	if w, inSet, _ := e.ChangeSetText(ctx, set.ID, "Team", "t1"); !inSet || !strings.Contains(string(w), "Drafted by an agent") {
		t.Fatalf("the change set's draft is %q", w)
	}
	if len(pr.seen) == 0 || pr.seen[0] != "ada@example.org via Claude | Ada's agent (Claude)" {
		t.Fatalf("announced as %v", pr.seen)
	}
	// The draft is announced on Ada's own feed, with the change set, for
	// her to follow; not on the team's shared draft, which it did not
	// touch, and never on the presence document everyone joins.
	docs := map[string]bool{}
	for _, a := range pr.steps {
		if a.agent["step"] != "draft" {
			continue
		}
		docs[a.doc] = true
		fields, _ := a.agent["fields"].([]string)
		if a.agent["for"] != "ada@example.org" || a.agent["kind"] != "Team" || a.agent["name"] != "Team One" || len(fields) != 1 || fields[0] != "/spec/description" {
			t.Errorf("the draft step: %+v", a.agent)
		}
		if _, ok := a.agent["met"]; !ok {
			t.Errorf("the draft step carries no check count: %+v", a.agent)
		}
		if a.agent["changeSet"] != set.ID {
			t.Errorf("the draft step does not name its change set: %+v", a.agent)
		}
	}
	feed, _ := e.Shared().AgentFeed(ctx, "ada@example.org")
	if !docs[feed] || len(docs) != 1 {
		t.Fatalf("the draft was announced on %v, want Ada's feed alone", docs)
	}

	res, text := callTool(t, cs, "propose_save", map[string]any{"kind": "Team", "id": "t1", "reason": "describe the team"})
	if res.IsError {
		t.Fatalf("propose_save: %s", text)
	}
	if vs, _ := e.Versions(ctx, "Team", "t1"); len(vs) != 1 {
		t.Fatalf("a proposal saved a version: %d versions", len(vs))
	}
	view, _ := e.ViewChangeSet(ctx, set.ID)
	if view.ChangeSet.Status != store.ChangeSetProposed || view.ChangeSet.Agent != "Claude" || view.ChangeSet.For != "ada@example.org" || len(view.Items) != 1 {
		t.Fatalf("Ada's change set: %+v", view)
	}
	if _, err := e.ReopenChangeSet(identity.WithPrincipal(ctx, ada), set.ID, "one more thing"); err != nil {
		t.Fatal(err)
	}

	// Problems come back by field, for the agent to fix.
	bad := map[string]any{"apiVersion": "cartograph/v1", "kind": "Team", "metadata": map[string]any{"id": "t1", "name": "T"}, "spec": map[string]any{"nope": 1}}
	if res, text := callTool(t, cs, "propose_save", map[string]any{"kind": "Team", "id": "t1", "manifest": bad, "reason": "r"}); !res.IsError || !strings.Contains(text, "Not valid") {
		t.Fatalf("an invalid proposal: %v %s", res.IsError, text)
	}
}

// An agent its person may not use is refused before it reads anything.
func TestAnAgentNotAllowedIsRefused(t *testing.T) {
	t.Parallel()
	_, _, cs := setup(t, refuseAgents{})
	if res, text := callTool(t, cs, "get", map[string]any{"kind": "Team", "id": "t1"}); !res.IsError || !strings.Contains(text, "forbidden") {
		t.Fatalf("an agent not allowed read: %v %s", res.IsError, text)
	}
}

// Every kind with a flow has a prompt to guide a person through it.
func TestGuidePrompts(t *testing.T) {
	t.Parallel()
	e, _, cs := setup(t, nil)
	prompts, err := cs.ListPrompts(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(prompts.Prompts) != len(e.FlowKinds()) || len(prompts.Prompts) == 0 {
		t.Fatalf("%d prompts for %d flows", len(prompts.Prompts), len(e.FlowKinds()))
	}
}

func TestAClientIsNamedByItsUserAgent(t *testing.T) {
	t.Parallel()
	for ua, want := range map[string]string{
		"claude-code/2.1.283 (external, cli)": "Claude Code",
		"Cursor/1.4":                          "Cursor",
		"Mozilla/5.0 (X11; Linux x86_64)":     "",
		"Go-http-client/1.1":                  "",
		"":                                    "",
	} {
		if got := mcp.ProductName(ua); got != want {
			t.Errorf("%q: %q, want %q", ua, got, want)
		}
	}
}

// A sub-agent names itself in _meta and is recorded beside its parent.
func TestASubagentIsNamedBesideItsParent(t *testing.T) {
	t.Parallel()
	_, pr, cs := setup(t, nil)
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "get", Arguments: map[string]any{"kind": "Team", "id": "t1"},
		Meta: sdk.Meta{mcp.SubagentMeta: "researcher"}})
	if err != nil || res.IsError {
		t.Fatalf("get: %v %+v", err, res)
	}
	pr.mu.Lock()
	defer pr.mu.Unlock()
	if len(pr.seen) == 0 || pr.seen[len(pr.seen)-1] != "ada@example.org via Claude › researcher | Ada's agent (Claude › researcher)" {
		t.Fatalf("announced as %v", pr.seen)
	}
}

// An agent edits the fields it names and no others, so what its person
// changed in the meantime stays, and comes back to the agent to build on.
func TestAnAgentsEditKeepsWhatItsPersonChanged(t *testing.T) {
	t.Parallel()
	e, _, cs := setup(t, nil)
	ctx := context.Background()
	goal := map[string]any{"apiVersion": "cartograph/v1", "kind": "Goal", "metadata": map[string]any{"id": "g9", "name": "Cut loss after picking"},
		"spec": map[string]any{"level": "goal", "objective": "Less fruit is lost"}}
	res, out := callTool(t, cs, "save_draft", map[string]any{"kind": "Goal", "id": "g9", "manifest": goal})
	if res.IsError {
		t.Fatalf("save_draft: %s", out)
	}
	agentCtx := identity.WithPrincipal(ctx, identity.Principal{Subject: "ada@example.org", Email: "ada@example.org", Name: "Ada", Agent: "Claude"})
	set, _, _ := e.CurrentChangeSet(agentCtx, "")
	// Meanwhile, its person writes why it matters, in the change set.
	if _, err := e.EditInChangeSet(identity.WithPrincipal(ctx, ada), set.ID, "Goal", "g9", map[string]any{"/spec/whyItMatters": "Members are paid by what arrives sound"}, nil); err != nil {
		t.Fatal(err)
	}
	res, out = callTool(t, cs, "edit_draft", map[string]any{"kind": "Goal", "id": "g9", "set": map[string]any{"/spec/objective": "Fruit arrives sound at every depot"}})
	if res.IsError {
		t.Fatalf("edit_draft: %s", out)
	}
	if !strings.Contains(out, "Members are paid by what arrives sound") || !strings.Contains(out, "Fruit arrives sound at every depot") {
		t.Fatalf("the draft returned lost a change: %s", out)
	}
	if res, out := callTool(t, cs, "edit_draft", map[string]any{"kind": "Goal", "id": "g9", "set": map[string]any{"/metadata/id": "other"}}); !res.IsError {
		t.Fatalf("an edit changed the id: %s", out)
	}
}

// Saving one draft tells the agent what the work around it still lacks:
// the gap its outcome closes has no indicator and no segments yet.
func TestADraftHearsWhatTheWorkAroundItLacks(t *testing.T) {
	t.Parallel()
	_, _, cs := setup(t, nil)
	gap := map[string]any{"apiVersion": "cartograph/v1", "kind": "Gap", "metadata": map[string]any{"id": "gap-bruising", "name": "Bruised on arrival"},
		"spec": map[string]any{"current": "One crate in five arrives bruised", "desired": "Fewer than one in fifty", "outcomes": []any{"o-sound"}}}
	outcome := map[string]any{"apiVersion": "cartograph/v1", "kind": "Goal", "metadata": map[string]any{"id": "o-sound", "name": "Fruit arrives sound"},
		"spec": map[string]any{"level": "outcome", "objective": "Fruit arrives sound at every depot"}}
	// In order (TAXONOMY.md D31): the outcome first, then the gap that
	// names it.
	if res, text := callTool(t, cs, "save_draft", map[string]any{"kind": "Goal", "id": "o-sound", "manifest": outcome}); res.IsError {
		t.Fatalf("save_draft outcome: %s", text)
	}
	if res, text := callTool(t, cs, "save_draft", map[string]any{"kind": "Gap", "id": "gap-bruising", "manifest": gap}); res.IsError {
		t.Fatalf("save_draft gap: %s", text)
	}
	res, text := callTool(t, cs, "edit_draft", map[string]any{"kind": "Goal", "id": "o-sound", "set": map[string]any{"/spec/objective": "Fruit arrives sound at every depot"}, "work": []string{"Gap/gap-bruising"}})
	if res.IsError {
		t.Fatalf("edit_draft outcome: %s", text)
	}
	for _, want := range []string{`"around"`, "gap-bruising", "gap-measured", "gap-segments", "aroundNext"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the outcome's report lacks %s: %s", want, text)
		}
	}
	if res, text := callTool(t, cs, "guide", map[string]any{"kind": "Goal", "level": "objective"}); res.IsError || !strings.Contains(text, `"plan"`) || !strings.Contains(text, "gap-measured") {
		t.Fatalf("the objective's guide has no plan through the gap's indicator: %.400s", text)
	}
}

// The agent is told what comes next, in Cartograph's order, without
// anyone telling it the order: the record is written from the top down
// (TAXONOMY.md D28), so the outcome is finished before the gap that
// names it.
func TestTheAgentIsToldWhatComesNext(t *testing.T) {
	t.Parallel()
	_, _, cs := setup(t, nil)
	gap := map[string]any{"apiVersion": "cartograph/v1", "kind": "Gap", "metadata": map[string]any{"id": "gap-bruising", "name": "Bruised on arrival"},
		"spec": map[string]any{"outcomes": []any{"o-sound"}}}
	outcome := map[string]any{"apiVersion": "cartograph/v1", "kind": "Goal", "metadata": map[string]any{"id": "o-sound", "name": "Fruit arrives sound"},
		"spec": map[string]any{"level": "outcome"}}
	callTool(t, cs, "save_draft", map[string]any{"kind": "Goal", "id": "o-sound", "manifest": outcome})
	res, text := callTool(t, cs, "save_draft", map[string]any{"kind": "Gap", "id": "gap-bruising", "manifest": gap, "work": []string{"Goal/o-sound"}})
	if res.IsError || !strings.Contains(text, `Next (define): Goal \"Fruit arrives sound\"`) {
		t.Fatalf("save_draft does not lead with the outcome the gap names: %s", text)
	}
	res, text = callTool(t, cs, "next", map[string]any{"work": []string{"Goal/o-sound", "Gap/gap-bruising"}})
	if res.IsError || !strings.Contains(text, `"fill"`) || !strings.Contains(text, `"define"`) {
		t.Fatalf("next: %s", text)
	}
}

// With no work named, next says where the workspace stands in the order
// of work: one that has only a team starts at its purpose.
func TestANewWorkspaceStartsAtItsPurpose(t *testing.T) {
	t.Parallel()
	_, _, cs := setup(t, nil)
	res, text := callTool(t, cs, "next", map[string]any{})
	if res.IsError || !strings.Contains(text, "Next: the purpose stage") || !strings.Contains(text, `"stages"`) || !strings.Contains(text, `"waiting"`) {
		t.Fatalf("next on a new workspace: %s", text)
	}
	res, text = callTool(t, cs, "taxonomy", map[string]any{})
	if res.IsError || strings.Index(text, `"Purpose"`) > strings.Index(text, `"Gap"`) || strings.Index(text, `"Gap"`) > strings.Index(text, `"Project"`) {
		t.Fatalf("taxonomy is not in the order of work: %.600s", text)
	}
}

// An agent reading a document maps it by definition: the taxonomy names
// every kind with what plans call it instead.
func TestTheTaxonomyMapsADocumentsWords(t *testing.T) {
	t.Parallel()
	_, _, cs := setup(t, nil)
	res, text := callTool(t, cs, "taxonomy", map[string]any{})
	if res.IsError || !strings.Contains(text, `"Gap"`) || !strings.Contains(text, "problem statement") || !strings.Contains(text, "never by the word") {
		t.Fatalf("taxonomy: %.600s", text)
	}
}

// An agent works in the order of work and is held to it (TAXONOMY.md D31):
// a draft that names what is neither in the record nor in its change set
// is refused, saying what to define first, and a placeholder is never an
// agent's to leave.
func TestAnAgentIsHeldToTheOrderOfWork(t *testing.T) {
	t.Parallel()
	_, _, cs := setup(t, nil)
	gap := map[string]any{"apiVersion": "cartograph/v1", "kind": "Gap", "metadata": map[string]any{"id": "gap-early", "name": "Bruised on arrival"},
		"spec": map[string]any{"outcomes": []any{"o-missing"}}}
	res, text := callTool(t, cs, "save_draft", map[string]any{"kind": "Gap", "id": "gap-early", "manifest": gap})
	if !res.IsError || !strings.Contains(text, "define it first") || !strings.Contains(text, "o-missing") {
		t.Fatalf("a gap naming an outcome nobody has defined: %s", text)
	}
	pending := map[string]any{"apiVersion": "cartograph/v1", "kind": "Gap", "metadata": map[string]any{"id": "gap-later", "name": "Bruised on arrival",
		"pending": []any{map[string]any{"path": "/spec/outcomes/0", "kind": "Goal", "name": "Fruit arrives sound"}}}, "spec": map[string]any{}}
	res, text = callTool(t, cs, "save_draft", map[string]any{"kind": "Gap", "id": "gap-later", "manifest": pending})
	if !res.IsError || !strings.Contains(text, "never leaves a placeholder") {
		t.Fatalf("an agent's placeholder: %s", text)
	}
	if res, text := callTool(t, cs, "edit_draft", map[string]any{"kind": "Gap", "id": "gap-x", "set": map[string]any{"/spec/outcomes": []any{"o-missing"}}}); !res.IsError {
		t.Fatalf("edit_draft named what does not exist: %s", text)
	}
}

// A draft with a missing required field is saved
// (drafts never refuse), but save_draft, edit_draft and checks all report
// what a save would refuse as an open check, so the agent hears it now
// rather than at propose.
func TestADraftsSchemaProblemsAreOpenChecks(t *testing.T) {
	t.Parallel()
	_, _, cs := setup(t, nil)
	draft := map[string]any{"apiVersion": "cartograph/v1", "kind": "Team", "metadata": map[string]any{"id": "t2"},
		"spec": map[string]any{"description": "Drafted by an agent"}}
	res, text := callTool(t, cs, "save_draft", map[string]any{"kind": "Team", "id": "t2", "manifest": draft})
	if res.IsError || !strings.Contains(text, `"id":"schema"`) || !strings.Contains(text, "Fix what is not valid first") || !strings.Contains(text, "not valid yet") {
		t.Fatalf("save_draft with no name: %s", text)
	}
	if res, text := callTool(t, cs, "checks", map[string]any{"kind": "Team", "id": "t2"}); res.IsError || !strings.Contains(text, `"id":"schema"`) || !strings.Contains(text, "/metadata") {
		t.Fatalf("checks with no name: %s", text)
	}
	if res, text := callTool(t, cs, "edit_draft", map[string]any{"kind": "Team", "id": "t2", "set": map[string]any{"/metadata/name": "Team Two"}}); res.IsError || strings.Contains(text, `"id":"schema"`) {
		t.Fatalf("edit_draft that fixes the name: %s", text)
	}
}

// checks and propose agree: checking one draft also lists what is open on
// the change set's other drafts, and propose refuses on exactly those.
func TestChecksAndProposeAgree(t *testing.T) {
	t.Parallel()
	_, _, cs := setup(t, nil)
	// A goal saved valid but unfinished (no horizon, not yet SMART), and a
	// team drafted beside it whose own checks are all met.
	goal := map[string]any{"apiVersion": "cartograph/v1", "kind": "Goal", "metadata": map[string]any{"id": "g-sound", "name": "Sound fruit"},
		"spec": map[string]any{"level": "goal", "objective": "Sound fruit"}}
	team := map[string]any{"apiVersion": "cartograph/v1", "kind": "Team", "metadata": map[string]any{"id": "t1", "name": "Team One"},
		"spec": map[string]any{"description": "Drafted by an agent"}}
	callTool(t, cs, "save_draft", map[string]any{"kind": "Goal", "id": "g-sound", "manifest": goal})
	callTool(t, cs, "save_draft", map[string]any{"kind": "Team", "id": "t1", "manifest": team})

	res, text := callTool(t, cs, "checks", map[string]any{"kind": "Team", "id": "t1"})
	if res.IsError || !strings.Contains(text, `"openInChangeSet":[{`) || !strings.Contains(text, `"id":"g-sound"`) {
		t.Fatalf("checks on the team does not list what is open on the goal: %s", text)
	}
	res, set := callTool(t, cs, "checks", map[string]any{})
	if res.IsError {
		t.Fatalf("checks on the whole set: %s", set)
	}
	var whole struct {
		Open []map[string]any `json:"openInChangeSet"`
	}
	if err := json.Unmarshal([]byte(set), &whole); err != nil {
		t.Fatal(err)
	}
	res, refused := callTool(t, cs, "propose", map[string]any{"reason": "r"})
	if !res.IsError || !strings.Contains(refused, fmt.Sprintf("%d check", len(whole.Open))) {
		t.Fatalf("checks found %d open; propose said: %s", len(whole.Open), refused)
	}
}

// An agent follows what it proposed: my_proposals lists the change set
// with its items, and checks still reads the set's drafts until the
// person accepts it.
func TestAnAgentFollowsTheChangeSetItProposed(t *testing.T) {
	t.Parallel()
	_, _, cs := setup(t, nil)
	team := map[string]any{"apiVersion": "cartograph/v1", "kind": "Team", "metadata": map[string]any{"id": "t1", "name": "Team One"},
		"spec": map[string]any{"description": "Drafted by an agent"}}
	callTool(t, cs, "start_work", map[string]any{"title": "Describe the team"})
	callTool(t, cs, "save_draft", map[string]any{"kind": "Team", "id": "t1", "manifest": team})
	if res, text := callTool(t, cs, "propose", map[string]any{"reason": "describe the team"}); res.IsError {
		t.Fatalf("propose: %s", text)
	}
	res, text := callTool(t, cs, "my_proposals", map[string]any{})
	if res.IsError || !strings.Contains(text, `"title":"Describe the team"`) || !strings.Contains(text, `"Team/t1"`) {
		t.Fatalf("my_proposals does not list the change set: %s", text)
	}
	res, text = callTool(t, cs, "get", map[string]any{"kind": "Team", "id": "t1"})
	if res.IsError || !strings.Contains(text, "Drafted by an agent") {
		t.Fatalf("after proposing, get reads the record, not the proposed draft: %s", text)
	}
}

// A draft saved under the wrong id is discarded, not left to block the
// change set; one another draft names is kept until that one changes.
func TestAnAgentDiscardsADraft(t *testing.T) {
	t.Parallel()
	_, _, cs := setup(t, nil)
	bad := map[string]any{"apiVersion": "cartograph/v1", "kind": "Purpose", "metadata": map[string]any{"id": "purpose", "name": "Purpose"}, "spec": map[string]any{"organisation": "Example"}}
	callTool(t, cs, "save_draft", map[string]any{"kind": "Purpose", "id": "purpose", "manifest": bad})
	if res, text := callTool(t, cs, "discard_draft", map[string]any{"kind": "Purpose", "id": "purpose"}); res.IsError {
		t.Fatalf("discard: %s", text)
	}
	if res, text := callTool(t, cs, "checks", map[string]any{}); res.IsError || strings.Contains(text, "Purpose") {
		t.Fatalf("the discarded draft is still in the set: %s", text)
	}
	if res, text := callTool(t, cs, "guide", map[string]any{"kind": "Purpose"}); res.IsError || !strings.Contains(text, `"id":"default"`) {
		t.Fatalf("the Purpose template does not give its id: %.300s", text)
	}
	if res, _ := callTool(t, cs, "edit_draft", map[string]any{"kind": "Team", "id": "t1", "unset": []string{"/"}}); !res.IsError {
		t.Fatal("unsetting / was taken as an edit")
	}
}

// What an agent drafts in its change set counts wherever it reads: a
// manifest naming a drafted team validates, and a guide's plan does not
// call a drafted cycle missing.
func TestReadingToolsSeeTheChangeSetsDrafts(t *testing.T) {
	t.Parallel()
	_, _, cs := setup(t, nil)
	team := map[string]any{"apiVersion": "cartograph/v1", "kind": "Team", "metadata": map[string]any{"id": "t-new", "name": "New team"}, "spec": map[string]any{"description": "Drafted"}}
	cycle := map[string]any{"apiVersion": "cartograph/v1", "kind": "ReportingCycle", "metadata": map[string]any{"id": "quarterly", "name": "Quarterly"}, "spec": map[string]any{"periodMonths": 3, "startMonth": 1}}
	callTool(t, cs, "save_draft", map[string]any{"kind": "Team", "id": "t-new", "manifest": team})
	callTool(t, cs, "save_draft", map[string]any{"kind": "ReportingCycle", "id": "quarterly", "manifest": cycle})
	source := map[string]any{"apiVersion": "cartograph/v1", "kind": "DataSource", "metadata": map[string]any{"id": "d-new", "name": "New source"}, "spec": map[string]any{"category": "database", "team": "t-new"}}
	res, text := callTool(t, cs, "validate", map[string]any{"kind": "DataSource", "id": "d-new", "manifest": source})
	if res.IsError || strings.Contains(text, "does not exist") {
		t.Fatalf("validate does not see the drafted team: %s", text)
	}
	res, text = callTool(t, cs, "guide", map[string]any{"kind": "KPI"})
	if res.IsError {
		t.Fatal(text)
	}
	var g struct {
		Plan []struct {
			Kind    string `json:"kind"`
			Missing bool   `json:"missing"`
		} `json:"plan"`
	}
	if err := json.Unmarshal([]byte(text), &g); err != nil {
		t.Fatal(err)
	}
	for _, p := range g.Plan {
		if p.Kind == "ReportingCycle" && p.Missing {
			t.Fatalf("the guide calls the drafted cycle missing: %s", text)
		}
	}
}

// An agent leaves a check only its person can settle, with the reason,
// as it goes: next passes it by, checks lists it apart, and propose
// waives it with that reason.
func TestAnAgentLeavesACheckForItsPerson(t *testing.T) {
	t.Parallel()
	e, _, cs := setup(t, nil)
	goal := map[string]any{"apiVersion": "cartograph/v1", "kind": "Goal", "metadata": map[string]any{"id": "g-sound", "name": "Sound fruit"},
		"spec": map[string]any{"level": "goal", "objective": "Sound fruit"}}
	callTool(t, cs, "start_work", map[string]any{"title": "A goal"})
	callTool(t, cs, "save_draft", map[string]any{"kind": "Goal", "id": "g-sound", "manifest": goal})
	_, before := callTool(t, cs, "checks", map[string]any{"kind": "Goal", "id": "g-sound"})
	var report struct {
		Open []struct {
			ID string `json:"id"`
		} `json:"open"`
	}
	if err := json.Unmarshal([]byte(before), &report); err != nil || len(report.Open) == 0 {
		t.Fatalf("no open checks to leave: %s", before)
	}
	for _, o := range report.Open {
		if res, text := callTool(t, cs, "leave_open", map[string]any{"asked": "Asked who sets it, because the document is silent; they will decide later", "kind": "Goal", "id": "g-sound", "check": o.ID, "reason": "Only the person knows " + o.ID}); res.IsError {
			t.Fatalf("leave_open: %s", text)
		}
	}
	// Leaving a check without having asked is refused: the person may
	// know the figure, or decide it now.
	if res, text := callTool(t, cs, "leave_open", map[string]any{"kind": "Goal", "id": "g-sound", "check": report.Open[0].ID, "reason": "Unknown"}); !res.IsError || !strings.Contains(text, "ask your person first") {
		t.Fatalf("a check was left without asking: %s", text)
	}
	// A second missing fact behind the same check adds its reason.
	callTool(t, cs, "leave_open", map[string]any{"asked": "Asked who sets it, because the document is silent; they will decide later", "kind": "Goal", "id": "g-sound", "check": report.Open[0].ID, "reason": "A second fact is missing"})
	_, after := callTool(t, cs, "checks", map[string]any{"kind": "Goal", "id": "g-sound"})
	if !strings.Contains(after, "Only the person knows "+report.Open[0].ID+" Also: A second fact is missing") {
		t.Fatalf("the first reason was lost: %s", after)
	}
	// A correction puts its reason in place of the first.
	callTool(t, cs, "leave_open", map[string]any{"asked": "Asked who sets it, because the document is silent; they will decide later", "kind": "Goal", "id": "g-sound", "check": report.Open[0].ID, "reason": "Only the person knows " + report.Open[0].ID, "correct": true})
	_, after = callTool(t, cs, "checks", map[string]any{"kind": "Goal", "id": "g-sound"})
	if strings.Contains(after, "A second fact is missing") {
		t.Fatalf("a correction kept the reason it corrects: %s", after)
	}
	if !strings.Contains(after, `"left":[`) || !strings.Contains(after, `"open":[]`) {
		t.Fatalf("the left checks are still open: %s", after)
	}
	if res, text := callTool(t, cs, "propose", map[string]any{"reason": "a goal"}); res.IsError {
		t.Fatalf("propose refused what was left with reasons: %s", text)
	}
	sets, _ := e.ChangeSets(identity.WithPrincipal(context.Background(), ada), "proposed", false)
	if len(sets) == 0 || len(sets[0].Waivers) != len(report.Open) || !strings.HasPrefix(sets[0].Waivers[0].Reason, "Only the person knows") {
		t.Fatalf("the proposal's waivers: %+v", sets)
	}
}

// taxonomy says where each part of a document goes, and what stays out,
// so an agent porting a charter has nothing to judge that the discipline
// has settled.
func TestTheTaxonomySaysWhereEachPartOfADocumentGoes(t *testing.T) {
	t.Parallel()
	_, _, cs := setup(t, nil)
	res, text := callTool(t, cs, "taxonomy", map[string]any{"full": true})
	if res.IsError {
		t.Fatal(text)
	}
	for _, want := range []string{`"porting":[`, "A workstream or strand whose result is an output", "Not ported", "governanceBody", "A measure taken once"} {
		if !strings.Contains(text, want) {
			t.Errorf("the porting map lacks %q", want)
		}
	}
	// By default, where each part goes and no more; how, by part.
	_, brief := callTool(t, cs, "taxonomy", map[string]any{})
	if strings.Contains(brief, `"how":"`+porting0How()) || len(brief) > len(text)/2 {
		t.Errorf("the default map is %d long against %d in full", len(brief), len(text))
	}
	_, part := callTool(t, cs, "taxonomy", map[string]any{"part": "workstream"})
	if !strings.Contains(part, "A workstream or strand whose result is an output") || strings.Contains(part, `"kinds"`) {
		t.Errorf("by part: %s", part)
	}
}

// A register draft nothing names is pointed out before propose.
func TestChecksListDraftsNothingNames(t *testing.T) {
	t.Parallel()
	_, _, cs := setup(t, nil)
	team := map[string]any{"apiVersion": "cartograph/v1", "kind": "Team", "metadata": map[string]any{"id": "t-lonely", "name": "Lonely team"}, "spec": map[string]any{"description": "Named by nothing"}}
	callTool(t, cs, "save_draft", map[string]any{"kind": "Team", "id": "t-lonely", "manifest": team})
	if res, text := callTool(t, cs, "checks", map[string]any{}); res.IsError || !strings.Contains(text, `"namedByNothing":["Team/t-lonely"]`) {
		t.Fatalf("an unnamed draft is not pointed out: %s", text)
	}
}

// Once a stage is drafted with nothing open, next names the stage after
// it rather than telling the agent to propose; checks on the whole set
// says one thing.
func TestNextMovesOnFromADraftedStage(t *testing.T) {
	t.Parallel()
	_, _, cs := setup(t, nil)
	purpose := map[string]any{"apiVersion": "cartograph/v1", "kind": "Purpose", "metadata": map[string]any{"id": "default", "name": "Purpose"},
		"spec": map[string]any{"organisation": "Example Cooperative", "vision": "Every member's produce reaches a buyer sound.", "mission": "We check, store and move produce for our members."}}
	callTool(t, cs, "start_work", map[string]any{"title": "Our purpose"})
	callTool(t, cs, "save_draft", map[string]any{"kind": "Purpose", "id": "default", "manifest": purpose})
	res, text := callTool(t, cs, "next", map[string]any{})
	if res.IsError || !strings.Contains(text, "the goal stage") {
		t.Fatalf("next after a drafted purpose: %s", text)
	}
	res, text = callTool(t, cs, "checks", map[string]any{})
	if res.IsError || strings.Contains(text, "Every check is met") && strings.Contains(text, "still open") {
		t.Fatalf("checks on the set contradicts itself: %s", text)
	}
}

// A second save of a whole manifest would undo every edit made to the
// draft since; it is refused unless the agent means to replace it.
func TestASaveDoesNotClobberADraft(t *testing.T) {
	t.Parallel()
	_, _, cs := setup(t, nil)
	goal := map[string]any{"apiVersion": "cartograph/v1", "kind": "Goal", "metadata": map[string]any{"id": "g-once", "name": "Sound fruit"},
		"spec": map[string]any{"level": "goal", "objective": "Sound fruit"}}
	callTool(t, cs, "start_work", map[string]any{"title": "A goal"})
	if res, text := callTool(t, cs, "save_draft", map[string]any{"kind": "Goal", "id": "g-once", "manifest": goal}); res.IsError {
		t.Fatalf("first save: %s", text)
	}
	if res, text := callTool(t, cs, "save_draft", map[string]any{"kind": "Goal", "id": "g-once", "manifest": goal}); !res.IsError || !strings.Contains(text, "edit_draft") {
		t.Fatalf("a second save was taken: %s", text)
	}
	if res, text := callTool(t, cs, "save_draft", map[string]any{"kind": "Goal", "id": "g-once", "manifest": goal, "replace": true}); res.IsError {
		t.Fatalf("a replace was refused: %s", text)
	}
}

// A KPI's target left for the person leaves the aims it measures waiting
// on the same fact: they are left with its reason, and propose takes it.
func TestAnAimWaitsOnItsKPIsLeftFigure(t *testing.T) {
	t.Parallel()
	_, _, cs := setup(t, nil)
	callTool(t, cs, "start_work", map[string]any{"title": "A measured goal"})
	goal := map[string]any{"apiVersion": "cartograph/v1", "kind": "Goal", "metadata": map[string]any{"id": "g-graded", "name": "Graded fruit"},
		"spec": map[string]any{"level": "goal", "objective": "Grade every lot the same way"}}
	kpi := map[string]any{"apiVersion": "cartograph/v1", "kind": "KPI", "metadata": map[string]any{"id": "k-graded", "name": "Lots graded"},
		"spec": map[string]any{"definition": "Lots graded at intake", "direction": "increase", "goals": []any{"g-graded"}}}
	callTool(t, cs, "save_draft", map[string]any{"kind": "Goal", "id": "g-graded", "manifest": goal})
	if res, text := callTool(t, cs, "save_draft", map[string]any{"kind": "KPI", "id": "k-graded", "manifest": kpi}); res.IsError {
		t.Fatalf("save the KPI: %s", text)
	}
	callTool(t, cs, "leave_open", map[string]any{"asked": "Asked who sets it, because the document is silent; they will decide later", "kind": "KPI", "id": "k-graded", "check": "kpi-target", "reason": "The board sets the target after the baseline"})
	_, text := callTool(t, cs, "checks", map[string]any{"kind": "Goal", "id": "g-graded"})
	var report struct {
		Open []struct {
			ID string `json:"id"`
		} `json:"open"`
	}
	if err := json.Unmarshal([]byte(text), &report); err != nil {
		t.Fatal(err)
	}
	for _, o := range report.Open {
		if o.ID == "smart-measurable" || o.ID == "smart-time-bound" {
			t.Fatalf("an aim still asks for the figure its KPI waits on: %s", text)
		}
	}
	if !strings.Contains(text, "The board sets the target after the baseline") {
		t.Fatalf("the aim does not say what it waits on: %s", text)
	}
}

// A vision and mission left for the person leave a goal's relevance
// waiting on them, with their reason, once the goal says why it matters.
func TestAGoalWaitsOnAVisionLeftOpen(t *testing.T) {
	t.Parallel()
	_, _, cs := setup(t, nil)
	callTool(t, cs, "start_work", map[string]any{"title": "A goal without a vision"})
	purpose := map[string]any{"apiVersion": "cartograph/v1", "kind": "Purpose", "metadata": map[string]any{"id": "default", "name": "Purpose"},
		"spec": map[string]any{"organisation": "Example Cooperative"}}
	goal := map[string]any{"apiVersion": "cartograph/v1", "kind": "Goal", "metadata": map[string]any{"id": "g-quality", "name": "Quality"},
		"spec": map[string]any{"level": "goal", "objective": "Raise produce quality", "whyItMatters": "Buyers reject bruised produce."}}
	callTool(t, cs, "save_draft", map[string]any{"kind": "Purpose", "id": "default", "manifest": purpose})
	callTool(t, cs, "leave_open", map[string]any{"asked": "Asked who sets it, because the document is silent; they will decide later", "kind": "Purpose", "id": "default", "check": "purpose-vision", "reason": "Only the board can state the vision",
		"also": []any{map[string]any{"kind": "Purpose", "id": "default", "check": "purpose-mission"}}})
	callTool(t, cs, "save_draft", map[string]any{"kind": "Goal", "id": "g-quality", "manifest": goal})
	_, text := callTool(t, cs, "checks", map[string]any{"kind": "Goal", "id": "g-quality"})
	if !strings.Contains(text, "Only the board can state the vision") {
		t.Fatalf("the goal's relevance does not wait on the vision: %s", text)
	}
}

// A person brings their agent into a change set of theirs: the agent
// finds it, works in it beside them, and leaves proposing it to them. It
// never reaches another person's change set, and left to choose it keeps
// to its own.
func TestAnAgentHelpsInItsPersonsChangeSet(t *testing.T) {
	t.Parallel()
	e, _, cs := setup(t, nil)
	ctx := context.Background()
	adaCtx := identity.WithPrincipal(ctx, ada)
	set, err := e.StartChangeSet(adaCtx, "Cut loss after picking", "")
	if err != nil {
		t.Fatal(err)
	}
	goal := "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: g9\n  name: Cut loss after picking\nspec:\n  level: goal\n  objective: Less fruit is lost\n"
	if err := e.SaveInChangeSet(adaCtx, set.ID, "Goal", "g9", []byte(goal)); err != nil {
		t.Fatal(err)
	}
	bob := identity.WithPrincipal(ctx, identity.Principal{Subject: "bob@example.org", Email: "bob@example.org", Name: "Bob"})
	other, err := e.StartChangeSet(bob, "Bob's work", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.SaveInChangeSet(bob, other.ID, "Team", "t1", []byte(team)); err != nil {
		t.Fatal(err)
	}

	res, text := callTool(t, cs, "change_sets", map[string]any{})
	if res.IsError || !strings.Contains(text, set.ID) || !strings.Contains(text, `"yours":false`) || !strings.Contains(text, `"workedBy":"your person"`) ||
		!strings.Contains(text, `"Goal/g9"`) || strings.Contains(text, other.ID) {
		t.Fatalf("change_sets does not list Ada's work, and only hers: %s", text)
	}
	if strings.Contains(text, `"open":0`) {
		t.Fatalf("change_sets counts nothing open on a goal with no measure: %s", text)
	}

	res, text = callTool(t, cs, "edit_draft", map[string]any{"changeSet": set.ID, "kind": "Goal", "id": "g9", "set": map[string]any{"/spec/whyItMatters": "Members are paid by what arrives sound"}})
	if res.IsError {
		t.Fatalf("edit_draft in Ada's change set: %s", text)
	}
	got, in, err := e.ChangeSetText(adaCtx, set.ID, "Goal", "g9")
	if err != nil || !in || !strings.Contains(string(got), "Members are paid by what arrives sound") {
		t.Fatalf("the agent's edit is not in Ada's change set: %s (%v)", got, err)
	}
	agentCtx := identity.WithPrincipal(ctx, identity.Principal{Subject: "ada@example.org", Email: "ada@example.org", Name: "Ada", Agent: "Claude"})
	if own, found, _ := e.CurrentChangeSet(agentCtx, ""); found {
		t.Fatalf("working in Ada's change set opened one of the agent's own: %+v", own)
	}
	// Told how the work ends here: Ada proposes and merges it.
	if res, text := callTool(t, cs, "checks", map[string]any{"changeSet": set.ID}); res.IsError || !strings.Contains(text, "openInChangeSet") ||
		!strings.Contains(text, "theirs to propose and merge") || strings.Contains(text, "with propose") {
		t.Fatalf("checks on Ada's change set: %s", text)
	}
	if res, text := callTool(t, cs, "propose", map[string]any{"changeSet": set.ID, "reason": "ready"}); !res.IsError || !strings.Contains(text, "propose it themselves") {
		t.Fatalf("the agent proposed Ada's change set: %s", text)
	}
	if res, text := callTool(t, cs, "edit_draft", map[string]any{"changeSet": other.ID, "kind": "Team", "id": "t1", "set": map[string]any{"/spec/description": "x"}}); !res.IsError {
		t.Fatalf("the agent worked in Bob's change set: %s", text)
	}
}

// An agent reads the components graph and a project's schedule: what
// holds the work up and when each milestone falls.
func TestAnAgentReadsTheComponentsAndTheSchedule(t *testing.T) {
	t.Parallel()
	e, _, cs := setup(t, nil)
	ctx := identity.WithPrincipal(context.Background(), ada)
	set, err := e.StartChangeSet(ctx, "Plan the pilot", "")
	if err != nil {
		t.Fatal(err)
	}
	project := "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: pilot\n  name: Pilot\nspec:\n  team: t1\n  milestones:\n" +
		"    - {id: m1, name: Kick-off, timing: {form: date, date: \"2026-01\"}}\n" +
		"    - {id: m2, name: Report, timing: {form: after, event: {on: {local: milestones, id: m1}}, lagMonths: 3}}\n"
	if err := e.SaveInChangeSet(ctx, set.ID, "Project", "pilot", []byte(project)); err != nil {
		t.Fatal(err)
	}
	res, text := callTool(t, cs, "schedule", map[string]any{"changeSet": set.ID, "kind": "Project", "id": "pilot"})
	if res.IsError || !strings.Contains(text, `"month":"2026-04"`) {
		t.Fatalf("schedule: %s", text)
	}
	res, text = callTool(t, cs, "components", map[string]any{"changeSet": set.ID})
	if res.IsError || !strings.Contains(text, `"work":"Project/pilot"`) || !strings.Contains(text, `"months":4`) {
		t.Fatalf("components: %s", text)
	}
}

// Structure first: an agent answers the questions for each piece and is
// told what each is and the order to write them (TAXONOMY.md D56).
func TestAnAgentIsToldTheStructure(t *testing.T) {
	t.Parallel()
	_, _, cs := setup(t, nil)
	res, text := callTool(t, cs, "structure", map[string]any{"pieces": []any{
		map[string]any{"name": "Rollout", "none": true},
		map[string]any{"name": "Handbook", "outputOf": "Rollout"},
		map[string]any{"name": "Survey", "changeOfItsOwn": true, "change": "sets the baseline", "dependedOnBy": []any{"Rollout"}},
	}})
	if res.IsError || !strings.Contains(text, `"order":["Survey","Rollout"]`) || !strings.Contains(text, `"kind":"Deliverable"`) {
		t.Fatalf("structure: %s", text)
	}
	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tl := range tools.Tools {
		if tl.Name == "structure" && !strings.Contains(tl.Description, "Is it an output another piece of work hands over") {
			t.Fatalf("the description lacks the questions: %s", tl.Description)
		}
	}
}

// The work is a plan: next names the first record of it not written yet,
// so an agent writes a whole port one record at a time.
func TestNextLeadsThroughRecordsNotWrittenYet(t *testing.T) {
	t.Parallel()
	_, _, cs := setup(t, nil)
	callTool(t, cs, "start_work", map[string]any{"title": "Plan"})
	res, text := callTool(t, cs, "next", map[string]any{"work": []any{"Team/t1", "Goal/new-goal", "Project/new-project"}})
	if res.IsError || !strings.Contains(text, `"write":"Goal/new-goal"`) || !strings.Contains(text, "1 of 3") {
		t.Fatalf("next: %s", text)
	}
}

// Names sent where pieces go are answered with the shape, not refused;
// pieces are answered with the work to pass to next, ids generated.
func TestStructureTeachesItsShapeAndHandsOverTheWork(t *testing.T) {
	t.Parallel()
	_, _, cs := setup(t, nil)
	res, text := callTool(t, cs, "structure", map[string]any{"pieces": []any{"Rollout", "Handbook"}})
	if res.IsError || !strings.Contains(text, "not text") || !strings.Contains(text, `\"outputOf\"`) {
		t.Fatalf("names: %s", text)
	}
	res, text = callTool(t, cs, "structure", map[string]any{"pieces": []any{
		map[string]any{"name": "Rollout", "none": true}, map[string]any{"name": "Handbook", "outputOf": "Rollout"},
		map[string]any{"name": "Survey", "changeOfItsOwn": true, "change": "sets the baseline", "dependedOnBy": []any{"Rollout"}},
	}})
	var out struct{ Work []string }
	if res.IsError || json.Unmarshal([]byte(text), &out) != nil || len(out.Work) != 2 || !strings.HasPrefix(out.Work[0], "Project/project-") {
		t.Fatalf("pieces: %s", text)
	}
	_, text = callTool(t, cs, "next", map[string]any{"work": out.Work})
	if !strings.Contains(text, `"draft"`) || !strings.Contains(text, strings.TrimPrefix(out.Work[0], "Project/")) {
		t.Fatalf("next: %s", text)
	}
}

// What a piece is, is the answer: a kind or a key the questions do not
// ask is refused with what to send instead, never silently ignored.
func TestStructureRefusesWhatItDoesNotAsk(t *testing.T) {
	t.Parallel()
	_, _, cs := setup(t, nil)
	_, text := callTool(t, cs, "structure", map[string]any{"pieces": []any{
		map[string]any{"name": "Rollout", "kind": "Project"},
		map[string]any{"name": "Pupils served", "kind": "KPI"},
	}})
	if !strings.Contains(text, `\"kind\" is not an answer`) || !strings.Contains(text, "is a KPI, not a piece of work") || strings.Contains(text, `"work"`) {
		t.Fatalf("structure: %s", text)
	}
}

// Many records in one call, each checked, and what comes next.
func TestSaveDraftsWritesManyAndSaysWhatComesNext(t *testing.T) {
	t.Parallel()
	_, _, cs := setup(t, nil)
	callTool(t, cs, "start_work", map[string]any{"title": "Plan"})
	goal := func(id, name string) map[string]any {
		return map[string]any{"apiVersion": "cartograph/v1", "kind": "Goal", "metadata": map[string]any{"id": id, "name": name}, "spec": map[string]any{"level": "goal"}}
	}
	res, text := callTool(t, cs, "save_drafts", map[string]any{
		"manifests": []any{goal("g-one", "Fair prices"), goal("g-two", "Steady supply")},
		"work":      []any{"Goal/g-one", "Goal/g-two", "Project/p-new"},
	})
	if res.IsError || strings.Count(text, `"saved":"draft`) != 2 || !strings.Contains(text, `"write":"Project/p-new"`) {
		t.Fatalf("save_drafts: %s", text)
	}
}

// porting0How is a phrase only the how of the porting map holds.
func porting0How() string { return "Ask in the document's own words" }

// start_work with the structure's answers drafts every record, linked:
// the survey a component of the rollout, the handbook its deliverable,
// the outside scheme a scope-out line, the checks service an operation.
func TestStartWorkDraftsTheStructure(t *testing.T) {
	t.Parallel()
	_, _, cs := setup(t, nil)
	res, text := callTool(t, cs, "start_work", map[string]any{"title": "Port", "pieces": []any{
		map[string]any{"name": "Rollout", "none": true},
		map[string]any{"name": "Handbook", "outputOf": "Rollout"},
		map[string]any{"name": "Baseline survey", "changeOfItsOwn": true, "change": "sets the baseline", "dependedOnBy": []any{"Rollout"}},
		map[string]any{"name": "Compliance checks", "ongoing": true},
		map[string]any{"name": "Farm supply scheme", "outOfScope": true},
	}})
	var out struct {
		ChangeSet string
		Work      []string
	}
	if res.IsError || json.Unmarshal([]byte(text), &out) != nil || len(out.Work) != 3 {
		t.Fatalf("start_work: %s", text)
	}
	var rollout, survey string
	for _, w := range out.Work {
		_, text := callTool(t, cs, "get", map[string]any{"kind": strings.Split(w, "/")[0], "id": strings.Split(w, "/")[1], "changeSet": out.ChangeSet})
		switch {
		case strings.Contains(text, "Rollout"):
			rollout = text
		case strings.Contains(text, "Baseline survey"):
			survey = w
		}
	}
	if !strings.Contains(rollout, strings.TrimPrefix(survey, "Project/")) || !strings.Contains(rollout, "Handbook") || !strings.Contains(rollout, "Farm supply scheme") {
		t.Errorf("the rollout's draft: %s", rollout)
	}
}

// next hands over a whole record's open checks, each with the field that
// settles it, so one edit settles the record.
func TestNextFillsAWholeRecord(t *testing.T) {
	t.Parallel()
	_, _, cs := setup(t, nil)
	_, text := callTool(t, cs, "start_work", map[string]any{"title": "Port", "pieces": []any{map[string]any{"name": "Rollout", "none": true}}})
	var out struct{ Work []string }
	_ = json.Unmarshal([]byte(text), &out)
	_, text = callTool(t, cs, "next", map[string]any{"work": out.Work})
	if !strings.Contains(text, `"fill":[`) || !strings.Contains(text, `"field":"/`) || !strings.Contains(text, "every check in fill at once") {
		t.Fatalf("next: %s", text)
	}
}

// One settle per record: the fields the documents give set, the checks
// they do not answer left with their reason, what comes next handed over;
// and the summary says what the change set really holds.
func TestSettleARecordInOneCall(t *testing.T) {
	t.Parallel()
	_, _, cs := setup(t, nil)
	_, text := callTool(t, cs, "start_work", map[string]any{"title": "Port", "pieces": []any{
		map[string]any{"name": "Rollout", "none": true}, map[string]any{"name": "Survey", "changeOfItsOwn": true, "change": "sets the baseline", "dependedOnBy": []any{"Rollout"}},
	}})
	var out struct{ Work []string }
	_ = json.Unmarshal([]byte(text), &out)
	kind, id, _ := strings.Cut(out.Work[0], "/")
	res, text := callTool(t, cs, "settle", map[string]any{"kind": kind, "id": id, "work": out.Work,
		"set":   map[string]any{"/spec/summary/about": "A survey of every depot", "/spec/team": "t1", "/spec/objectives/0/objective": "Faults are found at intake"},
		"open":  []any{map[string]any{"check": "resources-funding", "reason": "No budget is given"}},
		"asked": "not available"})
	if res.IsError || !strings.Contains(text, `"then":`) || !strings.Contains(text, "resources-funding") {
		t.Fatalf("settle: %s", text)
	}
	// A second objective is refused, and nothing is saved.
	res, text = callTool(t, cs, "settle", map[string]any{"kind": kind, "id": id, "set": map[string]any{"/spec/objectives/1/objective": "Buyers are paid"}})
	if !res.IsError || !strings.Contains(text, "/spec/objectives: maxItems") {
		t.Fatalf("a second objective: %s", text)
	}
	_, text = callTool(t, cs, "work_summary", map[string]any{})
	if !strings.Contains(text, `"objectives":1`) || !strings.Contains(text, `"leftForPerson":1`) || !strings.Contains(text, `"components":1`) {
		t.Fatalf("summary: %s", text)
	}
}

// settle reads fields sent beside kind and id or under fields, and a
// refused shape says which fields go where it was sent.
func TestSettleReadsFieldsWhereverSentAndTeachesTheShape(t *testing.T) {
	t.Parallel()
	_, _, cs := setup(t, nil)
	_, text := callTool(t, cs, "start_work", map[string]any{"title": "Port", "pieces": []any{map[string]any{"name": "Rollout", "none": true}}})
	var out struct{ Work []string }
	_ = json.Unmarshal([]byte(text), &out)
	kind, id, _ := strings.Cut(out.Work[0], "/")
	res, text := callTool(t, cs, "settle", map[string]any{"kind": kind, "id": id, "/spec/summary/about": "Checks at every depot", "fields": map[string]any{"/spec/team": "t1"}})
	if res.IsError {
		t.Fatalf("pointers beside kind and id: %s", text)
	}
	res, text = callTool(t, cs, "settle", map[string]any{"kind": kind, "id": id, "set": map[string]any{"/spec/summary/problems": []any{map[string]any{"statement": "Faults reach buyers"}}}})
	if !res.IsError || !strings.Contains(text, "/spec/summary/problems/0/problem/situation") {
		t.Fatalf("a made-up shape: %s", text)
	}
}

// calls is a trace recorder that keeps calls in memory.
type calls struct {
	mu   sync.Mutex
	seen []trace.Call
}

func (c *calls) Record(tc trace.Call) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seen = append(c.seen, tc)
}

// Every tool call is traced by its shape, under OpenTelemetry's names
// (docs/adr/0028): the tool, the record, the keys sent, how it ended and
// why; never what the arguments said.
func TestEveryToolCallIsTracedByItsShape(t *testing.T) {
	t.Parallel()
	rec := &calls{}
	_, _, cs := setupTraced(t, nil, rec)
	callTool(t, cs, "start_work", map[string]any{"title": "Secret plan"})
	callTool(t, cs, "edit_draft", map[string]any{"kind": "Project", "id": "p1", "set": map[string]any{"/spec/objectives/0/objective": "One", "/spec/objectives/1/objective": "Two"}})
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.seen) != 2 {
		t.Fatalf("calls traced: %d", len(rec.seen))
	}
	start, edit := rec.seen[0], rec.seen[1]
	if start.Method != "tools/call" || start.Operation != "execute_tool" || start.Tool != "start_work" || start.Outcome != trace.OK || start.Session == "" {
		t.Errorf("start_work: %+v", start)
	}
	if edit.Record != "Project/p1" || edit.Outcome != trace.Refused || edit.Defect != "partial" || edit.Problems == 0 || len(edit.Paths) == 0 {
		t.Errorf("a refused edit: %+v", edit)
	}
	b, _ := json.Marshal(rec.seen)
	if strings.Contains(string(b), "Secret plan") || !strings.Contains(string(b), `"gen_ai.tool.name":"edit_draft"`) {
		t.Errorf("a trace carried an argument's value, or lacks the conventions' names: %s", b)
	}
}

// A name where a register's reference goes finds the register, or drafts
// it: the team, and the sponsor's role, come into being as the project is
// settled, and the same name a second time is the same record.
func TestSettleFindsOrDraftsTheRegistersItNames(t *testing.T) {
	t.Parallel()
	_, _, cs := setup(t, nil)
	_, text := callTool(t, cs, "start_work", map[string]any{"title": "Port", "pieces": []any{map[string]any{"name": "Rollout", "none": true}}})
	var out struct{ Work []string }
	_ = json.Unmarshal([]byte(text), &out)
	kind, id, _ := strings.Cut(out.Work[0], "/")
	res, text := callTool(t, cs, "settle", map[string]any{"kind": kind, "id": id, "set": map[string]any{
		"/spec/team":      "Team One",
		"/spec/resources": []any{map[string]any{"role": "sponsor", "resource": "Permanent Secretary"}, map[string]any{"role": "manager", "resource": "Permanent Secretary"}},
	}})
	if res.IsError || strings.Contains(text, "Team/") || strings.Count(text, "Resource/resource-") != 1 {
		t.Fatalf("settle: %s", text)
	}
	_, text = callTool(t, cs, "get", map[string]any{"kind": kind, "id": id})
	if !strings.Contains(text, "team: t1") {
		t.Errorf("the existing team, found by its name: %s", text)
	}
}

// A settle with one field built wrong keeps the rest: a refused field
// costs only itself.
func TestSettleKeepsWhatItCan(t *testing.T) {
	t.Parallel()
	_, _, cs := setup(t, nil)
	_, text := callTool(t, cs, "start_work", map[string]any{"title": "Port", "pieces": []any{map[string]any{"name": "Rollout", "none": true}}})
	var out struct{ Work []string }
	_ = json.Unmarshal([]byte(text), &out)
	kind, id, _ := strings.Cut(out.Work[0], "/")
	_, text = callTool(t, cs, "settle", map[string]any{"kind": kind, "id": id, "set": map[string]any{
		"/spec/summary/about":   "Checks at every depot",
		"/spec/summary/scopeIn": []any{"Every depot"},
		"/spec/deliverables":    []any{map[string]any{"title": "A made-up field"}},
	}})
	if !strings.Contains(text, `"refused":[{"path":"/spec/deliverables/0"`) {
		t.Fatalf("settle: %s", text)
	}
	_, text = callTool(t, cs, "get", map[string]any{"kind": kind, "id": id})
	if !strings.Contains(text, "Checks at every depot") || !strings.Contains(text, "Every depot") {
		t.Errorf("the fields that could be kept were not: %s", text)
	}
}

// With no person to ask, what the document itself says is written, not
// left open: a port is not proposed hollow.
func TestAnAgentCannotWaiveWhatTheDocumentSays(t *testing.T) {
	t.Parallel()
	_, _, cs := setup(t, nil)
	_, text := callTool(t, cs, "start_work", map[string]any{"title": "Port", "pieces": []any{map[string]any{"name": "Rollout", "none": true}}})
	var out struct{ Work []string }
	_ = json.Unmarshal([]byte(text), &out)
	kind, id, _ := strings.Cut(out.Work[0], "/")
	_, text = callTool(t, cs, "settle", map[string]any{"kind": kind, "id": id, "asked": "not available",
		"open": []any{map[string]any{"check": "deliverables-count", "reason": "Deferred"}}})
	if !strings.Contains(text, `"left":null`) || !strings.Contains(text, "what the document itself says") {
		t.Fatalf("leaving the deliverables open: %s", text)
	}
	res, text := callTool(t, cs, "settle", map[string]any{"kind": kind, "id": id, "set": map[string]any{
		"/spec/summary/scopeIn": []any{"Every depot in the network, from the coast to the hill stations, through the whole season"},
		"/spec/deliverables":    []any{map[string]any{"id": "d1", "name": "Checklist", "due": "2026-09-04"}},
		"/spec/milestones":      []any{map[string]any{"id": "m1", "name": "Pilot ends", "timing": "15/10/2026"}},
		"/spec/summary/about":   "Checks at intake",
		"/spec/team":            "t1",
	}})
	if res.IsError || strings.Contains(text, `"refused"`) || !strings.Contains(text, `"cut"`) {
		t.Fatalf("formats: %s", text)
	}
	_, text = callTool(t, cs, "get", map[string]any{"kind": kind, "id": id})
	if !strings.Contains(text, "date: 2026-09") || !strings.Contains(text, "date: 2026-10") {
		t.Errorf("dates read as months: %s", text)
	}
}

// A document brought in is kept with the work and read a section at a
// time: its outline marks what each section feeds, the work starts in
// the same change set, and each check names the sections that answer it.
func TestADocumentIsBroughtInAndReadBySection(t *testing.T) {
	t.Parallel()
	_, _, cs := setup(t, nil)
	doc := "Depot Checks Charter\n\nA. Background and Authority\nThe board decided in March to check every delivery at intake, and asked the quality lead to run the work across the network this season.\n\n" +
		"D. Scope and Deliverables\nEvery depot is in scope. The work hands over a checklist, a training session for graders and a weekly report to the board from the first month.\n\n" +
		"K. Risks and Issues\nGraders may be short in the harvest weeks; forms may arrive late at the office; the scanner supply may run low during the pilot weeks.\n"
	res, text := callTool(t, cs, "bring_document", map[string]any{"title": "Depot Checks Charter", "text": doc, "fileSize": len(doc)})
	if res.IsError || !strings.Contains(text, `"heading":"D. Scope and Deliverables"`) || !strings.Contains(text, `"/spec/risks"`) {
		t.Fatalf("bring_document: %s", text)
	}
	var brought struct{ ChangeSet string }
	_ = json.Unmarshal([]byte(text), &brought)
	_, text = callTool(t, cs, "read_section", map[string]any{"ids": []any{"s3"}})
	if !strings.Contains(text, "hands over a checklist") || strings.Contains(text, "Graders may be short") {
		t.Fatalf("read_section: %s", text)
	}
	_, text = callTool(t, cs, "start_work", map[string]any{"title": "Port", "pieces": []any{map[string]any{"name": "Depot checks", "none": true}}})
	var started struct {
		ChangeSet string
		Work      []string
	}
	_ = json.Unmarshal([]byte(text), &started)
	if started.ChangeSet != brought.ChangeSet || !strings.Contains(text, `"read":["s3"]`) {
		t.Fatalf("start_work in the document's change set, with sections to read: %s", text)
	}
	// The document is the work's, never a draft: nothing proposes or
	// merges it.
	_, text = callTool(t, cs, "work_summary", map[string]any{})
	if strings.Contains(text, "_Source") {
		t.Errorf("the document shows as a draft: %s", text)
	}
}

// A person's name where a role goes is refused for that field alone,
// with the reason; the rest of the settle is kept.
func TestAPersonsNameCostsOnlyItsField(t *testing.T) {
	t.Parallel()
	_, _, cs := setup(t, nil)
	_, text := callTool(t, cs, "start_work", map[string]any{"title": "Port", "pieces": []any{map[string]any{"name": "Rollout", "none": true}}})
	var out struct{ Work []string }
	_ = json.Unmarshal([]byte(text), &out)
	kind, id, _ := strings.Cut(out.Work[0], "/")
	res, text := callTool(t, cs, "settle", map[string]any{"kind": kind, "id": id, "set": map[string]any{
		"/spec/summary/about": "Checks at every depot",
		"/spec/resources":     []any{map[string]any{"role": "manager", "resource": "Dr. Ada Mensah"}},
	}})
	if res.IsError || !strings.Contains(text, "is not a Resource's name") || !strings.Contains(text, "never a person") {
		t.Fatalf("settle: %s", text)
	}
	_, text = callTool(t, cs, "get", map[string]any{"kind": kind, "id": id})
	if !strings.Contains(text, "Checks at every depot") || strings.Contains(text, "Mensah") {
		t.Errorf("draft: %s", text)
	}
}

// A register in the document is settled in one call: its rows read from
// the table, multi-line cells and all, each a milestone in the project.
func TestARegisterIsSettledFromItsSection(t *testing.T) {
	t.Parallel()
	_, _, cs := setup(t, nil)
	doc := "Depot Checks Charter\n\n" +
		"G1. Milestone Plan\n" +
		"No.        Milestone                     Owner                  Due Date\n\n" +
		"           Checklist agreed with\n" +
		"M1                                       Quality team           15/09/2026\n" +
		"           every depot manager\n\n" +
		"M2         Graders trained               Training unit          20-21/10/2026\n\n" +
		"M3         Pilot season reviewed         Quality team           Term I 2026\n"
	callTool(t, cs, "bring_document", map[string]any{"title": "Depot Checks Charter", "text": doc, "fileSize": len(doc)})
	_, text := callTool(t, cs, "start_work", map[string]any{"title": "Port", "pieces": []any{map[string]any{"name": "Depot checks", "none": true}}})
	var started struct{ Work []string }
	_ = json.Unmarshal([]byte(text), &started)
	kind, id, _ := strings.Cut(started.Work[0], "/")
	res, text := callTool(t, cs, "settle_register", map[string]any{"kind": kind, "id": id, "field": "/spec/milestones", "section": "s2"})
	if res.IsError || !strings.Contains(text, `"added":3`) {
		t.Fatalf("settle_register: %s", text)
	}
	_, text = callTool(t, cs, "get", map[string]any{"kind": kind, "id": id})
	for _, want := range []string{"Checklist agreed with every depot manager", "date: 2026-09", "date: 2026-10", "Graders trained"} {
		if !strings.Contains(text, want) {
			t.Errorf("lacks %q: %s", want, text)
		}
	}
}

// A port in one chain: the document in, then the pieces, and the server
// drafts the structure and writes the registers into the main project.
func TestAPortIsOneChain(t *testing.T) {
	t.Parallel()
	_, _, cs := setup(t, nil)
	doc := "Depot Checks Charter\n\n" +
		"D. Scope and Deliverables\nEvery depot is in scope; the baseline survey of depots is run first, as a piece of its own, and the checks then begin.\n\n" +
		"G1. Milestone Plan\n" +
		"No.        Milestone                     Owner                  Due Date\n\n" +
		"M1         Checklist agreed              Quality team           15/09/2026\n\n" +
		"M2         Graders trained               Training unit          20/10/2026\n\n" +
		"M3         Pilot depots checking         Quality team           02/11/2026\n\n" +
		"M4         Pilot season reviewed         Steering committee     15/12/2026\n"
	res, text := callTool(t, cs, "port", map[string]any{"title": "Depot Checks Charter", "text": doc, "fileSize": len(doc)})
	if res.IsError || !strings.Contains(text, `"piecesIn":["s2"]`) {
		t.Fatalf("port, first call: %s", text)
	}
	res, text = callTool(t, cs, "port", map[string]any{"title": "Depot Checks Charter", "pieces": []any{
		map[string]any{"name": "Depot checks", "none": true},
		map[string]any{"name": "Baseline survey", "changeOfItsOwn": true, "change": "sets the baseline the checks are judged against", "dependedOnBy": []any{"Depot checks"}},
	}})
	if res.IsError || !strings.Contains(text, `"field":"/spec/milestones"`) || !strings.Contains(text, `"added":4`) || !strings.Contains(text, `"fill"`) {
		t.Fatalf("port, second call: %s", text)
	}
	_, text = callTool(t, cs, "work_summary", map[string]any{})
	if !strings.Contains(text, `"milestones":4`) || !strings.Contains(text, `"components":1`) {
		t.Fatalf("summary: %s", text)
	}
}

// start_work after port's first call ends where port's second does: the
// registers are written whichever the agent calls.
func TestStartWorkAfterADocumentIsAPort(t *testing.T) {
	t.Parallel()
	_, _, cs := setup(t, nil)
	doc := "Depot Checks Charter\n\nG1. Milestone Plan\n" +
		"No.        Milestone                     Owner                  Due Date\n\n" +
		"M1         Checklist agreed              Quality team           15/09/2026\n\n" +
		"M2         Graders trained               Training unit          20/10/2026\n\n" +
		"M3         Pilot depots checking         Quality team           02/11/2026\n\n" +
		"M4         Pilot season reviewed         Steering committee     15/12/2026\n"
	_, text := callTool(t, cs, "port", map[string]any{"title": "Depot Checks Charter", "text": doc, "fileSize": len(doc)})
	if !strings.Contains(text, `"questions"`) || !strings.Contains(text, `"example"`) {
		t.Fatalf("port's first answer lacks the questions: %s", text)
	}
	res, text := callTool(t, cs, "start_work", map[string]any{"title": "Port", "pieces": []any{map[string]any{"name": "Depot checks", "none": true}}})
	if res.IsError || !strings.Contains(text, `"field":"/spec/milestones"`) || !strings.Contains(text, `"added":4`) {
		t.Fatalf("start_work after a document: %s", text)
	}
}

// The document's own workstream plan says which names are workstreams: a
// project named after one is refused, a service carrying one is not.
func TestAPortRefusesTheDocumentsWorkstreamsAsProjects(t *testing.T) {
	t.Parallel()
	_, _, cs := setup(t, nil)
	doc := "Depot Checks Charter\n\nE1. Workstream Plan\n" +
		"    Workstream                Purpose                          Lead\n\n" +
		"WS1 Grading and              Agree the checklist and train    Quality team\n" +
		"and Training                 graders at every depot\n\n" +
		"WS2 Depot Inspection         Run the checks each week once    Inspection unit\n" +
		"and Follow-up                the pilot ends, and report\n"
	callTool(t, cs, "port", map[string]any{"title": "Depot Checks Charter", "text": doc, "fileSize": len(doc)})
	_, text := callTool(t, cs, "port", map[string]any{"title": "Depot Checks Charter", "pieces": []any{
		map[string]any{"name": "Depot checks", "none": true},
		map[string]any{"name": "Grading and Training", "changeOfItsOwn": true, "change": "graders apply one checklist", "dependedOnBy": []any{"Depot checks"}},
		map[string]any{"name": "Depot Inspection and Follow-up", "ongoing": true},
	}})
	if !strings.Contains(text, `\"Grading and Training\" is the document's workstream`) || strings.Contains(text, `\"Depot Inspection and Follow-up\" is the document's workstream`) {
		t.Fatalf("port: %s", text)
	}
}

// Re-sending a list the draft already holds merges by id: what the
// server wrote from a register is never lost to a settle.
func TestSettleMergesAListByID(t *testing.T) {
	t.Parallel()
	_, _, cs := setup(t, nil)
	_, text := callTool(t, cs, "start_work", map[string]any{"title": "Port", "pieces": []any{map[string]any{"name": "Rollout", "none": true}}})
	var out struct{ Work []string }
	_ = json.Unmarshal([]byte(text), &out)
	kind, id, _ := strings.Cut(out.Work[0], "/")
	callTool(t, cs, "settle", map[string]any{"kind": kind, "id": id, "set": map[string]any{"/spec/deliverables": []any{
		map[string]any{"id": "d1", "name": "Checklist"}, map[string]any{"id": "d2", "name": "Training"}}}})
	callTool(t, cs, "settle", map[string]any{"kind": kind, "id": id, "set": map[string]any{"/spec/deliverables": []any{
		map[string]any{"id": "d2", "name": "Training for graders"}, map[string]any{"id": "d3", "name": "Weekly report"}}}})
	_, text = callTool(t, cs, "get", map[string]any{"kind": kind, "id": id})
	for _, want := range []string{"Checklist", "Training for graders", "Weekly report"} {
		if !strings.Contains(text, want) {
			t.Errorf("lacks %q: %s", want, text)
		}
	}
}

// Items sent again without ids still merge, by their names: a register
// written by the server is never shrunk by a settle.
func TestSettleMergesItemsByName(t *testing.T) {
	t.Parallel()
	_, _, cs := setup(t, nil)
	_, text := callTool(t, cs, "start_work", map[string]any{"title": "Port", "pieces": []any{map[string]any{"name": "Rollout", "none": true}}})
	var out struct{ Work []string }
	_ = json.Unmarshal([]byte(text), &out)
	kind, id, _ := strings.Cut(out.Work[0], "/")
	callTool(t, cs, "settle", map[string]any{"kind": kind, "id": id, "set": map[string]any{"/spec/risks": []any{
		map[string]any{"id": "r1", "description": "Graders short in harvest weeks", "type": "risk"},
		map[string]any{"id": "r2", "description": "Forms arrive late", "type": "issue"}}}})
	callTool(t, cs, "settle", map[string]any{"kind": kind, "id": id, "set": map[string]any{"/spec/risks": []any{
		map[string]any{"description": "Forms arrive late", "type": "issue", "impact": "high"}}}})
	_, text = callTool(t, cs, "get", map[string]any{"kind": kind, "id": id})
	if !strings.Contains(text, "Graders short") || strings.Count(text, "Forms arrive late") != 1 || !strings.Contains(text, "impact: high") {
		t.Errorf("risks: %s", text)
	}
}

// An item appended with an id or a name the list already holds is merged
// into that item, never added twice.
func TestAnAppendedItemMergesWithItsTwin(t *testing.T) {
	t.Parallel()
	_, _, cs := setup(t, nil)
	_, text := callTool(t, cs, "start_work", map[string]any{"title": "Port", "pieces": []any{map[string]any{"name": "Rollout", "none": true}}})
	var out struct{ Work []string }
	_ = json.Unmarshal([]byte(text), &out)
	kind, id, _ := strings.Cut(out.Work[0], "/")
	callTool(t, cs, "settle", map[string]any{"kind": kind, "id": id, "set": map[string]any{"/spec/deliverables": []any{
		map[string]any{"id": "d1", "name": "Checklist"}}}})
	callTool(t, cs, "settle", map[string]any{"kind": kind, "id": id, "set": map[string]any{"/spec/deliverables/-": map[string]any{"id": "d1", "name": "Grading checklist"}}})
	_, text = callTool(t, cs, "get", map[string]any{"kind": kind, "id": id})
	if strings.Count(text, "id: d1") != 1 || !strings.Contains(text, "Grading checklist") {
		t.Errorf("deliverables: %s", text)
	}
}

// A port's third call writes every record at once, and says what is
// still open on each.
func TestAPortWritesEveryRecordInOneCall(t *testing.T) {
	t.Parallel()
	_, _, cs := setup(t, nil)
	doc := "Depot Checks Charter\n\nA. Purpose\nGraders at every depot apply one checklist, so produce is graded the same everywhere.\n"
	callTool(t, cs, "port", map[string]any{"title": "Depot Checks Charter", "text": doc, "fileSize": len(doc)})
	_, text := callTool(t, cs, "port", map[string]any{"title": "Depot Checks Charter", "pieces": []any{
		map[string]any{"name": "Depot checks", "none": true},
		map[string]any{"name": "Weekly inspection", "ongoing": true},
	}})
	var out struct {
		Records []struct {
			Record string
			Fill   []struct{ Check, Field string }
		}
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil || len(out.Records) != 2 {
		t.Fatalf("records: %s", text)
	}
	project := out.Records[0].Record
	if !strings.HasPrefix(project, "Project/") {
		project = out.Records[1].Record
	}
	_, text = callTool(t, cs, "port", map[string]any{"title": "Depot Checks Charter", "records": []any{
		map[string]any{"record": project, "set": map[string]any{"/spec/objectives/0/objective": "Produce is graded the same at every depot"},
			"open": []any{map[string]any{"check": "resources-funding", "reason": "The charter names no funding"}}},
	}})
	if !strings.Contains(text, `"left":["resources-funding"]`) || !strings.Contains(text, "stillOpen") {
		t.Fatalf("third call: %s", text)
	}
	_, text = callTool(t, cs, "get", map[string]any{"kind": "Project", "id": strings.TrimPrefix(project, "Project/")})
	if !strings.Contains(text, "graded the same at every depot") {
		t.Errorf("objective not written: %s", text)
	}
}

// A register's indicator row gives its KPI a baseline and a target.
func TestARegisterGivesItsKPIsBaselinesAndTargets(t *testing.T) {
	t.Parallel()
	e, _, cs := setup(t, nil)
	if _, err := e.SeedStandardUnits(context.Background()); err != nil {
		t.Fatal(err)
	}
	doc := "Depot Checks Charter\n\nH1. KPI Register\n" +
		"    Indicator                    Baseline              Target / Date             Data Source         Frequency    Owner\n\n" +
		"    Depots grading to checklist  0 (March 2026)        100% by June 2027         Inspection forms    Quarterly    Quality team\n\n" +
		"    Grading disputes             Not counted yet       Set after the pilot       Dispute log         Monthly      Dr. Ada Mensah / Quality team\n"
	callTool(t, cs, "port", map[string]any{"title": "Depot Checks Charter", "text": doc, "fileSize": len(doc)})
	_, ported := callTool(t, cs, "port", map[string]any{"title": "Depot Checks Charter", "pieces": []any{map[string]any{"name": "Depot checks", "none": true}}})
	_, text := callTool(t, cs, "work_summary", map[string]any{})
	if !strings.Contains(text, "KPI/") {
		t.Fatalf("no KPIs: %s\nport: %s", text, ported[strings.Index(ported, `"registers"`):])
	}
	_, text = callTool(t, cs, "propose", map[string]any{"reason": "check"})
	for _, bad := range []string{"kpi-baseline", "missing property 'team'", "periodMonths"} {
		if strings.Contains(text, bad) && !strings.Contains(text, "Grading disputes") {
			t.Errorf("%s still open: %s", bad, text)
		}
	}
}

// A text that is not the whole file (a summary, a part) is refused.
func TestAPortRefusesATextThatIsNotTheWholeFile(t *testing.T) {
	t.Parallel()
	_, _, cs := setup(t, nil)
	res, text := callTool(t, cs, "port", map[string]any{"title": "Depot Checks Charter", "text": "A short summary of the charter.", "fileSize": 40000})
	if !res.IsError || !strings.Contains(text, "never a summary") {
		t.Fatalf("port: %s", text)
	}
	res, _ = callTool(t, cs, "port", map[string]any{"title": "Depot Checks Charter", "text": "A short summary of the charter."})
	if !res.IsError {
		t.Fatal("port took a text without the file's size")
	}
}

// A document brought first and then start_work with the pieces is the
// same port: it goes on in the change set holding the document, and the
// registers are written.
func TestStartWorkAfterBringingADocumentIsItsPort(t *testing.T) {
	t.Parallel()
	_, _, cs := setup(t, nil)
	doc := "Depot Checks Charter\n\nG1. Milestone Plan\n" +
		"    Code    Milestone                    Date          Owner\n\n" +
		"    M1      Checklist agreed             03/03/2026    Quality team\n\n" +
		"    M2      Graders trained              06/05/2026    Quality team\n\n" +
		"    M3      First depot graded           01/06/2026    Inspection unit\n\n" +
		"    M4      Every depot graded           01/09/2026    Inspection unit\n\n" +
		"    M5      Disputes reviewed            01/10/2026    Quality team\n"
	_, brought := callTool(t, cs, "bring_document", map[string]any{"title": "Depot Checks Charter", "text": doc, "fileSize": len(doc)})
	_, text := callTool(t, cs, "start_work", map[string]any{"title": "Depot checks", "pieces": []any{map[string]any{"name": "Depot checks", "none": true}}})
	var a, b struct{ ChangeSet string }
	_ = json.Unmarshal([]byte(brought), &a)
	_ = json.Unmarshal([]byte(text), &b)
	if a.ChangeSet == "" || a.ChangeSet != b.ChangeSet || !strings.Contains(text, `"field":"/spec/milestones"`) {
		t.Fatalf("brought into %s, started %s: %s", a.ChangeSet, b.ChangeSet, text[strings.Index(text, `"registers"`):])
	}
}

// A settle that leaves a check open without saying what was asked keeps
// every field it sets: only the check is not left, and the answer says
// why.
func TestSettleKeepsItsFieldsWhenACheckCannotBeLeft(t *testing.T) {
	t.Parallel()
	_, _, cs := setup(t, nil)
	_, text := callTool(t, cs, "start_work", map[string]any{"title": "Port", "pieces": []any{map[string]any{"name": "Rollout", "none": true}}})
	var out struct{ Work []string }
	_ = json.Unmarshal([]byte(text), &out)
	kind, id, _ := strings.Cut(out.Work[0], "/")
	res, text := callTool(t, cs, "settle", map[string]any{"kind": kind, "id": id,
		"set":  map[string]any{"/spec/objectives/0/objective": "Produce is graded the same at every depot"},
		"open": []any{map[string]any{"check": "resources-funding", "reason": "No funding is named"}}})
	if res.IsError || !strings.Contains(text, "notLeft") {
		t.Fatalf("settle: %s", text)
	}
	_, text = callTool(t, cs, "get", map[string]any{"kind": kind, "id": id})
	if !strings.Contains(text, "graded the same at every depot") {
		t.Errorf("objective lost: %s", text)
	}
}

// Where the document has a deliverable register, a component project
// names the row that hands it over; a piece with no row is refused.
func TestAComponentNamesItsDeliverableRow(t *testing.T) {
	t.Parallel()
	_, _, cs := setup(t, nil)
	doc := "Depot Checks Charter\n\nD1. Deliverable Register\n" +
		"    Code    Deliverable                  Due           Owner\n\n" +
		"    D1      Grading checklist            03/03/2026    Quality team\n\n" +
		"    D2      Baseline survey report       06/05/2026    Quality team\n\n" +
		"    D3      Inspection forms             01/06/2026    Inspection unit\n\n" +
		"    D4      Dispute log                  01/09/2026    Inspection unit\n"
	callTool(t, cs, "port", map[string]any{"title": "Depot Checks Charter", "text": doc, "fileSize": len(doc)})
	_, text := callTool(t, cs, "port", map[string]any{"title": "Depot Checks Charter", "pieces": []any{
		map[string]any{"name": "Depot checks", "none": true},
		map[string]any{"name": "Baseline survey", "changeOfItsOwn": true, "change": "sets the baseline", "dependedOnBy": []any{"Depot checks"}, "deliverable": "D2"},
		map[string]any{"name": "Readiness phase", "changeOfItsOwn": true, "change": "depots are ready", "dependedOnBy": []any{"Depot checks"}},
	}})
	if !strings.Contains(text, `\"Readiness phase\" has a change of its own: give deliverable`) || strings.Contains(text, `\"Baseline survey\" has a change`) {
		t.Fatalf("port: %s", text)
	}
}

// A change set holding a document is a port: a one-at-a-time write in
// it is refused and sent to port with records.
func TestAPortHasOneWritePath(t *testing.T) {
	t.Parallel()
	_, _, cs := setup(t, nil)
	doc := "Depot Checks Charter\n\nA. Purpose\nGraders at every depot apply one checklist, so produce is graded the same everywhere.\n"
	callTool(t, cs, "port", map[string]any{"title": "Depot Checks Charter", "text": doc, "fileSize": len(doc)})
	_, text := callTool(t, cs, "port", map[string]any{"title": "Depot Checks Charter", "pieces": []any{map[string]any{"name": "Depot checks", "none": true}}})
	var out struct{ Work []string }
	_ = json.Unmarshal([]byte(text), &out)
	kind, id, _ := strings.Cut(out.Work[0], "/")
	res, text := callTool(t, cs, "edit_draft", map[string]any{"kind": kind, "id": id, "set": map[string]any{"/spec/summary/about": "x"}})
	if !res.IsError || !strings.Contains(text, "Call port with records now") {
		t.Fatalf("edit_draft in a port: %s", text)
	}
}

// The register chain applies the porting map, whoever runs it: a
// milestone already completed is not ported unless one to come waits on
// it, a measure taken once is not an indicator, a form after a register
// is not more of it, and a body named on several rows is one record.
func TestARegisterIsPortedAsThePortingMapSays(t *testing.T) {
	t.Parallel()
	e, _, cs := setup(t, nil)
	if _, err := e.SeedStandardUnits(context.Background()); err != nil {
		t.Fatal(err)
	}
	doc := "Depot Checks Charter\n\nG1. Milestone Plan\n" +
		"    No.     Milestone                    Owner            Dependency     Completion Evidence\n\n" +
		"    M1      Checklist agreed             Quality team                    Completed - minutes on file\n\n" +
		"    M2      Pilot graded                 Quality team                    Completed - pilot forms\n\n" +
		"    M3      Graders trained              Quality team     M2             Planned\n\n" +
		"    M4      Every depot graded           Inspection unit  M3             Planned\n\n" +
		"H1. KPI Register\n" +
		"    Indicator                    Baseline              Target / Date             Data Source         Frequency    Owner\n\n" +
		"    Depots grading to checklist  0 (March 2026)        100% by June 2027         Inspection forms    Quarterly    Quality team\n\n" +
		"    Pilot depots surveyed        0 (March 2026)        100% by May 2026          Pilot memo          Once         Quality team\n\n" +
		"    Field                        Requirement           Project Response\n\n" +
		"    Reporting Frequency          Mandatory             Monthly report to the board on every depot graded\n"
	callTool(t, cs, "port", map[string]any{"title": "Depot Checks Charter", "text": doc, "fileSize": len(doc)})
	_, text := callTool(t, cs, "port", map[string]any{"title": "Depot Checks Charter", "pieces": []any{map[string]any{"name": "Depot checks", "none": true}}})
	var out struct {
		Registers []struct {
			Field     string
			Added     int
			NotPorted []string
		}
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	skipped := map[string]int{}
	for _, r := range out.Registers {
		got[r.Field], skipped[r.Field] = r.Added, len(r.NotPorted)
	}
	// M1 is completed and nothing to come waits on it; M2 is completed
	// but M3 waits on it, so it stays.
	if got["/spec/milestones"] != 3 || skipped["/spec/milestones"] != 1 || got["/spec/kpis"] != 1 || skipped["/spec/kpis"] != 1 {
		t.Fatalf("registers %+v: %s", out.Registers, text)
	}
	_, text = callTool(t, cs, "work_summary", map[string]any{})
	if n := strings.Count(text, `"name":"Quality team","openChecks":0,"record":"Resource/`); n != 1 {
		t.Errorf("Quality team drafted as a role %d times: %s", n, text)
	}
	if strings.Contains(text, `"name":"Field"`) || strings.Contains(text, "Reporting Frequency") {
		t.Errorf("the form after the register was read as rows: %s", text)
	}
}

// Every record needed is read in one call.
func TestGetReadsManyRecordsInOneCall(t *testing.T) {
	t.Parallel()
	_, _, cs := setup(t, nil)
	_, text := callTool(t, cs, "start_work", map[string]any{"title": "Port", "pieces": []any{
		map[string]any{"name": "Rollout", "none": true}, map[string]any{"name": "Weekly checks", "ongoing": true}}})
	var out struct{ Work []string }
	_ = json.Unmarshal([]byte(text), &out)
	_, text = callTool(t, cs, "get", map[string]any{"records": out.Work})
	if strings.Count(text, `"yaml"`) != len(out.Work) || len(out.Work) != 2 {
		t.Fatalf("get: %s", text)
	}
}
