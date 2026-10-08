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
	h := mcp.Handler(mcp.Options{Engine: e, Authz: authz, Presence: pr, Version: "test"})
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
		if !readOnly && tl.Name != "save_draft" && tl.Name != "save_drafts" && tl.Name != "edit_draft" && tl.Name != "discard_draft" && tl.Name != "leave_open" && tl.Name != "start_work" && tl.Name != "propose" && !strings.HasPrefix(tl.Name, "propose_") {
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
	if res.IsError || !strings.Contains(text, `"then"`) || !strings.Contains(text, `"define"`) {
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
	res, text := callTool(t, cs, "taxonomy", map[string]any{})
	if res.IsError {
		t.Fatal(text)
	}
	for _, want := range []string{`"porting":[`, "A workstream or strand whose result is an output", "Not ported", "governanceBody", "A measure taken once"} {
		if !strings.Contains(text, want) {
			t.Errorf("the porting map lacks %q", want)
		}
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
		map[string]any{"name": "Rollout"},
		map[string]any{"name": "Handbook", "outputOf": "Rollout"},
		map[string]any{"name": "Survey", "changeOfItsOwn": true, "dependedOnBy": []any{"Rollout"}},
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
		map[string]any{"name": "Rollout"}, map[string]any{"name": "Handbook", "outputOf": "Rollout"},
		map[string]any{"name": "Survey", "changeOfItsOwn": true, "dependedOnBy": []any{"Rollout"}},
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
