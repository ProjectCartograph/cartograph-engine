package engine_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/auth"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/auth/access"
	codecyaml "github.com/ProjectCartograph/cartograph-engine/v2/internal/codec/yaml"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store/memory"
)

// The running example's structure: early grades sits under the
// curriculum division, assessment beside it. A long directory name gets
// a numbered id, as the interface gives one.
var directory = engine.Directory{
	Roles: map[string][]string{
		auth.RoleContributor:   {"g-curriculum", "g-early", "g-assessment"},
		auth.RoleAdministrator: {"g-admins"},
	},
	Teams: []engine.DirectoryTeam{
		{Group: "g-curriculum", Name: "Curriculum"},
		{Group: "g-early", Name: "Early grades", Parent: "Curriculum"},
		{Group: "g-assessment", Name: "Assessment"},
		{Group: "g-planning", Name: "Educational Facilities Planning and Procurement Division"},
	},
}

// accessEngine is an engine composed as serve composes it for access by
// role and team, with the mapped teams created.
func accessEngine(t *testing.T) *engine.Engine {
	t.Helper()
	var e *engine.Engine
	policy := access.Policy{Grants: func(ctx context.Context, p auth.Principal) (auth.Grants, error) { return e.Grants(ctx, p) }}
	e, err := engine.New(memory.NewManifestStore(), memory.NewOperationalStore(), engine.WithCodec(codecyaml.New()),
		engine.WithAuthorizer(policy), engine.WithAccess(memory.NewAccessStore(), directory))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.ApplyDirectory(context.Background(), "directory"); err != nil {
		t.Fatal(err)
	}
	return e
}

func as(email string, groups ...string) context.Context {
	return auth.WithPrincipal(context.Background(), auth.Principal{Subject: "sub-" + email, Email: email, Name: strings.Split(email, "@")[0], Roles: groups})
}

func TestApplyDirectoryCreatesTheTeamsOnce(t *testing.T) {
	e := accessEngine(t)
	ctx := context.Background()
	for id, parent := range map[string]string{"curriculum": "", "early-grades": "curriculum", "assessment": "", "team": ""} {
		v, err := e.Get(ctx, "Team", id)
		if err != nil {
			t.Fatalf("team %s: %v", id, err)
		}
		if parent != "" && !strings.Contains(string(v.YAML), "parent: "+parent) {
			t.Errorf("team %s should sit under %s:\n%s", id, parent, v.YAML)
		}
	}
	created, err := e.ApplyDirectory(ctx, "directory")
	if err != nil || len(created) != 0 {
		t.Fatalf("a second apply created %v (%v)", created, err)
	}
	beneath, err := e.TeamsBeneath(ctx, []string{"curriculum"})
	if err != nil || !slices.Equal(beneath, []string{"curriculum", "early-grades"}) {
		t.Fatalf("beneath curriculum: %v, %v", beneath, err)
	}
}

func TestSignInEnrolsFromTheDirectory(t *testing.T) {
	e := accessEngine(t)
	g, err := e.Grants(as("lee@example.org", "g-curriculum", "all-staff"), auth.Principal{Subject: "s", Email: "lee@example.org", Name: "Lee", Roles: []string{"g-curriculum", "all-staff"}})
	if err != nil {
		t.Fatal(err)
	}
	if !g.Listed || !slices.Equal(g.Roles, []string{auth.RoleContributor}) || !slices.Equal(g.Teams, []string{"curriculum"}) {
		t.Fatalf("grants: %+v", g)
	}
	people, err := e.People(context.Background())
	if err != nil || len(people) != 1 || people[0].Name != "Lee" || people[0].AddedBy != "directory" {
		t.Fatalf("enrolled: %+v, %v", people, err)
	}

	// Someone whose groups grant nothing is not enrolled.
	g, err = e.Grants(context.Background(), auth.Principal{Subject: "x", Email: "visitor@example.org", Roles: []string{"all-staff"}})
	if err != nil || g.Listed {
		t.Fatalf("an unmapped person: %+v, %v", g, err)
	}
}

