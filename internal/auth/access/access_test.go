package access_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/auth"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/auth/access"
)

// The running example's teams: early grades sits under the curriculum
// division; assessment is beside it.
const (
	curriculum  = "team-curriculum"
	earlyGrades = "team-early-grades"
	assessment  = "team-assessment"
)

func policy(g auth.Grants) access.Policy {
	return access.Policy{Grants: func(context.Context, auth.Principal) (auth.Grants, error) { return g, nil }}
}

var someone = auth.Principal{Subject: "sub", Email: "lee@example.org"}

func write(kind string, c *auth.Change) auth.Action {
	return auth.Action{Verb: auth.VerbWrite, Kind: kind, ID: "x", Change: c}
}

func chains(before, after []string) *auth.Change {
	return &auth.Change{TeamsBefore: before, TeamsAfter: after}
}

func TestRoles(t *testing.T) {
	reader := auth.Grants{Listed: true, Roles: []string{auth.RoleReader}}
	curriculumContributor := auth.Grants{Listed: true, Roles: []string{auth.RoleContributor}, Teams: []string{curriculum}}
	strategy := auth.Grants{Listed: true, Roles: []string{auth.RoleStrategyEditor}}
	admin := auth.Grants{Listed: true, Roles: []string{auth.RoleAdministrator}}
	coached := []string{earlyGrades, curriculum} // a project the early grades team runs
	assessed := []string{assessment}

	cases := []struct {
		name  string
		g     auth.Grants
		a     auth.Action
		allow bool
	}{
		{"a reader reads", reader, auth.Action{Verb: auth.VerbRead, Kind: "Project"}, true},
		{"a reader writes nothing", reader, write("Project", nil), false},
		{"a reader may not read the access list", reader, auth.Action{Verb: auth.VerbRead, Resource: auth.ResourceAccess}, false},
		{"an administrator reads the access list", admin, auth.Action{Verb: auth.VerbRead, Resource: auth.ResourceAccess}, true},
		{"anyone asks who they are", auth.Grants{}, auth.Action{Verb: auth.VerbRead, Resource: auth.ResourceSession}, true},

		{"a contributor edits a project of a team beneath theirs", curriculumContributor, write("Project", chains(coached, coached)), true},
		{"a contributor may not edit another team's project", curriculumContributor, write("Project", chains(assessed, assessed)), false},
		{"a contributor may not move their project to another team", curriculumContributor, write("Project", chains(coached, assessed)), false},
		{"a contributor may not take another team's project", curriculumContributor, write("Project", chains(assessed, coached)), false},
		{"a contributor starts a project with no team yet", curriculumContributor, write("Project", chains(nil, nil)), true},
		{"a contributor gives a new project to their team", curriculumContributor, write("Programme", chains(nil, []string{curriculum})), true},
		{"the HTTP layer lets a contributor through to the engine", curriculumContributor, write("Operation", nil), true},
		{"a contributor keeps the shared registers", curriculumContributor, write("KPIReadings", nil), true},
		{"a contributor may not change a goal", curriculumContributor, write("Goal", nil), false},
		{"a contributor may not change a team", curriculumContributor, write("Team", nil), false},

		{"a strategy editor changes a goal", strategy, write("Goal", nil), true},
		{"a strategy editor changes the vision and mission", strategy, write("Settings", &auth.Change{Fields: []string{"purpose"}}), true},
		{"a strategy editor may not change the printer", strategy, write("Settings", &auth.Change{Fields: []string{"chromium", "purpose"}}), false},
		{"a strategy editor may not change a project", strategy, write("Project", nil), false},

		{"an administrator changes any team's work", admin, write("Project", chains(assessed, coached)), true},
		{"an administrator changes a team", admin, write("Team", nil), true},
		{"an administrator manages the access list", admin, auth.Action{Verb: auth.VerbWrite, Resource: auth.ResourceAccess}, true},
		{"only an administrator applies the vault", curriculumContributor, auth.Action{Verb: auth.VerbWrite, Resource: auth.ResourceVault}, false},
		{"an unknown kind is an administrator's", curriculumContributor, write("Something", nil), false},

		{"an unlisted person reads nothing", auth.Grants{}, auth.Action{Verb: auth.VerbRead, Kind: "Goal"}, false},
		{"a listed person with no role reads nothing", auth.Grants{Listed: true}, auth.Action{Verb: auth.VerbRead, Kind: "Goal"}, false},
	}
	for _, c := range cases {
		err := policy(c.g).Authorize(context.Background(), someone, c.a)
		if c.allow && err != nil {
			t.Errorf("%s: refused: %v", c.name, err)
		}
		if !c.allow && !errors.Is(err, auth.ErrForbidden) {
			t.Errorf("%s: allowed (err %v)", c.name, err)
		}
	}
}

func TestRolesCombine(t *testing.T) {
	g := auth.Grants{Listed: true, Roles: []string{auth.RoleContributor, auth.RoleStrategyEditor}, Teams: []string{assessment}}
	for _, kind := range []string{"Goal", "KPI", "Operation"} {
		if err := policy(g).Authorize(context.Background(), someone, write(kind, nil)); err != nil {
			t.Errorf("%s: %v", kind, err)
		}
	}
}

func TestAnonymousIsRefused(t *testing.T) {
	admin := auth.Grants{Listed: true, Roles: []string{auth.RoleAdministrator}}
	if err := policy(admin).Authorize(context.Background(), auth.Anonymous, auth.Action{Verb: auth.VerbRead}); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("anonymous: %v", err)
	}
}

func TestScope(t *testing.T) {
	cases := []struct {
		roles []string
		kind  string
		want  auth.Scope
	}{
		{[]string{auth.RoleReader}, "Project", auth.ScopeNone},
		{[]string{auth.RoleContributor}, "Project", auth.ScopeTeams},
		{[]string{auth.RoleContributor}, "Gap", auth.ScopeAll},
		{[]string{auth.RoleContributor}, "Goal", auth.ScopeNone},
		{[]string{auth.RoleStrategyEditor}, "Goal", auth.ScopeAll},
		{[]string{auth.RoleStrategyEditor}, "Team", auth.ScopeNone},
		{[]string{auth.RoleAdministrator}, "Team", auth.ScopeAll},
	}
	for _, c := range cases {
		got, err := policy(auth.Grants{Listed: true, Roles: c.roles}).Scope(context.Background(), someone, c.kind)
		if err != nil || got != c.want {
			t.Errorf("%v on %s: got %s, %v; want %s", c.roles, c.kind, got, err, c.want)
		}
	}
	// Someone not on the list may write nothing, and that is no error.
	got, err := policy(auth.Grants{}).Scope(context.Background(), someone, "Project")
	if err != nil || got != auth.ScopeNone {
		t.Fatalf("unlisted: %s, %v", got, err)
	}
}
