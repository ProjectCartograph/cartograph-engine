package auth

import (
	"net/http"
	"strings"
)

// Verbs an Action carries. Read is any safe method; Write is the rest.
const (
	VerbRead  = "read"
	VerbWrite = "write"
)

// ActionFor classifies a request: its verb from the method, and the
// manifest it concerns from the path when the path names one
// (/manifests/{kind}/{id}...). Everything else (the goal tree, the
// snapshots list, the state manifest) is an action with no kind; the
// access list, the vault and the session are named as resources.
func ActionFor(r *http.Request) Action {
	a := Action{Verb: VerbWrite}
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		a.Verb = VerbRead
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) >= 2 && parts[0] == "manifests" {
		a.Kind = parts[1]
		if len(parts) >= 3 {
			a.ID = parts[2]
		}
	}
	if len(parts) >= 2 && parts[0] == "validate" {
		a.Verb = VerbRead // validation changes nothing
		a.Kind = parts[1]
	}
	switch parts[0] {
	case ResourceAccess, ResourceVault, ResourceSession:
		a.Resource = parts[0]
	}
	return a
}

// Authorize is the one enforcement point: every request through the API
// is classified and put to the Authorizer, after the Authenticator has
// put a principal on the context. A refusal is 403 with the contract's
// problem shape; the handler never runs.
func Authorize(z Authorizer, next http.Handler) http.Handler {
	if z == nil {
		z = AllowAll{}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := z.Authorize(r.Context(), PrincipalFrom(r.Context()), ActionFor(r)); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"problems":[{"message":"` + jsonEscape(err.Error()) + `"}]}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func jsonEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", " ")
	return r.Replace(s)
}
