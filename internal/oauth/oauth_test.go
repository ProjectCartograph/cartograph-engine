package oauth_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/auth"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/auth/access"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/auth/proxy"
	codecyaml "github.com/ProjectCartograph/cartograph-engine/v2/internal/codec/yaml"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/identity"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/oauth"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store/memory"
)

const verifier = "a-code-verifier-of-at-least-forty-three-characters-long"

func challenge(v string) string {
	sum := sha256.Sum256([]byte(v))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// fixture is Cartograph's authorization server in front of an engine
// whose contributors may use agents, with the person signed in by the
// proxy's headers, as serve composes it.
type fixture struct {
	t   *testing.T
	e   *engine.Engine
	srv *httptest.Server
	as  *oauth.Server
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	var e *engine.Engine
	policy := access.Policy{Grants: func(ctx context.Context, p auth.Principal) (auth.Grants, error) { return e.Grants(ctx, p) }}
	e, err := engine.New(memory.NewManifestStore(), memory.NewOperationalStore(), engine.WithCodec(codecyaml.New()),
		engine.WithAuthorizer(policy), engine.WithAccess(memory.NewAccessStore(), engine.Directory{
			Roles:  map[string][]string{auth.RoleContributor: {"g-team"}, auth.RoleReader: {"g-staff"}},
			Agents: []string{auth.RoleContributor},
		}))
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, e: e}
	mux := http.NewServeMux()
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	f.as, err = oauth.New(oauth.Options{Grants: e, Person: proxy.New("X-Forwarded-User"), Key: []byte(strings.Repeat("k", 32)), Issuer: f.srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range oauth.Routes {
		mux.Handle(p, f.as.Handler())
	}
	mux.Handle(oauth.MCPPath, f.as.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := identity.PrincipalFrom(r.Context())
		_ = json.NewEncoder(w).Encode(map[string]any{"email": p.Email, "agent": p.Agent, "delegated": p.Delegated})
	}), func(r *http.Request, p identity.Principal) *http.Request {
		return r.WithContext(identity.WithPrincipal(r.Context(), p))
	}))
	return f
}

