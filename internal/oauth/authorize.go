package oauth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/identity"
)

// authorize asks the signed-in person whether the client may act for
// them. Until the redirect address is known to be the client's, a problem
// is shown here; after, it goes back to the client (RFC 6749, 4.1.2.1).
func (s *Server) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	c, err := s.lookup(r.Context(), q.Get("client_id"))
	if err != nil {
		s.page(w, http.StatusBadRequest, pageData{Problem: err.Error()})
		return
	}
	redirect := q.Get("redirect_uri")
	if redirect == "" && len(c.Redirects) == 1 {
		redirect = c.Redirects[0]
	}
	if !c.redirectAllowed(redirect) {
		s.page(w, http.StatusBadRequest, pageData{Problem: "The redirect address is not one " + c.Name + " registered."})
		return
	}
	state := q.Get("state")
	back := func(code, description string) {
		http.Redirect(w, r, withQuery(redirect, url.Values{"error": {code}, "error_description": {description}, "state": {state}, "iss": {s.o.Issuer}}), http.StatusFound)
	}
	switch {
	case q.Get("response_type") != "code":
		back("unsupported_response_type", "only the code flow is supported")
		return
	case q.Get("code_challenge") == "" || q.Get("code_challenge_method") != "S256":
		back("invalid_request", "PKCE with S256 is required")
		return
	case q.Get("resource") != "" && q.Get("resource") != s.Resource():
		back("invalid_target", "this server issues tokens for "+s.Resource()+" only")
		return
	}
	who, ok := s.person(w, r)
	if !ok {
		return
	}
	consent := s.seal(claims{
		Type: typeConsent, Expires: time.Now().Add(consentLife).Unix(), Person: personOf(who),
		Client: c.ID, Name: c.Name, Redirect: redirect, Challenge: q.Get("code_challenge"), State: state,
	})
	s.page(w, http.StatusOK, pageData{Client: c.Name, Person: personOf(who), ReturnTo: hostOf(redirect), Consent: consent})
}

// consent records the person's answer. The sealed consent carries the
// request as it was shown, and whom to; only that person may answer it.
func (s *Server) consent(w http.ResponseWriter, r *http.Request) {
	var c claims
	if err := s.open(r.PostFormValue("consent"), typeConsent, &c); err != nil {
		s.page(w, http.StatusBadRequest, pageData{Problem: "This request has expired. Start connecting the agent again."})
		return
	}
	who, ok := s.person(w, r)
	if !ok {
		return
	}
	if subtle.ConstantTimeCompare([]byte(personOf(who)), []byte(c.Person)) != 1 {
		s.page(w, http.StatusForbidden, pageData{Problem: "This request was made for someone else."})
		return
	}
	if r.PostFormValue("decision") != "allow" {
		http.Redirect(w, r, withQuery(c.Redirect, url.Values{"error": {"access_denied"}, "state": {c.State}, "iss": {s.o.Issuer}}), http.StatusFound)
		return
	}
	grant, err := s.o.Grants.GrantAgent(identity.WithPrincipal(r.Context(), who), c.Name, 0)
	if err != nil {
		s.page(w, http.StatusForbidden, pageData{Problem: problem(err)})
		return
	}
	code := s.seal(claims{
		Type: typeCode, Expires: time.Now().Add(codeLife).Unix(), Grant: grant.ID,
		Client: c.Client, Redirect: c.Redirect, Challenge: c.Challenge, Audience: s.Resource(),
	})
	http.Redirect(w, r, withQuery(c.Redirect, url.Values{"code": {code}, "state": {c.State}, "iss": {s.o.Issuer}}), http.StatusFound)
}

// person is who is signed in, and may let an agent act for them; when
// not, it says why on the page and returns false.
func (s *Server) person(w http.ResponseWriter, r *http.Request) (identity.Principal, bool) {
	who, err := s.o.Person.Authenticate(r)
	if err != nil || who.Anonymous || personOf(who) == "" {
		s.page(w, http.StatusUnauthorized, pageData{Problem: "Sign in to Cartograph, then connect the agent again."})
		return identity.Principal{}, false
	}
	if who.Agent != "" {
		s.page(w, http.StatusForbidden, pageData{Problem: "An agent cannot connect another agent."})
		return identity.Principal{}, false
	}
	g, err := s.o.Grants.Grants(r.Context(), who)
	if err != nil || !g.Agents {
		s.page(w, http.StatusForbidden, pageData{Problem: "Agents are not enabled for you. An administrator enables them on the Access page."})
		return identity.Principal{}, false
	}
	return who, true
}

