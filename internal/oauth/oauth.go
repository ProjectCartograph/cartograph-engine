// Package oauth is Cartograph's own OAuth 2.1 authorization server for
// agents (docs/adr/0016): the browser flow an MCP client starts, with no
// client registered anywhere beforehand. A client registers itself, by a
// Client ID Metadata Document or by dynamic registration; its person
// signs in as they always do, through whatever authenticates the
// deployment, and consents on a page here; the client gets a short-lived
// access token and a refresh token, both bound to an agent grant the
// engine keeps and its person or an administrator revokes.
//
// It is one adapter of two. A deployment whose own stack is the
// authorization server (an identity provider behind a proxy, or Laravel
// Passport) leaves it out, and its proxy authenticates MCP requests as it
// does every other.
//
// Nothing is kept here between requests: registrations, codes and tokens
// are signed with the deployment's key, and a grant's state is the
// engine's.
package oauth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/identity"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

// Grants is what this adapter needs of the engine: grants are recorded,
// resolved and advanced there, under its rules.
type Grants interface {
	Grants(ctx context.Context, p identity.Principal) (identity.Grants, error)
	GrantAgent(ctx context.Context, label string, ttl time.Duration) (store.AgentGrant, error)
	AgentGrantPrincipal(ctx context.Context, id string) (identity.Principal, store.AgentGrant, error)
	AdvanceAgentGrant(ctx context.Context, id string, gen int) (int, error)
}

// Authenticator says who is signing in at the consent page: the same
// authenticator as every other request.
type Authenticator interface {
	Authenticate(r *http.Request) (identity.Principal, error)
}

// Options configures a Server.
type Options struct {
	Grants Grants
	// Person identifies the person at the consent page.
	Person Authenticator
	// Key signs registrations, codes and tokens: the same on every
	// replica, at least 32 bytes.
	Key []byte
	// Issuer is this deployment's public address, such as
	// https://cartograph.example.org, with no trailing slash.
	Issuer string
	// Fetch fetches Client ID Metadata Documents. nil is a client that
	// refuses private and loopback addresses.
	Fetch *http.Client
}

// Lifetimes.
const (
	codeLife    = time.Minute
	accessLife  = time.Hour
	consentLife = 10 * time.Minute
)

// MCPPath is where the protected resource is served.
const MCPPath = "/api/v1/mcp"

// Server is the authorization server, and the authenticator for the
// tokens it issues.
type Server struct {
	o Options
}

// ErrShortKey is a signing key under 32 bytes.
var ErrShortKey = errors.New("the agent signing key must be at least 32 bytes")

// New returns a Server.
func New(o Options) (*Server, error) {
	if len(o.Key) < 32 {
		return nil, ErrShortKey
	}
	o.Issuer = strings.TrimRight(o.Issuer, "/")
	if o.Fetch == nil {
		o.Fetch = publicClient()
	}
	return &Server{o: o}, nil
}

// Resource is the MCP endpoint's address, which every access token
// names as its audience.
func (s *Server) Resource() string { return s.o.Issuer + MCPPath }

// Handler serves the authorization server's endpoints and metadata.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", s.metadata)
	mux.HandleFunc("GET /.well-known/oauth-protected-resource", s.resourceMetadata)
	mux.HandleFunc("GET /.well-known/oauth-protected-resource"+MCPPath, s.resourceMetadata)
	mux.HandleFunc("POST /oauth/register", s.register)
	mux.HandleFunc("GET /oauth/authorize", s.authorize)
	mux.HandleFunc("POST /oauth/authorize", s.consent)
	mux.HandleFunc("POST /oauth/token", s.token)
	return mux
}

// Routes are the paths Handler serves, for a mux to send there.
var Routes = []string{
	"/.well-known/oauth-authorization-server",
	"/.well-known/oauth-protected-resource",
	"/.well-known/oauth-protected-resource" + MCPPath,
	"/oauth/register",
	"/oauth/authorize",
	"/oauth/token",
}

func (s *Server) metadata(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                s.o.Issuer,
		"authorization_endpoint":                s.o.Issuer + "/oauth/authorize",
		"token_endpoint":                        s.o.Issuer + "/oauth/token",
		"registration_endpoint":                 s.o.Issuer + "/oauth/register",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"},
		"scopes_supported":                      []string{Scope},
		"client_id_metadata_document_supported": true,
	})
}

// Scope is the one scope there is: an agent acts for its person, under
// ADR 0016's ceiling, and no scope widens or narrows that.
const Scope = "cartograph"

func (s *Server) resourceMetadata(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"resource":                 s.Resource(),
		"authorization_servers":    []string{s.o.Issuer},
		"bearer_methods_supported": []string{"header"},
		"scopes_supported":         []string{Scope},
		"resource_name":            "Cartograph",
	})
}

