// Package identity is who a request runs as and what they ask to do,
// without HTTP: the part of the identity port (docs/ARCHITECTURE.md) the
// engine may know. internal/auth adds the HTTP side (authenticators, the
// middleware, classifying a request) and re-exports everything here, so
// adapters and the API keep using auth's names.
package identity

import (
	"context"
	"errors"
	"fmt"
)

// Principal is who a request runs as.
type Principal struct {
	// Subject is the stable identifier, what a version's Actor records.
	Subject string
	// Name is for display. Falls back to Subject.
	Name string
	// Email is the organisation address, when the authenticator has
	// one: what an access list knows a person by.
	Email string
	// Roles are the groups the principal belongs to, for an Authorizer.
	Roles []string
	// Anonymous is true when no authenticator identified anyone.
	Anonymous bool
	// Agent names the agent acting for this person, through MCP
	// (docs/adr/0016); empty when the person acts themself. An agent
	// holds its person's authority and no more, and may read and propose
	// but not make the record: that waits for the person.
	Agent string
	// Delegated is a principal an agent grant carries, not a sign-in: no
	// directory groups came with the request, so the person's roles and
	// teams are the ones recorded at their last sign-in.
	Delegated bool
}

// Person is the principal without its agent: the person it acts for.
func (p Principal) Person() Principal {
	p.Agent = ""
	return p
}

// Anonymous is the principal of a request nobody identified.
var Anonymous = Principal{Subject: "", Anonymous: true}

// Actor is what a write records: the subject, or fallback for an
// anonymous principal (a vault's spec.operator, typically).
func (p Principal) Actor(fallback string) string {
	who := p.Subject
	if p.Anonymous || who == "" {
		who = fallback
	}
	if p.Agent != "" {
		return who + " via " + p.Agent
	}
	return who
}

// ErrForbidden is returned by an Authorizer that refuses an action.
var ErrForbidden = errors.New("forbidden")

// ErrAgentProposes refuses an agent something that makes the record: a
// version, a reading, a state change, a deletion. It may propose it, and
// its person confirms (docs/adr/0016).
var ErrAgentProposes = fmt.Errorf("%w: an agent proposes this, and its person confirms it", ErrForbidden)

// Verbs an Action carries. Read is any safe method; Write is the rest.
const (
	VerbRead  = "read"
	VerbWrite = "write"
)

// Action is one thing a principal asks to do.
type Action struct {
	// Verb is read or write (the HTTP method's intent).
	Verb string
	// Kind and ID name the manifest, when the action concerns one.
	Kind string
	ID   string
	// Resource names what the action concerns when it is not a manifest:
	// ResourceAccess, ResourceVault or ResourceSession.
	Resource string
	// Change is what a write would do to the manifest's content. The HTTP
	// layer cannot see it and leaves it nil; the engine sets it at every
	// write, so a policy that decides by content decides there.
	Change *Change
}

// Resources an action may concern besides a manifest.
const (
	ResourceAccess  = "access"  // the access list
	ResourceVault   = "vault"   // applying and recovering the vault
	ResourceSession = "session" // who the caller is
	ResourceAgent   = "agent"   // acting through an agent at all
)

// Change is what one write does to one manifest.
type Change struct {
	// TeamsBefore and TeamsAfter are the chains of teams the manifest
	// sits under, nearest first, as stored and as it would be after the
	// write. A new manifest has no chain before; a deletion none after;
	// a manifest that names no team has none on that side.
	TeamsBefore, TeamsAfter []string
	// Fields are the top-level spec fields whose values change, sorted.
	Fields []string
}

// The roles an access list grants (docs/adr/0011, TAXONOMY.md D27).
const (
	RoleReader         = "reader"
	RoleContributor    = "contributor"
	RoleStrategyEditor = "strategyEditor"
	RoleAdministrator  = "administrator"
)

// Roles is every role, from the least to the most a role may do.
var Roles = []string{RoleReader, RoleContributor, RoleStrategyEditor, RoleAdministrator}

// Grants are what an access list gives a principal: whether they are
// listed at all, and the roles and teams they hold, from an
// administrator and from the directory together.
type Grants struct {
	Listed bool
	Roles  []string
	// Teams are the teams the principal acts for, without the teams
	// beneath them.
	Teams []string
	// Agents is whether the principal may act through an agent: one of
	// their roles is allowed agents, and an administrator has not turned
	// theirs off (docs/adr/0016).
	Agents bool
}

// Scope is how much of a kind a principal may write.
type Scope string

// Scopes, from none to all.
const (
	ScopeNone  Scope = "none"  // nothing of the kind
	ScopeTeams Scope = "teams" // what their teams and the teams beneath them own
	ScopeAll   Scope = "all"   // all of it
)

// Scoper is an Authorizer that can say ahead of any write how much of a
// kind a principal may write, so an interface offers only that. It is a
// hint: the write itself is decided again.
type Scoper interface {
	Scope(ctx context.Context, p Principal, kind string) (Scope, error)
}

// Authorizer decides whether a principal may perform an action.
type Authorizer interface {
	Authorize(ctx context.Context, p Principal, a Action) error
}

type principalKey struct{}

// WithPrincipal returns a context carrying p.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// PrincipalFrom returns the principal on ctx, or Anonymous.
func PrincipalFrom(ctx context.Context) Principal {
	if p, ok := ctx.Value(principalKey{}).(Principal); ok {
		return p
	}
	return Anonymous
}
