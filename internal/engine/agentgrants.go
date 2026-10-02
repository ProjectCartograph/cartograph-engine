package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/identity"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

// An agent grant is a person's consent to one agent acting for them
// (docs/adr/0016). The engine keeps the rules: only a signed-in person
// whose agents are enabled grants one, never an agent; a grant ends when
// it is revoked, expires, or its person leaves the access list; its
// person or an administrator revokes it. Whatever carries a grant on the
// wire (an OAuth token, say) is an adapter's, and the engine never sees
// it.

// Bounds on a grant's life.
const (
	defaultAgentGrantLife = 90 * 24 * time.Hour
	maxAgentGrantLife     = 365 * 24 * time.Hour
	agentGrantTouchEvery  = 10 * time.Minute
)

var (
	// ErrAgentGrantsOff is a deployment that keeps no access list, so no
	// grants.
	ErrAgentGrantsOff = fmt.Errorf("%w: agent grants need an access list (CARTOGRAPH_AUTHZ=access)", ErrConflict)
	// ErrAgentGrantEnded is a grant that is unknown, revoked, expired, or
	// for someone no longer on the access list.
	ErrAgentGrantEnded = errors.New("the agent grant has ended")
	// ErrAgentGrantReused is a code or refresh token presented twice; the
	// grant is revoked, since one of the two was not its agent's.
	ErrAgentGrantReused = errors.New("the agent grant was used twice and is revoked")
)

// GrantAgent records the signed-in person's consent to the agent they
// label acting for them, for ttl (90 days when 0, at most a year).
func (e *Engine) GrantAgent(ctx context.Context, label string, ttl time.Duration) (store.AgentGrant, error) {
	if err := refuseAgent(ctx); err != nil {
		return store.AgentGrant{}, err
	}
	if e.access == nil {
		return store.AgentGrant{}, ErrAgentGrantsOff
	}
	who := identity.PrincipalFrom(ctx)
	email := emailOf(who)
	if who.Anonymous || email == "" {
		return store.AgentGrant{}, fmt.Errorf("%w: sign in to let an agent act for you", identity.ErrForbidden)
	}
	g, err := e.Grants(ctx, who)
	if err != nil {
		return store.AgentGrant{}, err
	}
	if !g.Agents {
		return store.AgentGrant{}, fmt.Errorf("%w: agents are not enabled for you", identity.ErrForbidden)
	}
	label = strings.TrimSpace(label)
	if label == "" || len(label) > 64 {
		return store.AgentGrant{}, &ValidationError{Problems: []Problem{{Path: "/label", Message: "Name the agent, in up to 64 characters."}}}
	}
	if ttl <= 0 {
		ttl = defaultAgentGrantLife
	}
	now := timeNow().UTC()
	grant := store.AgentGrant{ID: newProposalID(), Email: email, Label: label, CreatedAt: now, ExpiresAt: now.Add(min(ttl, maxAgentGrantLife))}
	if err := e.access.PutAgentGrant(ctx, grant); err != nil {
		return store.AgentGrant{}, err
	}
	return grant, nil
}

// AgentGrantPrincipal is who a grant acts for: its person, with the
// agent beside them, as the access list has them now. Whether that
// person may still use an agent is the access policy's question, asked
// on every request.
func (e *Engine) AgentGrantPrincipal(ctx context.Context, id string) (identity.Principal, store.AgentGrant, error) {
	if e.access == nil {
		return identity.Principal{}, store.AgentGrant{}, ErrAgentGrantEnded
	}
	grant, err := e.access.GetAgentGrant(ctx, id)
	if err != nil {
		return identity.Principal{}, store.AgentGrant{}, ErrAgentGrantEnded
	}
	now := timeNow().UTC()
	if !grant.RevokedAt.IsZero() || !now.Before(grant.ExpiresAt) {
		return identity.Principal{}, store.AgentGrant{}, ErrAgentGrantEnded
	}
	person, err := e.access.GetPerson(ctx, grant.Email)
	if err != nil {
		return identity.Principal{}, store.AgentGrant{}, ErrAgentGrantEnded
	}
	if now.Sub(grant.LastUsed) > agentGrantTouchEvery {
		_ = e.access.TouchAgentGrant(ctx, grant.ID, now)
	}
	subject := person.Subject
	if subject == "" {
		subject = person.Email
	}
	return identity.Principal{Subject: subject, Email: person.Email, Name: person.Name, Agent: grant.Label, Delegated: true}, grant, nil
}

// AdvanceAgentGrant moves a live grant from generation gen to the next,
// once: a code or refresh token is exchanged for the next. Presented
// twice, the grant is revoked (ErrAgentGrantReused).
func (e *Engine) AdvanceAgentGrant(ctx context.Context, id string, gen int) (int, error) {
	if _, _, err := e.AgentGrantPrincipal(ctx, id); err != nil {
		return 0, err
	}
	ok, err := e.access.RotateAgentGrant(ctx, id, gen)
	if err != nil {
		return 0, err
	}
	if !ok {
		_ = e.access.RevokeAgentGrant(ctx, id, timeNow().UTC())
		return 0, ErrAgentGrantReused
	}
	return gen + 1, nil
}

// AgentGrants lists grants: the caller's own, or, for an administrator,
// someone's (email) or everyone's ("*").
func (e *Engine) AgentGrants(ctx context.Context, email string) ([]store.AgentGrant, error) {
	if e.access == nil {
		return []store.AgentGrant{}, nil
	}
	self, admin, err := e.grantCaller(ctx)
	if err != nil {
		return nil, err
	}
	email = strings.ToLower(strings.TrimSpace(email))
	switch {
	case email == "" || email == self:
		return e.access.ListAgentGrants(ctx, self)
	case !admin:
		return nil, fmt.Errorf("%w: only an administrator reads someone else's agents", identity.ErrForbidden)
	case email == "*":
		return e.access.ListAgentGrants(ctx, "")
	}
	return e.access.ListAgentGrants(ctx, email)
}

// RevokeAgentGrant ends a grant at once: the caller's own, or anyone's
// for an administrator.
func (e *Engine) RevokeAgentGrant(ctx context.Context, id string) error {
	if e.access == nil {
		return fmt.Errorf("%w: agent grant %s", ErrNotFound, id)
	}
	self, admin, err := e.grantCaller(ctx)
	if err != nil {
		return err
	}
	grant, err := e.access.GetAgentGrant(ctx, id)
	if errors.Is(err, store.ErrNoAgentGrant) {
		return fmt.Errorf("%w: agent grant %s", ErrNotFound, id)
	}
	if err != nil {
		return err
	}
	if grant.Email != self && !admin {
		return fmt.Errorf("%w: only its person or an administrator revokes an agent", identity.ErrForbidden)
	}
	return e.access.RevokeAgentGrant(ctx, id, timeNow().UTC())
}

// grantCaller is the person managing grants, never an agent, and whether
// they are an administrator.
func (e *Engine) grantCaller(ctx context.Context) (self string, admin bool, err error) {
	if err := refuseAgent(ctx); err != nil {
		return "", false, err
	}
	who := identity.PrincipalFrom(ctx)
	self = emailOf(who)
	if who.Anonymous || self == "" {
		return "", false, fmt.Errorf("%w: sign in to manage agents", identity.ErrForbidden)
	}
	g, err := e.Grants(ctx, who)
	if err != nil {
		return "", false, err
	}
	return self, slices.Contains(g.Roles, identity.RoleAdministrator), nil
}
