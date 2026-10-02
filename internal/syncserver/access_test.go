package syncserver_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/auth"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/auth/access"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/codec/yaml"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/crdt"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	fanoutmemory "github.com/ProjectCartograph/cartograph-engine/v2/internal/fanout/memory"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store/memory"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/syncserver"
)

// A contributor's edit on the sync socket is decided by the team the
// manifest belongs to, as a save is: their own team's project takes it,
// another team's refuses it, and the refused change is never stored.
func TestTeamsDecideEditsOnTheSocket(t *testing.T) {
	ctx := context.Background()
	var e *engine.Engine
	policy := access.Policy{Grants: func(ctx context.Context, p auth.Principal) (auth.Grants, error) { return e.Grants(ctx, p) }}
	bus := fanoutmemory.New()
	e, err := engine.New(memory.NewManifestStore(), memory.NewOperationalStore(), engine.WithCodec(yaml.New()),
		engine.WithCRDT(am), engine.WithDocStore(memory.NewDocStore()), engine.WithFanout(bus),
		engine.WithAuthorizer(policy), engine.WithAccess(memory.NewAccessStore(), engine.Directory{
			Roles: map[string][]string{auth.RoleContributor: {"g-curriculum", "g-assessment"}},
			Teams: []engine.DirectoryTeam{{Group: "g-curriculum", Name: "Curriculum"}, {Group: "g-assessment", Name: "Assessment"}},
		}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.ApplyDirectory(ctx, "directory"); err != nil {
		t.Fatal(err)
	}
	for id, team := range map[string]string{"coach-teachers": "curriculum", "grade-two-check": "assessment"} {
		text := "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: " + id + "\n  name: " + id + "\nspec:\n  team: " + team + "\n  purpose: As planned\n"
		if err := e.PutWorking(ctx, "Project", id, []byte(text)); err != nil {
			t.Fatal(err)
		}
	}
	// Every connection acts as a contributor in the curriculum division,
	// as the authentication middleware would put them on the request.
	lee := auth.Principal{Subject: "sub-lee", Email: "lee@example.org", Name: "Lee", Roles: []string{"g-curriculum"}}
	inner := syncserver.New(e.Shared(), bus, policy, slog.New(slog.NewTextHandler(nilWriter{}, nil)))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inner.ServeHTTP(w, r.WithContext(auth.WithPrincipal(r.Context(), lee)))
	}))
	t.Cleanup(func() { srv.Close(); bus.Close() })

	edit := func(id string) *peer {
		docID, err := e.Shared().DocumentFor(ctx, "Project", id)
		if err != nil {
			t.Fatal(err)
		}
		p := connect(t, srv, "lee", docID)
		p.until(func() bool { return p.field("spec", "purpose") == "As planned" })
		j, _ := p.doc.JSON()
		j["spec"].(map[string]any)["purpose"] = "Rewritten by Lee"
		if _, err := p.doc.Reconcile(j, e.Shape("Project"), crdt.Change{}); err != nil {
			t.Fatal(err)
		}
		return p
	}

	own := edit("coach-teachers")
	own.until(func() bool {
		text, _, _ := e.GetWorking(ctx, "Project", "coach-teachers")
		return strings.Contains(string(text), "Rewritten by Lee")
	})

	theirs := edit("grade-two-check")
	theirs.offer()
	for {
		m := theirs.read()
		if m.Type == "error" {
			break
		}
		if m.Type == "sync" {
			if err := theirs.doc.ReceiveSyncMessage(theirs.st, m.Data); err != nil {
				t.Fatal(err)
			}
			theirs.offer()
		}
	}
	text, _, _ := e.GetWorking(ctx, "Project", "grade-two-check")
	if strings.Contains(string(text), "Rewritten by Lee") {
		t.Fatal("another team's project took the edit")
	}
	// A fresh connection reads the draft as the store has it.
	docID, _ := e.Shared().DocumentFor(ctx, "Project", "grade-two-check")
	fresh := connect(t, srv, "lee-again", docID)
	fresh.until(func() bool { return fresh.field("spec", "purpose") != nil })
	if got := fresh.field("spec", "purpose"); got != "As planned" {
		t.Fatalf("the refused change reached the shared draft: %v", got)
	}
}

type nilWriter struct{}

func (nilWriter) Write(b []byte) (int, error) { return len(b), nil }
