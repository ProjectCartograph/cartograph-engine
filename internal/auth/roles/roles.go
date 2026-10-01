// Package roles is a role-based Authorizer: principals in a write role may
// read and write, principals in a read role may read, everyone else is
// refused. Roles come from the Authenticator (the proxy's groups header,
// an OIDC token's claims), so this adapter stores nothing.
//
// It is deliberately coarse. A policy per kind, or per manifest, is a
// second adapter over the same Action; the enforcement point does not
// change.
package roles

import (
	"context"
	"fmt"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/internal/auth"
)

// Authorizer holds the two role sets.
type Authorizer struct {
	read  map[string]bool
	write map[string]bool
}

var _ auth.Authorizer = (*Authorizer)(nil)

// New builds the policy. An empty read set means any authenticated
// principal may read; an empty write set means nobody may write.
func New(readRoles, writeRoles []string) *Authorizer {
	return &Authorizer{read: set(readRoles), write: set(writeRoles)}
}

// Parse builds the policy from two comma-separated lists, the shape the
// environment carries.
func Parse(readRoles, writeRoles string) *Authorizer {
	return New(strings.Split(readRoles, ","), strings.Split(writeRoles, ","))
}

func set(roles []string) map[string]bool {
	out := map[string]bool{}
	for _, r := range roles {
		if r = strings.TrimSpace(r); r != "" {
			out[r] = true
		}
	}
	return out
}

// Authorize refuses an anonymous principal, then applies the two sets.
func (a *Authorizer) Authorize(_ context.Context, p auth.Principal, act auth.Action) error {
	if p.Anonymous {
		return fmt.Errorf("%w: sign in to %s", auth.ErrForbidden, act.Verb)
	}
	canWrite := a.has(p, a.write)
	switch act.Verb {
	case auth.VerbRead:
		if canWrite || len(a.read) == 0 || a.has(p, a.read) {
			return nil
		}
	default:
		if canWrite {
			return nil
		}
	}
	what := act.Verb
	if act.Kind != "" {
		what += " " + act.Kind
	}
	return fmt.Errorf("%w: %s may not %s", auth.ErrForbidden, p.Actor("anonymous"), what)
}

func (a *Authorizer) has(p auth.Principal, roles map[string]bool) bool {
	for _, r := range p.Roles {
		if roles[r] {
			return true
		}
	}
	return false
}
