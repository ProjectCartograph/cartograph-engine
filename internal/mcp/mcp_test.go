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
	mu   sync.Mutex
	seen []string
}

func (p *presence) AnnounceAgent(_ context.Context, docID, actor, name string) {
	p.mu.Lock()
	p.seen = append(p.seen, actor+" | "+name)
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
		"spec": map[string]any{"name": "Team One", "description": "Drafted by an agent"}}
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
