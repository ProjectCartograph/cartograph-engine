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
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/identity"
	"net/http"
)

// The identity types live in internal/identity, which the engine may
// import; auth re-exports them so the HTTP side keeps one vocabulary.
type (
	Principal  = identity.Principal
	Action     = identity.Action
	Change     = identity.Change
	Grants     = identity.Grants
	Scope      = identity.Scope
	Scoper     = identity.Scoper
	Authorizer = identity.Authorizer
)

// Anonymous is the principal of a request nobody identified.
var Anonymous = identity.Anonymous

// ErrForbidden is returned by an Authorizer that refuses an action.
var ErrForbidden = identity.ErrForbidden

// ErrAgentProposes refuses an agent what its person must confirm.
var ErrAgentProposes = identity.ErrAgentProposes

// Verbs, resources, roles and scopes, as identity names them.
const (
	VerbRead           = identity.VerbRead
	VerbWrite          = identity.VerbWrite
	ResourceAccess     = identity.ResourceAccess
	ResourceVault      = identity.ResourceVault
	ResourceSession    = identity.ResourceSession
	ResourceAgent      = identity.ResourceAgent
	RoleReader         = identity.RoleReader
	RoleContributor    = identity.RoleContributor
	RoleStrategyEditor = identity.RoleStrategyEditor
	RoleAdministrator  = identity.RoleAdministrator
	ScopeNone          = identity.ScopeNone
	ScopeTeams         = identity.ScopeTeams
	ScopeAll           = identity.ScopeAll
)

// Roles is every role, from the least to the most a role may do.
var Roles = identity.Roles

// WithPrincipal returns a context carrying p.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return identity.WithPrincipal(ctx, p)
}

// PrincipalFrom returns the principal on ctx, or Anonymous.
func PrincipalFrom(ctx context.Context) Principal { return identity.PrincipalFrom(ctx) }

// ErrUnauthenticated is returned by an Authenticator that requires a
// credential and found none, or a bad one.
var ErrUnauthenticated = errors.New("unauthenticated")

// Authenticator turns a request into a Principal. Return Anonymous for a
// request that carries no identity when that is allowed; return
// ErrUnauthenticated to refuse it.
type Authenticator interface {
	Authenticate(r *http.Request) (Principal, error)
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
