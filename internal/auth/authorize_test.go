package auth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/auth"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/auth/roles"
)

func TestActionForClassifiesMethodAndPath(t *testing.T) {
	cases := []struct {
		method, path, verb, kind, id, resource string
	}{
		{"GET", "/manifests/Goal/g1", "read", "Goal", "g1", ""},
		{"PUT", "/manifests/Goal/g1", "write", "Goal", "g1", ""},
		{"DELETE", "/manifests/Goal/g1", "write", "Goal", "g1", ""},
		{"POST", "/manifests/Project/p1/state", "write", "Project", "p1", ""},
		{"GET", "/goals/tree", "read", "", "", ""},
		{"POST", "/vault/apply", "write", "", "", "vault"},
		{"POST", "/validate/Goal", "read", "Goal", "", ""},
		{"GET", "/access/people", "read", "", "", "access"},
		{"PUT", "/access/people/jo@example.org", "write", "", "", "access"},
		{"GET", "/session", "read", "", "", "session"},
		{"POST", "/proposals/p1/accept", "read", "", "", ""},
		{"POST", "/mcp", "read", "", "", "agent"},
		{"DELETE", "/agents/g1", "read", "", "", ""},
	}
	for _, c := range cases {
		a := auth.ActionFor(httptest.NewRequest(c.method, c.path, nil))
		if a.Verb != c.verb || a.Kind != c.kind || a.ID != c.id || a.Resource != c.resource || a.Change != nil {
			t.Errorf("%s %s: got %+v", c.method, c.path, a)
		}
	}
}

func withPrincipal(p auth.Principal, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(auth.WithPrincipal(r.Context(), p)))
	})
}

func TestRolesPolicy(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	policy := roles.Parse("readers", "editors, admins")
	h := auth.Authorize(policy, ok)

	try := func(p auth.Principal, method, path string) int {
		rec := httptest.NewRecorder()
		withPrincipal(p, h).ServeHTTP(rec, httptest.NewRequest(method, path, nil))
		return rec.Code
	}
	reader := auth.Principal{Subject: "r", Roles: []string{"readers"}}
	editor := auth.Principal{Subject: "e", Roles: []string{"editors"}}
	nobody := auth.Principal{Subject: "n", Roles: []string{"guests"}}

	if got := try(reader, "GET", "/manifests/Goal/g1"); got != 204 {
		t.Errorf("reader read: %d", got)
	}
	if got := try(reader, "PUT", "/manifests/Goal/g1"); got != 403 {
		t.Errorf("reader write: %d", got)
	}
	if got := try(editor, "PUT", "/manifests/Goal/g1"); got != 204 {
		t.Errorf("editor write: %d", got)
	}
	if got := try(editor, "GET", "/goals/tree"); got != 204 {
		t.Errorf("editor read: %d", got)
	}
	if got := try(nobody, "GET", "/goals/tree"); got != 403 {
		t.Errorf("guest read with a read role set: %d", got)
	}
	if got := try(auth.Anonymous, "GET", "/goals/tree"); got != 403 {
		t.Errorf("anonymous: %d", got)
	}

	rec := httptest.NewRecorder()
	withPrincipal(reader, h).ServeHTTP(rec, httptest.NewRequest("PUT", "/manifests/Goal/g1", nil))
	if !strings.Contains(rec.Body.String(), `"problems"`) || !strings.Contains(rec.Body.String(), "may not write Goal") {
		t.Fatalf("a refusal is a problem list naming the action, got %s", rec.Body.String())
	}
}

func TestOpenReadSetLetsAnyAuthenticatedPrincipalRead(t *testing.T) {
	policy := roles.Parse("", "editors")
	p := auth.Principal{Subject: "x", Roles: []string{"anything"}}
	if err := policy.Authorize(context.Background(), p, auth.Action{Verb: auth.VerbRead}); err != nil {
		t.Fatalf("read should be open: %v", err)
	}
	if err := policy.Authorize(context.Background(), p, auth.Action{Verb: auth.VerbWrite}); err == nil {
		t.Fatal("write should be refused")
	}
}