// do sends a request as email (signed in by the proxy) or as nobody,
// without following redirects.
func (f *fixture) do(method, path, email, groups string, form url.Values, header ...string) *http.Response {
	f.t.Helper()
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	req, _ := http.NewRequest(method, f.srv.URL+path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if email != "" {
		req.Header.Set("X-Forwarded-User", "sub-"+email)
		req.Header.Set("X-Forwarded-Email", email)
		req.Header.Set("X-Forwarded-Groups", groups)
	}
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	resp, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func decode(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func (f *fixture) register(redirects ...string) string {
	f.t.Helper()
	b, _ := json.Marshal(map[string]any{"client_name": "Claude", "redirect_uris": redirects})
	resp, err := http.Post(f.srv.URL+"/oauth/register", "application/json", strings.NewReader(string(b)))
	if err != nil || resp.StatusCode != http.StatusCreated {
		f.t.Fatalf("registering: %v %v", resp.StatusCode, err)
	}
	defer resp.Body.Close()
	return decode(f.t, resp)["client_id"].(string)
}

var consentField = regexp.MustCompile(`name="consent" value="([^"]+)"`)

// connect runs the browser flow as email, and returns the code.
func (f *fixture) connect(clientID, redirect, email, groups string) string {
	f.t.Helper()
	q := url.Values{"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {redirect},
		"code_challenge": {challenge(verifier)}, "code_challenge_method": {"S256"}, "state": {"s1"}, "resource": {f.as.Resource()}}
	resp := f.do("GET", "/oauth/authorize?"+q.Encode(), email, groups, nil)
	page := new(strings.Builder)
	_, _ = io.Copy(page, resp.Body)
	m := consentField.FindStringSubmatch(page.String())
	if resp.StatusCode != http.StatusOK || m == nil || !strings.Contains(page.String(), "Let Claude act for you") {
		f.t.Fatalf("the consent page: %d %s", resp.StatusCode, page)
	}
	resp = f.do("POST", "/oauth/authorize", email, groups, url.Values{"consent": {html.UnescapeString(m[1])}, "decision": {"allow"}})
	loc, _ := url.Parse(resp.Header.Get("Location"))
	if resp.StatusCode != http.StatusFound || loc.Query().Get("state") != "s1" || loc.Query().Get("code") == "" || loc.Query().Get("iss") != f.srv.URL {
		f.t.Fatalf("allowing: %d %s", resp.StatusCode, loc)
	}
	return loc.Query().Get("code")
}

func (f *fixture) exchange(form url.Values) (int, map[string]any) {
	f.t.Helper()
	resp := f.do("POST", "/oauth/token", "", "", form)
	return resp.StatusCode, decode(f.t, resp)
}

func (f *fixture) mcp(token string) *http.Response {
	return f.do("POST", oauth.MCPPath, "", "", nil, "Authorization", "Bearer "+token)
}

func TestTheBrowserFlow(t *testing.T) {
	f := newFixture(t)
	const redirect = "http://127.0.0.1:33418/callback"

	// An MCP client with no token is pointed at where to start.
	resp := f.do("POST", oauth.MCPPath, "", "", nil)
	if resp.StatusCode != http.StatusUnauthorized || !strings.Contains(resp.Header.Get("WWW-Authenticate"), "/.well-known/oauth-protected-resource/api/v1/mcp") {
		t.Fatalf("no token: %d %q", resp.StatusCode, resp.Header.Get("WWW-Authenticate"))
	}
	meta := decode(t, f.do("GET", "/.well-known/oauth-authorization-server", "", "", nil))
	if meta["issuer"] != f.srv.URL || meta["client_id_metadata_document_supported"] != true {
		t.Fatalf("metadata: %v", meta)
	}

	client := f.register("http://127.0.0.1/callback")
	// A loopback redirect matches on any port; the client picks one.
	code := f.connect(client, redirect, "lee@example.org", "g-team")

	exchange := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "client_id": {client}, "redirect_uri": {redirect}, "code_verifier": {"x" + verifier[1:]}}
	if status, body := f.exchange(exchange); status != http.StatusBadRequest || body["error"] != "invalid_grant" {
		t.Fatalf("the wrong verifier: %d %v", status, body)
	}
	exchange.Set("code_verifier", verifier)
	status, tokens := f.exchange(exchange)
	if status != http.StatusOK || tokens["token_type"] != "Bearer" {
		t.Fatalf("exchanging the code: %d %v", status, tokens)
	}
	who := decode(t, f.mcp(tokens["access_token"].(string)))
	if who["email"] != "lee@example.org" || who["agent"] != "Claude" || who["delegated"] != true {
		t.Fatalf("the agent's principal: %v", who)
	}

	// Refreshing gives the next pair; the spent refresh token, presented
	// again, means one of the two was not Lee's agent, and ends the grant.
	refresh := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tokens["refresh_token"].(string)}, "client_id": {client}}
	status, next := f.exchange(refresh)
	if status != http.StatusOK || next["refresh_token"] == tokens["refresh_token"] {
		t.Fatalf("refreshing: %d %v", status, next)
	}
	if status, body := f.exchange(refresh); status != http.StatusBadRequest || body["error"] != "invalid_grant" {
		t.Fatalf("a refresh token used twice: %d %v", status, body)
	}
	if got := f.mcp(next["access_token"].(string)).StatusCode; got != http.StatusUnauthorized {
		t.Fatalf("after reuse, the newest access token: %d", got)
	}

	// A code exchanged twice ends its grant too.
	code = f.connect(client, redirect, "lee@example.org", "g-team")
	exchange.Set("code", code)
	if status, _ := f.exchange(exchange); status != http.StatusOK {
		t.Fatalf("a second connection: %d", status)
	}
	if status, _ := f.exchange(exchange); status != http.StatusBadRequest {
		t.Fatalf("a code used twice: %d", status)
	}
}

