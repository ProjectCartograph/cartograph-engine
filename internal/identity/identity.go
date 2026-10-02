// Package identity is who a request runs as and what they ask to do,
// without HTTP: the part of the identity port (docs/ARCHITECTURE.md) the
// engine may know. internal/auth adds the HTTP side (authenticators, the
// middleware, classifying a request) and re-exports everything here, so
// adapters and the API keep using auth's names.
package identity

import (
	"context"
	"errors"
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
}

// Anonymous is the principal of a request nobody identified.
var Anonymous = Principal{Subject: "", Anonymous: true}

// Actor is what a write records: the subject, or fallback for an
// anonymous principal (a vault's spec.operator, typically).
func (p Principal) Actor(fallback string) string {
	if p.Anonymous || p.Subject == "" {
		return fallback
	}
	return p.Subject
}

// ErrForbidden is returned by an Authorizer that refuses an action.
var ErrForbidden = errors.New("forbidden")

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
