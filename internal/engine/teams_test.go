package engine_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/auth"
	codecyaml "github.com/ProjectCartograph/cartograph-engine/v2/internal/codec/yaml"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store/memory"
)

// recorder is an authorizer that keeps every action it is asked about
// and refuses those refuse says to.
type recorder struct {
	seen   []auth.Action
	refuse func(auth.Action) bool
}

func (r *recorder) Authorize(_ context.Context, _ auth.Principal, a auth.Action) error {
	r.seen = append(r.seen, a)
	if r.refuse != nil && r.refuse(a) {
		return auth.ErrForbidden
	}
	return nil
}

func project(id, team string) []byte {
	return []byte("apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: " + id + "\n  name: " + id + "\nspec:\n  team: " + team + "\n")
}

func team(id, parent string) []byte {
	text := "apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: " + id + "\n  name: " + id + "\nspec:\n  name: " + id + "\n"
	if parent != "" {
		text += "  parent: " + parent + "\n"
	}
	return []byte(text)
}

// The engine tells the authorizer what each write does to the manifest:
// the chains of teams it sits under before and after, nearest first, and
// the spec fields it changes. Its own work, with nobody on the context,
// is not asked about.
func TestEveryWriteIsAskedWithItsTeams(t *testing.T) {
	t.Parallel()
	r := &recorder{}
	e, err := engine.New(memory.NewManifestStore(), memory.NewOperationalStore(), engine.WithCodec(codecyaml.New()), engine.WithAuthorizer(r))
	if err != nil {
		t.Fatal(err)
	}
	system := context.Background()
	for _, tm := range [][2]string{{"curriculum", ""}, {"early-grades", "curriculum"}, {"assessment", ""}} {
		if _, err := e.Commit(system, "Team", tm[0], team(tm[0], tm[1]), "directory", "set up"); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.PutWorking(system, "Project", "coach", project("coach", "early-grades")); err != nil {
		t.Fatal(err)
	}
	if len(r.seen) != 0 {
		t.Fatalf("the engine's own work was put to the authorizer: %+v", r.seen)
	}

	lee := auth.WithPrincipal(context.Background(), auth.Principal{Subject: "lee"})
	if err := e.PutWorking(lee, "Project", "coach", project("coach", "assessment")); err != nil {
		t.Fatal(err)
	}
	if len(r.seen) != 1 {
		t.Fatalf("asked %d times", len(r.seen))
	}
	a := r.seen[0]
	if a.Verb != auth.VerbWrite || a.Kind != "Project" || a.ID != "coach" || a.Change == nil {
		t.Fatalf("action: %+v", a)
	}
	if !slices.Equal(a.Change.TeamsBefore, []string{"early-grades", "curriculum"}) || !slices.Equal(a.Change.TeamsAfter, []string{"assessment"}) {
		t.Fatalf("chains: %+v", a.Change)
	}
	if !slices.Equal(a.Change.Fields, []string{"team"}) {
		t.Fatalf("fields: %v", a.Change.Fields)
	}

	// A refusal stops the write.
	r.refuse = func(auth.Action) bool { return true }
	if err := e.PutWorking(lee, "Project", "coach", project("coach", "curriculum")); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("refused write: %v", err)
	}
	text, _, _ := e.GetWorking(system, "Project", "coach")
	if string(text) != string(project("coach", "assessment")) {
		t.Fatalf("a refused write changed the working copy:\n%s", text)
	}
}

func TestTeamsBeneath(t *testing.T) {
	t.Parallel()
	e := newTestEngine(t)
	ctx := context.Background()
	for _, tm := range [][2]string{{"curriculum", ""}, {"early-grades", "curriculum"}, {"reading", "early-grades"}, {"assessment", ""}} {
		if _, err := e.Commit(ctx, "Team", tm[0], team(tm[0], tm[1]), "directory", "set up"); err != nil {
			t.Fatal(err)
		}
	}
	got, err := e.TeamsBeneath(ctx, []string{"curriculum"})
	if err != nil || !slices.Equal(got, []string{"curriculum", "early-grades", "reading"}) {
		t.Fatalf("beneath curriculum: %v, %v", got, err)
	}
}
