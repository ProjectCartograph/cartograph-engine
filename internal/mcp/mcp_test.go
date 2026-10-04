package mcp_test

import (
	"context"
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
	e, pr, cs := setup(t, nil)
	ctx := context.Background()

	tools, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tl := range tools.Tools {
		readOnly := tl.Annotations != nil && tl.Annotations.ReadOnlyHint
		if !readOnly && tl.Name != "save_draft" && tl.Name != "edit_draft" && tl.Name != "start_work" && tl.Name != "propose" && !strings.HasPrefix(tl.Name, "propose_") {
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
	_, _, cs := setup(t, refuseAgents{})
	if res, text := callTool(t, cs, "get", map[string]any{"kind": "Team", "id": "t1"}); !res.IsError || !strings.Contains(text, "forbidden") {
		t.Fatalf("an agent not allowed read: %v %s", res.IsError, text)
	}
}

// Every kind with a flow has a prompt to guide a person through it.
func TestGuidePrompts(t *testing.T) {
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
	_, _, cs := setup(t, nil)
	gap := map[string]any{"apiVersion": "cartograph/v1", "kind": "Gap", "metadata": map[string]any{"id": "gap-bruising", "name": "Bruised on arrival"},
		"spec": map[string]any{"current": "One crate in five arrives bruised", "desired": "Fewer than one in fifty", "outcomes": []any{"o-sound"}}}
	outcome := map[string]any{"apiVersion": "cartograph/v1", "kind": "Goal", "metadata": map[string]any{"id": "o-sound", "name": "Fruit arrives sound"},
		"spec": map[string]any{"level": "outcome", "objective": "Fruit arrives sound at every depot"}}
	if res, text := callTool(t, cs, "save_draft", map[string]any{"kind": "Gap", "id": "gap-bruising", "manifest": gap}); res.IsError {
		t.Fatalf("save_draft gap: %s", text)
	}
	res, text := callTool(t, cs, "save_draft", map[string]any{"kind": "Goal", "id": "o-sound", "manifest": outcome, "work": []string{"Gap/gap-bruising"}})
	if res.IsError {
		t.Fatalf("save_draft outcome: %s", text)
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
	_, _, cs := setup(t, nil)
	res, text := callTool(t, cs, "taxonomy", map[string]any{})
	if res.IsError || !strings.Contains(text, `"Gap"`) || !strings.Contains(text, "problem statement") || !strings.Contains(text, "never by the word") {
		t.Fatalf("taxonomy: %.600s", text)
	}
}
