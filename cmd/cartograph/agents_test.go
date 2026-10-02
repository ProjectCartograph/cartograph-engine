package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/auth"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/auth/proxy"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/oauth"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/printer"
)

// asPerson sends the headers the proxy would, for one person.
type asPerson struct {
	email, groups string
}

func (p asPerson) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("X-Forwarded-User", p.email)
	r.Header.Set("X-Forwarded-Email", p.email)
	r.Header.Set("X-Forwarded-Groups", p.groups)
	return http.DefaultTransport.RoundTrip(r)
}

// Agents through exactly the stack serve runs: the proxy's headers, the
// access list's agents key, and the metadata that tells a client where
// to sign in (docs/adr/0016).
func TestAgentsThroughTheServeStack(t *testing.T) {
	dir := t.TempDir()
	if out, err := exec.Command("cp", "-r", "../../examples/minimal/.", dir).CombinedOutput(); err != nil {
		t.Fatalf("copy the example: %v %s", err, out)
	}
	file := writeFile(t, "access.yaml", accessYAML+"agents: [contributor]\n")
	d, err := loadDirectory(file)
	if err != nil {
		t.Fatal(err)
	}
	comp, err := compose(context.Background(), storeOptions{Target: dir, Codec: "yaml", Access: &d})
	if err != nil {
		t.Fatal(err)
	}
	defer comp.Close()
	var ready atomic.Bool
	ready.Store(true)
	mux, _ := routes(comp.Engine, comp.Fanout, proxy.New("X-Forwarded-User"), comp.Authz, printer.None{}, comp.Reports,
		agentsConfig{On: true, Issuer: "https://sso.example.org/dex"}, &ready)
	srv := httptest.NewServer(requestLog(slog.New(slog.NewTextHandler(io.Discard, nil)), nil, mux))
	defer srv.Close()

	// The metadata is public: it says where to get a token.
	resp, err := http.Get(srv.URL + "/.well-known/oauth-protected-resource/api/v1/mcp")
	if err != nil {
		t.Fatal(err)
	}
	var meta map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&meta)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || meta["resource"] != srv.URL+"/api/v1/mcp" || !strings.Contains(asText(meta["authorization_servers"]), "sso.example.org") {
		t.Fatalf("protected resource metadata: %d %v", resp.StatusCode, meta)
	}

	connect := func(p asPerson) (*sdk.ClientSession, error) {
		client := sdk.NewClient(&sdk.Implementation{Name: "Claude", Version: "1"}, nil)
		return client.Connect(context.Background(), &sdk.StreamableClientTransport{Endpoint: srv.URL + "/api/v1/mcp", HTTPClient: &http.Client{Transport: p}}, nil)
	}
	// A contributor's agent works.
	cs, err := connect(asPerson{"lee@example.org", "quality-team"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "get", Arguments: map[string]any{"kind": "Team", "id": "quality-team"}})
	if err != nil || res.IsError {
		t.Fatalf("a contributor's agent reading: %v %+v", err, res)
	}
	cs.Close()

	// An administrator's does not: the mapping allows agents for
	// contributors only.
	if cs, err := connect(asPerson{"ada@example.org", "cartograph-administrators"}); err == nil {
		res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "get", Arguments: map[string]any{"kind": "Team", "id": "quality-team"}})
		cs.Close()
		if err == nil && !res.IsError {
			t.Fatal("an agent its person's roles do not allow read the record")
		}
	}
}

// bearer sends an access token, and nothing else.
type bearer string

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+string(b))
	return http.DefaultTransport.RoundTrip(r)
}

