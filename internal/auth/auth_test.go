package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/auth"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/auth/proxy"
)

func principalEcho(t *testing.T) (http.Handler, *auth.Principal) {
	t.Helper()
	var seen auth.Principal
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = auth.PrincipalFrom(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}), &seen
}

func TestNoAuthenticationIsAnonymousAndRecordsTheFallbackActor(t *testing.T) {
	next, seen := principalEcho(t)
	h := auth.Middleware(auth.NoAuthentication{}, next)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status %d", rec.Code)
	}
	if !seen.Anonymous || seen.Actor("operator") != "operator" {
		t.Fatalf("got %+v", *seen)
	}
}

func TestProxyReadsTheIdentityHeaders(t *testing.T) {
	next, seen := principalEcho(t)
	h := auth.Middleware(proxy.New("X-Forwarded-User"), next)
	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("X-Forwarded-User", "jo@example.org")
	req.Header.Set("X-Forwarded-Preferred-Username", "Jo")
	req.Header.Set("X-Forwarded-Groups", "editors, admins")
	req.Header.Set("X-Forwarded-Email", " Jo@Example.org ")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status %d", rec.Code)
	}
	if seen.Anonymous || seen.Subject != "jo@example.org" || seen.Name != "Jo" || len(seen.Roles) != 2 || seen.Roles[1] != "admins" {
		t.Fatalf("got %+v", *seen)
	}
	// The address is what an access list knows a person by, so it is
	// compared lower-case, without the spaces a header may carry.
	if seen.Email != "jo@example.org" {
		t.Fatalf("got %+v", *seen)
	}
	if seen.Actor("operator") != "jo@example.org" {
		t.Fatalf("an authenticated principal is the actor, got %q", seen.Actor("operator"))
	}
}

func TestProxyRefusesARequestWithoutTheHeader(t *testing.T) {
	next, _ := principalEcho(t)
	h := auth.Middleware(proxy.New("X-Forwarded-User"), next)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestPrincipalFromAnEmptyContextIsAnonymous(t *testing.T) {
	if !auth.PrincipalFrom(httptest.NewRequest("GET", "/", nil).Context()).Anonymous {
		t.Fatal("expected anonymous")
	}
}
