package engine

import (
	"context"
	"strings"
	"testing"

	codecyaml "github.com/ProjectCartograph/cartograph-engine/v2/internal/codec/yaml"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/crdt"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/crdt/automerge"
	fanoutmemory "github.com/ProjectCartograph/cartograph-engine/v2/internal/fanout/memory"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/identity"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store/memory"
)

// A change set's draft of a manifest is a live document of its own: an
// edit in the document lands in the change set's item, never the
// working copy, and an edit to the item reaches the document
// (docs/adr/0024).
func TestAChangeSetsDraftIsLive(t *testing.T) {
	am, err := automerge.New(1)
	if err != nil {
		t.Fatal(err)
	}
	ms := memory.NewManifestStore()
	e, err := New(ms, memory.NewOperationalStore(), WithCodec(codecyaml.New()),
		WithCRDT(am), WithDocStore(memory.NewDocStore()), WithFanout(fanoutmemory.New()))
	if err != nil {
		t.Fatal(err)
	}
	ctx := identity.WithPrincipal(context.Background(), identity.Principal{Subject: "ada@example.org", Email: "ada@example.org", Name: "Ada"})
	team := "apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: t1\n  name: Team One\nspec:\n  description: As it is\n"
	if _, err := e.Commit(ctx, "Team", "t1", []byte(team), "ada", "seed"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.EditInChangeSet(ctx, "", "Team", "t1", map[string]any{"/spec/description": "In the change set"}, nil); err != nil {
		t.Fatal(err)
	}
	cs, _ := e.WorkingChangeSet(ctx, "")
	sh := e.Shared()
	docID, err := sh.DocumentInSet(ctx, cs.ID, "Team", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if own, _ := sh.DocumentFor(ctx, "Team", "t1"); own == docID {
		t.Fatal("the change set's draft is the manifest's own")
	}

	// An edit in the document, as a peer's sync would make it.
	sd, err := sh.load(ctx, docID)
	if err != nil {
		t.Fatal(err)
	}
	doc, _ := sd.doc.JSON()
	doc["spec"].(map[string]any)["description"] = "Edited live"
	if _, err := sd.doc.Reconcile(doc, e.Shape("Team"), crdt.Change{Message: "peer"}); err != nil {
		t.Fatal(err)
	}
	err = sh.persist(ctx, docID, sd)
	sd.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if text, _, _ := e.ChangeSetText(ctx, cs.ID, "Team", "t1"); !strings.Contains(string(text), "Edited live") {
		t.Fatalf("the item after a live edit: %s", text)
	}
	if text, found, _ := ms.GetWorking(ctx, "Team", "t1"); found && strings.Contains(string(text), "Edited live") {
		t.Fatal("a change set's edit reached the working copy")
	}

	// An edit to the item, as an agent makes it, reaches the document.
	if _, err := e.EditInChangeSet(ctx, cs.ID, "Team", "t1", map[string]any{"/spec/description": "From the agent"}, nil); err != nil {
		t.Fatal(err)
	}
	sd, err = sh.load(ctx, docID)
	if err != nil {
		t.Fatal(err)
	}
	doc, _ = sd.doc.JSON()
	sd.mu.Unlock()
	if doc["spec"].(map[string]any)["description"] != "From the agent" {
		t.Fatalf("the document after an item edit: %v", doc)
	}
}
