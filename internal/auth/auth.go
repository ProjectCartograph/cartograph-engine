// Package auth is the identity port: who is making this request, and may
// they do what they are asking. The engine never sees a credential; it
// sees a Principal, which the HTTP layer puts on the context after an
// Authenticator has produced one. Authorization is a second port, so a
// deployment can plug in a policy without touching authentication.
//
// The shape follows Kubernetes: an authenticator chain turns a request
// into a subject with groups (here Roles), and an authorizer decides on
// (subject, verb, resource). Cartograph ships the no-identity adapters an
// unauthenticated single-operator deployment needs; a deployment that
// puts Cartograph behind an authenticating proxy, or wires an OIDC provider,
// adds an Authenticator and keeps everything else.
package auth

import (
	"context"
	"errors"
	"net/http"
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

// ErrUnauthenticated is returned by an Authenticator that requires a
// credential and found none, or a bad one.
var ErrUnauthenticated = errors.New("unauthenticated")

// ErrForbidden is returned by an Authorizer that refuses an action.
var ErrForbidden = errors.New("forbidden")

// Authenticator turns a request into a Principal. Return Anonymous for a
// request that carries no identity when that is allowed; return
// ErrUnauthenticated to refuse it.
type Authenticator interface {
	Authenticate(r *http.Request) (Principal, error)
}

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

// NoAuthentication identifies nobody: every request is Anonymous. The
// default for a single-operator deployment on a trusted network.
type NoAuthentication struct{}

// Authenticate returns Anonymous.
func (NoAuthentication) Authenticate(*http.Request) (Principal, error) { return Anonymous, nil }

// AllowAll permits every action. The default authorizer.
type AllowAll struct{}

// Authorize returns nil.
func (AllowAll) Authorize(context.Context, Principal, Action) error { return nil }

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

// Middleware runs a on every request and puts the principal on the
// context. A request the authenticator refuses gets 401 and never reaches
// the handler.
func Middleware(a Authenticator, next http.Handler) http.Handler {
	if a == nil {
		a = NoAuthentication{}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, err := a.Authenticate(r)
		if err != nil {
			http.Error(w, `{"problems":[{"message":"unauthenticated"}]}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), p)))
	})
}