// token exchanges a code, or a refresh token, for an access token and the
// next refresh token. Each code and refresh token is good once: the
// grant's generation moves on, and one presented again revokes the grant.
func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	var grantID string
	var gen int
	clientID := r.PostFormValue("client_id")
	switch r.PostFormValue("grant_type") {
	case "authorization_code":
		var c claims
		if err := s.open(r.PostFormValue("code"), typeCode, &c); err != nil {
			oauthError(w, http.StatusBadRequest, "invalid_grant", "the code is not valid, or has expired")
			return
		}
		if c.Client != clientID || c.Redirect != r.PostFormValue("redirect_uri") {
			oauthError(w, http.StatusBadRequest, "invalid_grant", "the code was issued to another client or redirect address")
			return
		}
		if !verifies(r.PostFormValue("code_verifier"), c.Challenge) {
			oauthError(w, http.StatusBadRequest, "invalid_grant", "the code_verifier does not match")
			return
		}
		if res := r.PostFormValue("resource"); res != "" && res != s.Resource() {
			oauthError(w, http.StatusBadRequest, "invalid_target", "this server issues tokens for "+s.Resource()+" only")
			return
		}
		grantID, gen = c.Grant, 0
	case "refresh_token":
		var c claims
		if err := s.open(r.PostFormValue("refresh_token"), typeRefresh, &c); err != nil || c.Client != clientID {
			oauthError(w, http.StatusBadRequest, "invalid_grant", "the refresh token is not valid")
			return
		}
		grantID, gen = c.Grant, c.Gen
	default:
		oauthError(w, http.StatusBadRequest, "unsupported_grant_type", "authorization_code or refresh_token")
		return
	}
	next, err := s.o.Grants.AdvanceAgentGrant(r.Context(), grantID, gen)
	if err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_grant", problem(err))
		return
	}
	_, grant, err := s.o.Grants.AgentGrantPrincipal(r.Context(), grantID)
	if err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_grant", problem(err))
		return
	}
	expires := time.Now().Add(accessLife)
	if grant.ExpiresAt.Before(expires) {
		expires = grant.ExpiresAt
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  s.seal(claims{Type: typeAccess, Expires: expires.Unix(), Grant: grantID, Client: clientID, Audience: s.Resource()}),
		"token_type":    "Bearer",
		"expires_in":    int(time.Until(expires).Seconds()),
		"refresh_token": s.seal(claims{Type: typeRefresh, Expires: grant.ExpiresAt.Unix(), Grant: grantID, Gen: next, Client: clientID}),
		"scope":         Scope,
	})
}

// verifies is PKCE's S256 check (RFC 7636).
func verifies(verifier, challenge string) bool {
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	return subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(sum[:])), []byte(challenge)) == 1
}

func personOf(p identity.Principal) string {
	if p.Email != "" {
		return strings.ToLower(p.Email)
	}
	return p.Subject
}

func withQuery(base string, v url.Values) string {
	u, err := url.Parse(base)
	if err != nil {
		return base
	}
	q := u.Query()
	for k, vs := range v {
		if len(vs) > 0 && vs[0] != "" {
			q.Set(k, vs[0])
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func hostOf(raw string) string {
	if u, err := url.Parse(raw); err == nil {
		return u.Host
	}
	return raw
}

// problem is an error as a person reads it: its last clause.
func problem(err error) string {
	msg := err.Error()
	if errors.Is(err, identity.ErrForbidden) {
		msg = strings.TrimPrefix(msg, identity.ErrForbidden.Error()+": ")
	}
	if msg == "" {
		return msg
	}
	return strings.ToUpper(msg[:1]) + msg[1:] + "."
}

type pageData struct {
	Client, Person, ReturnTo, Consent, Problem string
}

// page shows the consent page, or a problem. It may not be framed, so it
// cannot be clicked through by a page laid over it.
func (s *Server) page(w http.ResponseWriter, status int, d pageData) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self' *; frame-ancestors 'none'")
	h.Set("Referrer-Policy", "no-referrer")
	w.WriteHeader(status)
	_ = consentPage.Execute(w, d)
}

var consentPage = template.Must(template.New("consent").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Connect an agent · Cartograph</title>
<style>
:root { color-scheme: light dark; --bg: #f7f7f5; --card: #fff; --ink: #1c1c1a; --muted: #5f5f5a; --line: #e2e2dd; --accent: #7c3aed; }
@media (prefers-color-scheme: dark) { :root { --bg: #161615; --card: #20201e; --ink: #ededea; --muted: #a3a39d; --line: #34342f; --accent: #a78bfa; } }
body { margin: 0; min-height: 100vh; display: grid; place-items: center; background: var(--bg); color: var(--ink); font: 16px/1.5 system-ui, sans-serif; }
main { box-sizing: border-box; width: min(28rem, calc(100vw - 32px)); background: var(--card); border: 1px solid var(--line); border-radius: 12px; padding: 28px; }
h1 { font-size: 1.25rem; margin: 0 0 4px; }
p { margin: 8px 0; color: var(--muted); }
ul { padding-left: 1.2rem; margin: 16px 0; }
li { margin: 6px 0; }
.who { font-size: .875rem; }
.actions { display: flex; gap: 12px; justify-content: flex-end; margin-top: 24px; }
button { font: inherit; padding: 8px 16px; border-radius: 8px; border: 1px solid var(--line); background: transparent; color: var(--ink); cursor: pointer; }
button[value=allow] { background: var(--accent); border-color: var(--accent); color: #fff; }
</style>
</head>
<body>
<main>
{{if .Problem}}
<h1>The agent cannot connect</h1>
<p>{{.Problem}}</p>
{{else}}
<h1>Let {{.Client}} act for you in Cartograph?</h1>
<p class="who">Signed in as {{.Person}}</p>
<ul>
<li>It reads what you can read.</li>
<li>It edits drafts, and the people working on them see it there.</li>
<li>It proposes saves, readings and project moves. Nothing is saved until you accept, under Proposals.</li>
<li>You can disconnect it at any time, on the Access page.</li>
</ul>
<p>After you answer, you return to {{.ReturnTo}}.</p>
<form method="post" action="/oauth/authorize" class="actions">
<input type="hidden" name="consent" value="{{.Consent}}">
<button type="submit" name="decision" value="deny">Cancel</button>
<button type="submit" name="decision" value="allow">Allow</button>
</form>
{{end}}
</main>
</body>
</html>
`))