// Authenticate is who an access token acts for: its grant's person, with
// the agent beside them. A request with no token, or a bad one, is
// refused.
func (s *Server) Authenticate(r *http.Request) (identity.Principal, error) {
	raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return identity.Principal{}, errNoToken
	}
	var c claims
	if err := s.open(strings.TrimSpace(raw), typeAccess, &c); err != nil || c.Audience != s.Resource() {
		return identity.Principal{}, errBadToken
	}
	p, _, err := s.o.Grants.AgentGrantPrincipal(r.Context(), c.Grant)
	if err != nil {
		return identity.Principal{}, errBadToken
	}
	return p, nil
}

var (
	errNoToken  = errors.New("no bearer token")
	errBadToken = errors.New("the bearer token is not valid")
)

// Protect authenticates every request to next by its access token, and
// answers one without a good token as MCP clients expect: 401, pointing
// at the protected resource metadata, where the browser flow starts.
func (s *Server) Protect(next http.Handler, onPrincipal func(*http.Request, identity.Principal) *http.Request) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, err := s.Authenticate(r)
		if err != nil {
			challenge := `Bearer resource_metadata="` + s.o.Issuer + `/.well-known/oauth-protected-resource` + MCPPath + `", scope="` + Scope + `"`
			if errors.Is(err, errBadToken) {
				challenge += `, error="invalid_token"`
			}
			w.Header().Set("WWW-Authenticate", challenge)
			writeJSON(w, http.StatusUnauthorized, map[string]any{"problems": []map[string]string{{"message": "unauthenticated"}}})
			return
		}
		next.ServeHTTP(w, onPrincipal(r, p))
	})
}

// What a signed value is, so one kind is never taken for another.
const (
	typeClient  = "client"
	typeConsent = "consent"
	typeCode    = "code"
	typeAccess  = "access"
	typeRefresh = "refresh"
)

// prefixes start each kind of value, so a person or a secret scanner can
// tell them apart.
var prefixes = map[string]string{
	typeClient: "cgc_", typeConsent: "cgx_", typeCode: "cgk_", typeAccess: "cga_", typeRefresh: "cgr_",
}

// claims is every signed value's body; each kind uses some fields.
type claims struct {
	Type      string   `json:"typ"`
	Expires   int64    `json:"exp,omitempty"`
	Grant     string   `json:"g,omitempty"`
	Gen       int      `json:"gen,omitempty"`
	Client    string   `json:"cid,omitempty"`
	Redirect  string   `json:"ruri,omitempty"`
	Challenge string   `json:"cc,omitempty"`
	Audience  string   `json:"aud,omitempty"`
	State     string   `json:"st,omitempty"`
	Person    string   `json:"sub,omitempty"`
	Name      string   `json:"name,omitempty"`
	Redirects []string `json:"ruris,omitempty"`
}

func (s *Server) seal(c claims) string {
	body, _ := json.Marshal(c)
	head := prefixes[c.Type] + base64.RawURLEncoding.EncodeToString(body)
	mac := hmac.New(sha256.New, s.o.Key)
	mac.Write([]byte(head))
	return head + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

var errBadSeal = errors.New("not a value this server signed")

// open checks a sealed value's signature, kind and expiry, and decodes it
// into c.
func (s *Server) open(raw, typ string, c *claims) error {
	head, sig, ok := strings.Cut(raw, ".")
	if !ok || !strings.HasPrefix(head, prefixes[typ]) {
		return errBadSeal
	}
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		return errBadSeal
	}
	mac := hmac.New(sha256.New, s.o.Key)
	mac.Write([]byte(head))
	if !hmac.Equal(got, mac.Sum(nil)) {
		return errBadSeal
	}
	body, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(head, prefixes[typ]))
	if err != nil || json.Unmarshal(body, c) != nil || c.Type != typ {
		return errBadSeal
	}
	if c.Expires != 0 && time.Now().Unix() >= c.Expires {
		return errBadSeal
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// oauthError answers a token or registration request as RFC 6749 says.
func oauthError(w http.ResponseWriter, status int, code, description string) {
	writeJSON(w, status, map[string]string{"error": code, "error_description": description})
}

// Mint lets an agent act for the signed-in person by a token they paste
// into its client, for a client that cannot sign in through the browser:
// an access token that lasts as long as its grant, and ends with it.
func (s *Server) Mint(ctx context.Context, label string, ttl time.Duration) (string, store.AgentGrant, error) {
	grant, err := s.o.Grants.GrantAgent(ctx, label, ttl)
	if err != nil {
		return "", store.AgentGrant{}, err
	}
	return s.seal(claims{Type: typeAccess, Expires: grant.ExpiresAt.Unix(), Grant: grant.ID, Client: "pasted", Audience: s.Resource()}), grant, nil
}