// With Cartograph as the authorization server, through the stack serve
// runs: a client registers itself, its person consents behind the proxy,
// and the agent then carries only its token; a proxy's headers on the MCP
// path count for nothing.
func TestCartographAuthorizesAgents(t *testing.T) {
	dir := t.TempDir()
	if out, err := exec.Command("cp", "-r", "../../examples/minimal/.", dir).CombinedOutput(); err != nil {
		t.Fatalf("copy the example: %v %s", err, out)
	}
	file := writeFile(t, "access.yaml", accessYAML+"agents: [contributor]\n")
	d, err := loadDirectory(file)
	if err != nil {
		t.Fatal(err)
	}
	comp, err := compose(context.Background(), storeOptions{Target: dir, Codec: "yaml", Access: &d})
	if err != nil {
		t.Fatal(err)
	}
	defer comp.Close()
	var ready atomic.Bool
	ready.Store(true)
	var handler http.Handler
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	defer srv.Close()
	authn := proxy.New("X-Forwarded-User")
	as, err := oauth.New(oauth.Options{Grants: comp.Engine, Person: authn, Key: []byte(strings.Repeat("k", 32)), Issuer: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	mux, _ := routes(comp.Engine, comp.Fanout, authn, comp.Authz, printer.None{}, comp.Reports, agentsConfig{On: true, Issuer: srv.URL, OAuth: as}, &ready)
	handler = mux

	// A proxy's headers, with no token, are not an agent's identity.
	req, _ := http.NewRequest("POST", srv.URL+"/api/v1/mcp", strings.NewReader("{}"))
	req.Header.Set("X-Forwarded-User", "lee@example.org")
	req.Header.Set("X-Forwarded-Email", "lee@example.org")
	req.Header.Set("X-Forwarded-Groups", "quality-team")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized || !strings.Contains(resp.Header.Get("WWW-Authenticate"), "resource_metadata=") {
		t.Fatalf("forged headers on the MCP path: %d %q", resp.StatusCode, resp.Header.Get("WWW-Authenticate"))
	}

	// The client registers, Lee consents, the code becomes a token.
	resp, err = http.Post(srv.URL+"/oauth/register", "application/json", strings.NewReader(`{"client_name":"Claude","redirect_uris":["http://127.0.0.1/cb"]}`))
	if err != nil {
		t.Fatal(err)
	}
	var reg map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&reg)
	resp.Body.Close()
	clientID, _ := reg["client_id"].(string)
	const verifier = "a-code-verifier-of-at-least-forty-three-characters-long"
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {"http://127.0.0.1:4100/cb"}, "state": {"s"},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"}, "resource": {srv.URL + "/api/v1/mcp"}}
	lee := &http.Client{Transport: asPerson{"lee@example.org", "quality-team"}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err = lee.Get(srv.URL + "/oauth/authorize?" + q.Encode())
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	m := regexp.MustCompile(`name="consent" value="([^"]+)"`).FindSubmatch(page)
	if m == nil {
		t.Fatalf("the consent page: %d %s", resp.StatusCode, page)
	}
	resp, err = lee.PostForm(srv.URL+"/oauth/authorize", url.Values{"consent": {html.UnescapeString(string(m[1]))}, "decision": {"allow"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	loc, _ := url.Parse(resp.Header.Get("Location"))
	resp, err = http.PostForm(srv.URL+"/oauth/token", url.Values{"grant_type": {"authorization_code"}, "code": {loc.Query().Get("code")},
		"client_id": {clientID}, "redirect_uri": {"http://127.0.0.1:4100/cb"}, "code_verifier": {verifier}})
	if err != nil {
		t.Fatal(err)
	}
	var tokens map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&tokens)
	resp.Body.Close()
	token, _ := tokens["access_token"].(string)
	if token == "" {
		t.Fatalf("the token response: %v", tokens)
	}

	// The agent, by its token alone, acts for Lee with Lee's teams.
	client := sdk.NewClient(&sdk.Implementation{Name: "Something else", Version: "1"}, nil)
	cs, err := client.Connect(context.Background(), &sdk.StreamableClientTransport{Endpoint: srv.URL + "/api/v1/mcp", HTTPClient: &http.Client{Transport: bearer(token)}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "get", Arguments: map[string]any{"kind": "Team", "id": "quality-team"}})
	if err != nil || res.IsError {
		t.Fatalf("the agent reading: %v %+v", err, res)
	}
	grants, err := comp.Engine.AgentGrants(auth.WithPrincipal(context.Background(), auth.Principal{Subject: "lee@example.org", Email: "lee@example.org", Roles: []string{"quality-team"}}), "")
	if err != nil || len(grants) != 1 || grants[0].Label != "Claude" || grants[0].LastUsed.IsZero() {
		t.Fatalf("Lee's agents: %+v, %v", grants, err)
	}

	// A client that cannot sign in through the browser takes a token Lee
	// pastes into it; disconnecting it ends it at once.
	resp, err = lee.Post(srv.URL+"/api/v1/agents", "application/json", strings.NewReader(`{"label":"A script","days":7}`))
	if err != nil {
		t.Fatal(err)
	}
	var minted struct {
		Grant struct{ ID string }
		Token string
	}
	_ = json.NewDecoder(resp.Body).Decode(&minted)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated || minted.Token == "" {
		t.Fatalf("a pasted token: %d %+v", resp.StatusCode, minted)
	}
	mcpStatus := func(token string) int {
		req, _ := http.NewRequest("POST", srv.URL+"/api/v1/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if got := mcpStatus(minted.Token); got != http.StatusOK {
		t.Fatalf("the pasted token: %d", got)
	}
	var mine []struct{ ID, Label string }
	resp, _ = lee.Get(srv.URL + "/api/v1/agents")
	_ = json.NewDecoder(resp.Body).Decode(&mine)
	resp.Body.Close()
	if len(mine) != 2 || mine[0].Label != "A script" {
		t.Fatalf("Lee's agents, newest first: %+v", mine)
	}
	req, _ = http.NewRequest("DELETE", srv.URL+"/api/v1/agents/"+mine[0].ID, nil)
	if resp, err = lee.Do(req); err != nil || resp.StatusCode != http.StatusNoContent {
		t.Fatalf("disconnecting: %v %v", resp.StatusCode, err)
	}
	resp.Body.Close()
	if got := mcpStatus(minted.Token); got != http.StatusUnauthorized {
		t.Fatalf("the token after disconnecting: %d", got)
	}
}

func asText(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
