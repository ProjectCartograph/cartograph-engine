// Package proxy is the Authenticator for a deployment behind an
// authenticating reverse proxy (oauth2-proxy, Pomerium, Authelia, an
// ingress with OIDC): the proxy verifies the person and forwards their
// identity in a header, and this adapter trusts that header.
//
// Trusting a header is only safe when the proxy is the only way to reach
// the server, which is a deployment property, not a code one. The
// deployment guide says so in the same breath as it enables this.
package proxy

import (
	"net/http"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/auth"
)

// Authenticator reads the identity from Header and, when the proxy
// forwards them, the display name, the email address and the groups
// (comma-separated) from their own headers.
type Authenticator struct {
	Header       string
	NameHeader   string
	EmailHeader  string
	GroupsHeader string
}

var _ auth.Authenticator = (*Authenticator)(nil)

// New returns an Authenticator reading the subject from header, with the
// conventional companions X-Forwarded-Preferred-Username,
// X-Forwarded-Email and X-Forwarded-Groups, the headers oauth2-proxy
// sets, for the display name, the address and the groups.
func New(header string) *Authenticator {
	return &Authenticator{
		Header:       header,
		NameHeader:   "X-Forwarded-Preferred-Username",
		EmailHeader:  "X-Forwarded-Email",
		GroupsHeader: "X-Forwarded-Groups",
	}
}

// Authenticate refuses a request with no identity header: behind an
// authenticating proxy, a request without one did not come through the
// proxy.
func (a *Authenticator) Authenticate(r *http.Request) (auth.Principal, error) {
	subject := strings.TrimSpace(r.Header.Get(a.Header))
	if subject == "" {
		return auth.Anonymous, auth.ErrUnauthenticated
	}
	p := auth.Principal{
		Subject: subject,
		Name:    strings.TrimSpace(r.Header.Get(a.NameHeader)),
		Email:   strings.ToLower(strings.TrimSpace(r.Header.Get(a.EmailHeader))),
	}
	if p.Name == "" {
		p.Name = subject
	}
	// A list header may arrive as one comma-separated line or as several
	// lines, and any proxy on the way may split it (RFC 9110, 5.3).
	for _, line := range r.Header.Values(a.GroupsHeader) {
		for _, g := range strings.Split(line, ",") {
			if g = strings.TrimSpace(g); g != "" {
				p.Roles = append(p.Roles, g)
			}
		}
	}
	return p, nil
}
