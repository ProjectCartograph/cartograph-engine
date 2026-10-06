package engine_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/identity"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

var (
	adasOther = identity.Principal{Subject: "ada@example.org", Email: "ada@example.org", Name: "Ada", Agent: "Claude", Grant: "g2"}
	sam       = identity.Principal{Subject: "sam@example.org", Email: "sam@example.org", Name: "Sam"}
)

// Two agents of one person keep their work apart, even on one manifest;
// the person reviews one change set at a time, trims what is not ready,
// and accepts the rest whole, as a pull request is merged.
func TestChangeSetsKeepWorkApartAndMergeWhole(t *testing.T) {
	t.Parallel()
	e := newTestEngine(t)
	seed := actingAs(ada)
	if _, err := e.Commit(seed, "Team", "t1", []byte(fmt.Sprintf(teamText, "first")), "ada@example.org", "seed"); err != nil {
		t.Fatal(err)
	}
	one := actingAs(identity.Principal{Subject: "ada@example.org", Email: "ada@example.org", Name: "Ada", Agent: "Claude", Grant: "g1"})
	two := actingAs(adasOther)

	if _, err := e.EditInChangeSet(one, "", "Team", "t1", map[string]any{"/spec/description": "from the first agent"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := e.EditInChangeSet(two, "", "Team", "t1", map[string]any{"/spec/description": "from the second agent"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := e.EditInChangeSet(one, "", "Team", "t2", map[string]any{"/metadata/name": "Team Two", "/spec/description": "new"}, nil); err != nil {
		t.Fatal(err)
	}
	mine, _ := e.WorkingChangeSet(one, "")
	theirs, _ := e.WorkingChangeSet(two, "")
	if mine.ID == theirs.ID {
		t.Fatal("two agents share one change set")
	}
	if text, _, _ := e.ChangeSetText(one, mine.ID, "Team", "t1"); !strings.Contains(string(text), "first agent") {
		t.Fatalf("the first agent's draft: %s", text)
	}
	if text, _, _ := e.ChangeSetText(two, theirs.ID, "Team", "t1"); !strings.Contains(string(text), "second agent") {
		t.Fatalf("the second agent's draft: %s", text)
	}
	if v, _ := e.Get(seed, "Team", "t1"); !strings.Contains(string(v.YAML), "first\n") {
		t.Fatalf("the record changed before anyone accepted: %s", v.YAML)
	}
	// Nobody else works in it.
	if _, err := e.EditInChangeSet(actingAs(sam), mine.ID, "Team", "t1", map[string]any{"/spec/description": "x"}, nil); !errors.Is(err, engine.ErrNotTheirChangeSet) {
		t.Fatalf("someone else edited it: %v", err)
	}

	view, err := e.ViewChangeSet(seed, mine.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Items) != 2 || len(view.Items[0].Changes) == 0 {
		t.Fatalf("the review: %+v", view.Items)
	}

	// The person trims the new team, and the agent proposes the rest.
	if err := e.IncludeChangeItem(seed, mine.ID, "Team", "t2", false); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ProposeChangeSet(one, mine.ID, "describe the team", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := e.AcceptChangeSet(one, mine.ID, ""); err == nil {
		t.Fatal("an agent accepted its own work")
	}
	if _, err := e.AcceptChangeSet(actingAs(sam), mine.ID, ""); !errors.Is(err, engine.ErrNotTheirChangeSet) {
		t.Fatalf("someone else accepted it: %v", err)
	}
	saved, err := e.AcceptChangeSet(seed, mine.ID, "looks right")
	if err != nil {
		t.Fatal(err)
	}
	if len(saved) != 1 || saved[0].ID != "t1" {
		t.Fatalf("saved %+v, want only the included team", saved)
	}
	if v, _ := e.Get(seed, "Team", "t1"); !strings.Contains(string(v.YAML), "first agent") {
		t.Fatalf("the record after accepting: %s", v.YAML)
	}
	// What was trimmed is still there, and the change set is open for it.
	after, _ := e.ViewChangeSet(seed, mine.ID)
	if after.ChangeSet.Status != store.ChangeSetOpen || len(after.Items) != 1 || after.Items[0].Item.ID != "t2" {
		t.Fatalf("after accepting: %s, %+v", after.ChangeSet.Status, after.Items)
	}
	// Accepting again is refused: it is no longer proposed.
	if _, err := e.AcceptChangeSet(seed, mine.ID, ""); err == nil {
		t.Fatal("accepted twice")
	}

	// The second agent's draft started from version 1; version 2 is saved
	// now, so accepting it would undo the first agent's work unseen.
	if _, err := e.ProposeChangeSet(two, theirs.ID, "", nil); err != nil {
		t.Fatal(err)
	}
	if view, _ := e.ViewChangeSet(seed, theirs.ID); view.Items[0].Stale != 2 {
		t.Fatalf("the review does not say the record moved on: %+v", view.Items[0])
	}
	if _, err := e.AcceptChangeSet(seed, theirs.ID, ""); !errors.Is(err, engine.ErrProposalStale) {
		t.Fatalf("a stale change set was accepted: %v", err)
	}
	if cs, _ := e.ViewChangeSet(seed, theirs.ID); cs.ChangeSet.Status != store.ChangeSetProposed {
		t.Fatalf("a refused acceptance left it %s", cs.ChangeSet.Status)
	}
	if _, err := e.CloseChangeSet(seed, theirs.ID, "superseded"); err != nil {
		t.Fatal(err)
	}
}