func TestWhoMayConnect(t *testing.T) {
	f := newFixture(t)
	client := f.register("https://agent.example.org/callback")
	q := url.Values{"response_type": {"code"}, "client_id": {client}, "redirect_uri": {"https://agent.example.org/callback"},
		"code_challenge": {challenge(verifier)}, "code_challenge_method": {"S256"}, "state": {"s"}}
	if got := f.do("GET", "/oauth/authorize?"+q.Encode(), "", "", nil).StatusCode; got != http.StatusUnauthorized {
		t.Fatalf("nobody signed in: %d", got)
	}
	if got := f.do("GET", "/oauth/authorize?"+q.Encode(), "noor@example.org", "g-staff", nil).StatusCode; got != http.StatusForbidden {
		t.Fatalf("a reader, whose agents are not enabled: %d", got)
	}
	q.Set("redirect_uri", "https://elsewhere.example.org/callback")
	if got := f.do("GET", "/oauth/authorize?"+q.Encode(), "lee@example.org", "g-team", nil).StatusCode; got != http.StatusBadRequest {
		t.Fatalf("a redirect the client did not register: %d", got)
	}
	q.Set("redirect_uri", "https://agent.example.org/callback")
	q.Del("code_challenge")
	resp := f.do("GET", "/oauth/authorize?"+q.Encode(), "lee@example.org", "g-team", nil)
	if loc, _ := url.Parse(resp.Header.Get("Location")); resp.StatusCode != http.StatusFound || loc.Query().Get("error") != "invalid_request" {
		t.Fatalf("no PKCE: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}

	// Lee's consent, answered by someone else's session.
	q.Set("code_challenge", challenge(verifier))
	page := new(strings.Builder)
	_, _ = io.Copy(page, f.do("GET", "/oauth/authorize?"+q.Encode(), "lee@example.org", "g-team", nil).Body)
	m := consentField.FindStringSubmatch(page.String())
	if got := f.do("POST", "/oauth/authorize", "sam@example.org", "g-team", url.Values{"consent": {html.UnescapeString(m[1])}, "decision": {"allow"}}).StatusCode; got != http.StatusForbidden {
		t.Fatalf("someone else allowing Lee's: %d", got)
	}
	resp = f.do("POST", "/oauth/authorize", "lee@example.org", "g-team", url.Values{"consent": {html.UnescapeString(m[1])}, "decision": {"deny"}})
	if loc, _ := url.Parse(resp.Header.Get("Location")); loc.Query().Get("error") != "access_denied" {
		t.Fatalf("cancelling: %s", resp.Header.Get("Location"))
	}

	for _, bad := range []string{"http://agent.example.org/callback", "javascript:alert(1)", "https://agent.example.org/#x"} {
		b, _ := json.Marshal(map[string]any{"redirect_uris": []string{bad}})
		resp, _ := http.Post(f.srv.URL+"/oauth/register", "application/json", strings.NewReader(string(b)))
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("registering %q: %d", bad, resp.StatusCode)
		}
	}
	if got := f.mcp("cga_forged.signature").StatusCode; got != http.StatusUnauthorized {
		t.Fatalf("a forged token: %d", got)
	}
}

// A client identified by its metadata document, fetched at consent.
func TestClientMetadataDocuments(t *testing.T) {
	var docURL string
	doc := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"client_id": docURL, "client_name": "An IDE", "redirect_uris": []string{"http://localhost/cb"}})
	}))
	defer doc.Close()
	docURL = doc.URL + "/client.json"

	f := newFixture(t)
	// The test's document is on loopback, which the default fetcher
	// refuses: that is checked below.
	var err error
	f.as, err = oauth.New(oauth.Options{Grants: nil, Person: proxy.New("X-Forwarded-User"), Key: []byte(strings.Repeat("k", 32)), Issuer: f.srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	q := url.Values{"response_type": {"code"}, "client_id": {docURL}, "redirect_uri": {"http://localhost:5000/cb"},
		"code_challenge": {challenge(verifier)}, "code_challenge_method": {"S256"}}
	rec := httptest.NewRecorder()
	f.as.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/oauth/authorize?"+q.Encode(), nil))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "not a public address") {
		t.Fatalf("a document on a private address: %d %s", rec.Code, rec.Body)
	}

	g := newFixture(t)
	g.as, _ = oauth.New(oauth.Options{Grants: g.e, Person: proxy.New("X-Forwarded-User"), Key: []byte(strings.Repeat("k", 32)), Issuer: g.srv.URL, Fetch: doc.Client()})
	rec = httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/oauth/authorize?"+q.Encode(), nil)
	req.Header.Set("X-Forwarded-User", "sub-lee")
	req.Header.Set("X-Forwarded-Email", "lee@example.org")
	req.Header.Set("X-Forwarded-Groups", "g-team")
	g.as.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Let An IDE act for you") {
		t.Fatalf("a document's client: %d %s", rec.Code, rec.Body)
	}
}
