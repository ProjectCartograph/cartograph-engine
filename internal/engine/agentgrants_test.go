package engine_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/auth"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/auth/access"
	codecyaml "github.com/ProjectCartograph/cartograph-engine/v2/internal/codec/yaml"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store/memory"
)

func grantsEngine(t *testing.T) *engine.Engine {
	t.Helper()
	d := directory
	d.Agents = []string{auth.RoleContributor}
	var e *engine.Engine
	policy := access.Policy{Grants: func(ctx context.Context, p auth.Principal) (auth.Grants, error) { return e.Grants(ctx, p) }}
	e, err := engine.New(memory.NewManifestStore(), memory.NewOperationalStore(), engine.WithCodec(codecyaml.New()),
		engine.WithAuthorizer(policy), engine.WithAccess(memory.NewAccessStore(), d))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.ApplyDirectory(context.Background(), "directory"); err != nil {
		t.Fatal(err)
	}
	return e
}

// A person lets an agent act for them; the grant names them, with the
// agent beside them, until it is revoked, and reusing one of its codes
// revokes it.
func TestAgentGrants(t *testing.T) {
	e := grantsEngine(t)
	lee, ada, noor := as("lee@example.org", "g-early"), as("ada@example.org", "g-admins"), as("noor@example.org")
	if _, err := e.GrantAgent(noor, "Claude", 0); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("someone whose agents are not enabled: %v", err)
	}
	if _, err := e.GrantAgent(auth.WithPrincipal(context.Background(), auth.Principal{Subject: "s", Email: "lee@example.org", Agent: "Claude"}), "Other", 0); !errors.Is(err, auth.ErrAgentProposes) {
		t.Fatalf("an agent granting itself: %v", err)
	}
	g, err := e.GrantAgent(lee, "Claude", 0)
	if err != nil {
		t.Fatal(err)
	}
	p, _, err := e.AgentGrantPrincipal(context.Background(), g.ID)
	if err != nil || p.Email != "lee@example.org" || p.Agent != "Claude" || !p.Delegated {
		t.Fatalf("who the grant acts for: %+v, %v", p, err)
	}
	// No groups come with the agent: Lee's roles are those of their last
	// sign-in, and asking does not overwrite them.
	for range 2 {
		if g, err := e.Grants(context.Background(), p); err != nil || !g.Agents || len(g.Teams) == 0 {
			t.Fatalf("the agent's grants: %+v, %v", g, err)
		}
	}
	if next, err := e.AdvanceAgentGrant(context.Background(), g.ID, 0); err != nil || next != 1 {
		t.Fatalf("exchanging the code: %d, %v", next, err)
	}
	if mine, _ := e.AgentGrants(lee, ""); len(mine) != 1 {
		t.Fatalf("Lee's agents: %+v", mine)
	}
	if _, err := e.AgentGrants(lee, "ada@example.org"); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("Lee reading Ada's agents: %v", err)
	}
	if all, err := e.AgentGrants(ada, "*"); err != nil || len(all) != 1 {
		t.Fatalf("an administrator reading every agent: %+v, %v", all, err)
	}

	// The code exchanged again: one of the two was not Lee's agent.
	if _, err := e.AdvanceAgentGrant(context.Background(), g.ID, 0); !errors.Is(err, engine.ErrAgentGrantReused) {
		t.Fatalf("a code used twice: %v", err)
	}
	if _, _, err := e.AgentGrantPrincipal(context.Background(), g.ID); !errors.Is(err, engine.ErrAgentGrantEnded) {
		t.Fatalf("after reuse: %v", err)
	}

	g2, _ := e.GrantAgent(lee, "Claude", 0)
	if err := e.RevokeAgentGrant(noor, g2.ID); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("someone else revoking: %v", err)
	}
	if err := e.RevokeAgentGrant(ada, g2.ID); err != nil {
		t.Fatalf("an administrator revoking: %v", err)
	}
	if _, _, err := e.AgentGrantPrincipal(context.Background(), g2.ID); !errors.Is(err, engine.ErrAgentGrantEnded) {
		t.Fatalf("after revoking: %v", err)
	}
}
