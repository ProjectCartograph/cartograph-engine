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

func (p *presence) KeepAgent(ctx context.Context, docID, actor, name string, agent map[string]any) {
	p.AnnounceAgent(ctx, docID, actor, name, "", agent)
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

// An agent reads, drafts in sight of everyone on the manifest, and
// proposes; nothing it does makes the record.
func TestAnAgentReadsDraftsAndProposes(t *testing.T) {
	e, pr, cs := setup(t, nil)
	ctx := context.Background()

	tools, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tl := range tools.Tools {
		readOnly := tl.Annotations != nil && tl.Annotations.ReadOnlyHint
		if !readOnly && tl.Name != "save_draft" && !strings.HasPrefix(tl.Name, "propose_") {
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
	w, found, _ := e.GetWorking(ctx, "Team", "t1")
	if !found || !strings.Contains(string(w), "Drafted by an agent") {
		t.Fatalf("the draft is %q", w)
	}
	if len(pr.seen) == 0 || pr.seen[0] != "ada@example.org via Claude | Ada's agent (Claude)" {
		t.Fatalf("announced as %v", pr.seen)
	}
	// The draft is announced on the team's document, at the field it
	// changed, and on Ada's own feed, for her to follow; never on the
	// presence document everyone joins.
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
	}
	feed, _ := e.Shared().AgentFeed(ctx, "ada@example.org")
	teamDoc, _ := e.Shared().DocumentFor(ctx, "Team", "t1")
	if !docs[feed] || !docs[teamDoc] || len(docs) != 2 {
		t.Fatalf("the draft was announced on %v, want the team's and Ada's feed", docs)
	}

	res, text := callTool(t, cs, "propose_save", map[string]any{"kind": "Team", "id": "t1", "reason": "describe the team"})
	if res.IsError {
		t.Fatalf("propose_save: %s", text)
	}
	if vs, _ := e.Versions(ctx, "Team", "t1"); len(vs) != 1 {
		t.Fatalf("a proposal saved a version: %d versions", len(vs))
	}
	open, _ := e.Proposals(identity.WithPrincipal(ctx, ada), store.ProposalFilter{Status: store.ProposalOpen})
	if len(open) != 1 || open[0].Agent != "Claude" || open[0].For != "ada@example.org" || !strings.Contains(string(open[0].Text), "Drafted by an agent") {
		t.Fatalf("Ada's open proposals: %+v", open)
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
