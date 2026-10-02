package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/api"
	apigen "github.com/ProjectCartograph/cartograph-engine/v2/internal/api/gen"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/auth"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/auth/access"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/auth/proxy"
	codecyaml "github.com/ProjectCartograph/cartograph-engine/v2/internal/codec/yaml"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store/memory"
)

// accessServer is the API as serve composes it for access by role and
// team: the proxy's headers, the access policy at the middleware and in
// the engine, two teams and a project of each.
func accessServer(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	var e *engine.Engine
	policy := access.Policy{Grants: func(ctx context.Context, p auth.Principal) (auth.Grants, error) { return e.Grants(ctx, p) }}
	e, err := engine.New(memory.NewManifestStore(), memory.NewOperationalStore(), engine.WithCodec(codecyaml.New()),
		engine.WithAuthorizer(policy), engine.WithAccess(memory.NewAccessStore(), engine.Directory{
			Roles: map[string][]string{
				auth.RoleContributor:   {"g-curriculum", "g-assessment"},
				auth.RoleAdministrator: {"g-admins"},
			},
			Teams: []engine.DirectoryTeam{{Group: "g-curriculum", Name: "Curriculum"}, {Group: "g-early", Name: "Early grades", Parent: "Curriculum"}, {Group: "g-assessment", Name: "Assessment"}},
		}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.ApplyDirectory(ctx, "directory"); err != nil {
		t.Fatal(err)
	}
	for id, team := range map[string]string{"coach-teachers": "early-grades", "grade-two-check": "assessment"} {
		if err := e.PutWorking(ctx, "Project", id, projectYAML(id, team)); err != nil {
			t.Fatal(err)
		}
	}
	h := auth.Middleware(proxy.New("X-Forwarded-User"), auth.Authorize(policy, api.New(e, api.Deps{Authorizer: policy})))
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv.URL
}

func projectYAML(id, team string) []byte {
	return []byte("apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: " + id + "\n  name: " + id + "\nspec:\n  team: " + team + "\n")
}

func person(email string, groups string) map[string]string {
	return map[string]string{"X-Forwarded-User": "sub-" + email, "X-Forwarded-Email": email, "X-Forwarded-Preferred-Username": email, "X-Forwarded-Groups": groups}
}

func TestTheSessionSaysWhatTheAccessListGives(t *testing.T) {
	base := accessServer(t)
	s := decode[apigen.Session](t, doJSON(t, "GET", base+"/session", nil, person("lee@example.org", "g-curriculum")))
	if s.Access == nil || !s.Access.Listed || s.Email == nil || *s.Email != "lee@example.org" {
		t.Fatalf("session: %+v", s)
	}
	if !slices.Equal(s.Access.Roles, []apigen.Role{apigen.Contributor}) || !slices.Equal(s.Access.Teams, []string{"curriculum"}) || !slices.Equal(s.Access.Reach, []string{"curriculum", "early-grades"}) {
		t.Fatalf("access: %+v", *s.Access)
	}
	if s.Access.Scopes["Project"] != apigen.Teams || s.Access.Scopes["Gap"] != apigen.All || s.Access.Scopes["Goal"] != apigen.None || !s.CanWrite {
		t.Fatalf("scopes: %v (canWrite %v)", s.Access.Scopes, s.CanWrite)
	}

	// Someone not on the list still learns why they see nothing.
	s = decode[apigen.Session](t, doJSON(t, "GET", base+"/session", nil, person("visitor@example.org", "all-staff")))
	if s.Access == nil || s.Access.Listed || s.CanWrite {
		t.Fatalf("an unlisted session: %+v", s)
	}
	if resp := doJSON(t, "GET", base+"/manifests/Project/coach-teachers", nil, person("visitor@example.org", "all-staff")); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("an unlisted read: %d", resp.StatusCode)
	}
}

func TestAContributorIsRefusedAnotherTeamsWork(t *testing.T) {
	base := accessServer(t)
	lee := person("lee@example.org", "g-curriculum")
	if resp := doJSON(t, "PUT", base+"/manifests/Project/coach-teachers/working", map[string]string{"yaml": string(projectYAML("coach-teachers", "early-grades"))}, lee); resp.StatusCode/100 != 2 {
		t.Fatalf("their own team's project: %d", resp.StatusCode)
	}
	resp := doJSON(t, "PUT", base+"/manifests/Project/grade-two-check/working", map[string]string{"yaml": string(projectYAML("grade-two-check", "assessment"))}, lee)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("another team's project: %d", resp.StatusCode)
	}
	if p := decode[apigen.ProblemList](t, resp); len(p.Problems) != 1 || p.Problems[0].Message == "" {
		t.Fatalf("the refusal says why: %+v", p)
	}
	if resp := doJSON(t, "GET", base+"/access/people", nil, lee); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("a contributor reading the access list: %d", resp.StatusCode)
	}
}

func TestAnAdministratorManagesTheList(t *testing.T) {
	base := accessServer(t)
	admin := person("admin@example.org", "g-admins")
	resp := doJSON(t, "PUT", base+"/access/people/Sam@Example.org", map[string]any{"roles": []string{"reader", "contributor"}, "teams": []string{"assessment"}}, admin)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("grant: %d", resp.StatusCode)
	}
	if p := decode[apigen.Person](t, resp); p.Email != "sam@example.org" || len(p.Roles) != 2 || p.AddedBy != "sub-admin@example.org" || p.LastSignedIn != nil {
		t.Fatalf("granted: %+v", p)
	}
	if resp := doJSON(t, "PUT", base+"/access/people/sam@example.org", map[string]any{"roles": []string{"owner"}, "teams": []string{}}, admin); resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("an unknown role: %d", resp.StatusCode)
	}
	list := decode[struct{ People []apigen.Person }](t, doJSON(t, "GET", base+"/access/people", nil, admin))
	if len(list.People) != 2 || list.People[0].Email != "admin@example.org" || list.People[1].Email != "sam@example.org" {
		t.Fatalf("listed: %+v", list.People)
	}
	if resp := doJSON(t, "DELETE", base+"/access/people/admin@example.org", nil, admin); resp.StatusCode != http.StatusConflict {
		t.Fatalf("removing yourself: %d", resp.StatusCode)
	}
	if resp := doJSON(t, "DELETE", base+"/access/people/sam@example.org", nil, admin); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("remove: %d", resp.StatusCode)
	}
}
