package engine_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
)

func TestBootstrapActorAccepted(t *testing.T) {
	t.Parallel()
	e := newTestEngine(t)
	ctx := context.Background()
	y := "apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: t1\n  name: Team One\nspec:\n  name: Team One\n"
	v, err := e.Commit(ctx, "Team", "t1", []byte(y), "anyone-at-all", "bootstrap")
	if err != nil {
		t.Fatalf("expected the bootstrap commit to succeed, got %v", err)
	}
	if v.Number != 1 {
		t.Fatalf("expected version 1, got %d", v.Number)
	}
}

// TestActorAccepted covers I3.2's removal of actor validation: Cartograph is a
// single-person application, so the interface always sends the literal actor
// "local"; checkActor accepts any actor unconditionally.
func TestActorAccepted(t *testing.T) {
	t.Parallel()
	e := seededEngine(t) // commits a Team
	ctx := context.Background()
	y := "apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: t9\n  name: Team Nine\nspec:\n  name: Team Nine\n"
	if _, err := e.Commit(ctx, "Team", "t9", []byte(y), "local", "test"); err != nil {
		t.Fatalf("expected actor \"local\" to be accepted, got %v", err)
	}
}

func TestCommitRejectsIDMismatch(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	ctx := context.Background()
	y := "apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: t9\n  name: Team Nine\nspec:\n  name: Team Nine\n"
	_, err := e.Commit(ctx, "Team", "some-other-id", []byte(y), "p1", "test")
	if err == nil {
		t.Fatal("expected an error for a metadata.id that does not match the path id")
	}
	if !errors.Is(err, engine.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestCommitIncrementsVersion(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	ctx := context.Background()
	y1 := "apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: t9\n  name: Team Nine\nspec:\n  name: Team Nine\n"
	v1, err := e.Commit(ctx, "Team", "t9", []byte(y1), "p1", "create")
	if err != nil {
		t.Fatal(err)
	}
	if v1.Number != 1 {
		t.Fatalf("expected version 1, got %d", v1.Number)
	}
	y2 := "apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: t9\n  name: Team Nine Renamed\nspec:\n  name: Team Nine Renamed\n"
	v2, err := e.Commit(ctx, "Team", "t9", []byte(y2), "p1", "rename")
	if err != nil {
		t.Fatal(err)
	}
	if v2.Number != 2 {
		t.Fatalf("expected version 2, got %d", v2.Number)
	}

	got, err := e.Get(ctx, "Team", "t9")
	if err != nil {
		t.Fatal(err)
	}
	if got.Number != 2 {
		t.Fatalf("expected current version 2, got %d", got.Number)
	}

	versions, err := e.Versions(ctx, "Team", "t9")
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 2 {
		t.Fatalf("expected 2 versions, got %d", len(versions))
	}
}

func TestGetUnknownKindAndNotFound(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	ctx := context.Background()
	if _, err := e.Get(ctx, "Widget", "x"); !errors.Is(err, engine.ErrUnknownKind) {
		t.Fatalf("expected ErrUnknownKind, got %v", err)
	}
	if _, err := e.Get(ctx, "Team", "does-not-exist"); !errors.Is(err, engine.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestKindsListsCountsAfterCommit(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	infos := e.Kinds()
	found := map[string]int{}
	for _, ki := range infos {
		found[ki.Kind] = ki.Count
	}
	if found["Team"] != 1 {
		t.Fatalf("expected 1 Team, got %d (all: %+v)", found["Team"], infos)
	}
	if found["KPI"] != 1 {
		t.Fatalf("expected 1 KPI, got %d", found["KPI"])
	}
	if found["Project"] != 0 {
		t.Fatalf("expected 0 Project, got %d", found["Project"])
	}
}

func TestListFilterByQueryAndReference(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	ctx := context.Background()

	y2 := "apiVersion: cartograph/v1\nkind: KPI\nmetadata:\n  id: k9\n  name: Unrelated KPI\nspec:\n  name: Unrelated KPI\n  definition: something else\n  unit: count\n  direction: increase\n  source: d1\n"
	if _, err := e.Commit(ctx, "KPI", "k9", []byte(y2), "p1", "create"); err != nil {
		t.Fatal(err)
	}

	all, err := e.List(ctx, "KPI", engine.Filter{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 KPIs, got %d", len(all))
	}

	byQ, err := e.List(ctx, "KPI", engine.Filter{Q: "unrelated"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(byQ) != 1 || byQ[0].ID != "k9" {
		t.Fatalf("got %+v", byQ)
	}

	byRef, err := e.List(ctx, "KPI", engine.Filter{Refs: []engine.Ref{{Kind: "Goal", ID: "g1-f"}}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(byRef) != 1 || byRef[0].ID != "k1" {
		t.Fatalf("expected only k1 to reference g1-f, got %+v", byRef)
	}
}

// I3a.1 (2026-09-18): List(includeDrafts=true) surfaces a Project that has
// only ever been saved as a draft, never committed.
func TestReferences(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	ctx := context.Background()

	refs, err := e.References(ctx, "KPI", "k1")
	if err != nil {
		t.Fatal(err)
	}
	foundSource := false
	foundGoal := false
	for _, r := range refs.Outgoing {
		if r.Kind == "DataSource" && r.ID == "d1" {
			foundSource = true
		}
		if r.Kind == "Goal" && r.ID == "g1-f" {
			foundGoal = true
		}
	}
	if !foundSource || !foundGoal {
		t.Fatalf("expected outgoing refs to DataSource d1 and Goal g1-f, got %+v", refs.Outgoing)
	}

	incoming, err := e.References(ctx, "Goal", "g1-f")
	if err != nil {
		t.Fatal(err)
	}
	if len(incoming.Incoming) != 1 || incoming.Incoming[0].Kind != "KPI" || incoming.Incoming[0].ID != "k1" {
		t.Fatalf("expected KPI k1 as an incoming reference to Goal g1-f, got %+v", incoming.Incoming)
	}
}