func TestAnAdministratorGrantsBesideTheDirectory(t *testing.T) {
	e := accessEngine(t)
	admin := as("admin@example.org", "g-admins")
	if _, err := e.Grants(admin, auth.PrincipalFrom(admin)); err != nil {
		t.Fatal(err)
	}
	p, err := e.GrantPerson(admin, "Sam@Example.org", []string{auth.RoleReader, auth.RoleStrategyEditor}, []string{"assessment"}, "admin@example.org")
	if err != nil {
		t.Fatal(err)
	}
	if p.Email != "sam@example.org" || !slices.Equal(p.Roles, []string{auth.RoleReader, auth.RoleStrategyEditor}) || p.AddedBy != "admin@example.org" {
		t.Fatalf("granted: %+v", p)
	}
	// Sam's own groups make them a contributor too; the two combine.
	g, err := e.Grants(context.Background(), auth.Principal{Subject: "s2", Email: "sam@example.org", Roles: []string{"g-assessment"}})
	if err != nil || !slices.Equal(g.Roles, []string{auth.RoleReader, auth.RoleContributor, auth.RoleStrategyEditor}) {
		t.Fatalf("combined: %+v, %v", g, err)
	}

	var invalid *engine.ValidationError
	if _, err := e.GrantPerson(admin, "not-an-address", []string{"owner"}, []string{"no-such-team"}, "admin"); !errors.As(err, &invalid) || len(invalid.Problems) != 3 {
		t.Fatalf("bad grant: %v", err)
	}
	if err := e.RemovePerson(admin, "admin@example.org"); !errors.Is(err, engine.ErrSelf) {
		t.Fatalf("removing yourself: %v", err)
	}
	if err := e.RemovePerson(admin, "sam@example.org"); err != nil {
		t.Fatal(err)
	}
}

func TestAContributorChangesOnlyTheirTeamsWork(t *testing.T) {
	e := accessEngine(t)
	system := context.Background()
	for id, team := range map[string]string{"coach-teachers": "early-grades", "grade-two-check": "assessment"} {
		if err := e.PutWorking(system, "Project", id, project(id, team)); err != nil {
			t.Fatal(err)
		}
	}
	lee := as("lee@example.org", "g-curriculum")

	// A project of a team beneath the curriculum division.
	if err := e.PutWorking(lee, "Project", "coach-teachers", project("coach-teachers", "early-grades")); err != nil {
		t.Fatalf("own team's project: %v", err)
	}
	// Another team's project, and moving their own out of reach.
	if err := e.PutWorking(lee, "Project", "grade-two-check", project("grade-two-check", "assessment")); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("another team's project: %v", err)
	}
	if err := e.PutWorking(lee, "Project", "coach-teachers", project("coach-teachers", "assessment")); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("moving it to another team: %v", err)
	}
	// A new project for their own team.
	if err := e.PutWorking(lee, "Project", "reading-clubs", project("reading-clubs", "curriculum")); err != nil {
		t.Fatalf("a new project of their own: %v", err)
	}
	// A state move is a change to the project too.
	if _, err := e.TransitionProjectState(lee, "grade-two-check", "cancelled", "lee@example.org", "not ours"); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("moving another team's project: %v", err)
	}
	// Teams are an administrator's.
	if err := e.PutWorking(lee, "Team", "assessment", []byte("apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: assessment\n  name: Assessment\nspec:\n  name: Assessment\n  parent: curriculum\n")); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("changing a team: %v", err)
	}
	// Someone not on the list changes nothing.
	if err := e.PutWorking(as("visitor@example.org", "all-staff"), "Project", "coach-teachers", project("coach-teachers", "early-grades")); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("an unlisted person: %v", err)
	}
}

func TestTheEnginesOwnWorkIsNotAsked(t *testing.T) {
	e := accessEngine(t)
	// No principal on the context: seeding, an import at start, a
	// refresh another replica asked for.
	if err := e.PutWorking(context.Background(), "Team", "assessment", []byte("apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: assessment\n  name: Assessment\nspec:\n  name: Assessment\n")); err != nil {
		t.Fatal(err)
	}
}
