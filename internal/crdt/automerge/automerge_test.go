package automerge_test

import (
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/crdt"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/crdt/automerge"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/crdt/conformance"
)

func TestConformance(t *testing.T) {
	conformance.Run(t, func(t *testing.T) crdt.Engine {
		e, err := automerge.New(2)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { e.Close() })
		return e
	})
}

// A replica may drop a document from its cache and load it again while
// a peer is connected; the reload can land on another module instance.
// The peer's sync state must carry on (afresh, from heads) rather than
// fail the connection.
func TestSyncStateFollowsAReloadedDocument(t *testing.T) {
	e, err := automerge.New(2)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	shape := crdt.Shape{Texts: []string{"/note"}}

	seed, _ := e.New()
	if _, err := seed.Reconcile(map[string]any{"note": "first"}, shape, crdt.Change{}); err != nil {
		t.Fatal(err)
	}
	saved, _ := seed.Save()
	seed.Close()

	server, _ := e.Load(saved) // one instance
	peer, _ := e.New()
	defer peer.Close()
	theirs, _ := e.NewSyncState() // the server's state for the peer
	ours, _ := e.NewSyncState()
	defer theirs.Close()
	defer ours.Close()
	exchange := func(server crdt.Doc) {
		t.Helper()
		for round := 0; round < 20; round++ {
			m1, ok1, err := server.GenerateSyncMessage(theirs)
			if err != nil {
				t.Fatalf("server generate: %v", err)
			}
			if ok1 {
				if err := peer.ReceiveSyncMessage(ours, m1); err != nil {
					t.Fatal(err)
				}
			}
			m2, ok2, err := peer.GenerateSyncMessage(ours)
			if err != nil {
				t.Fatal(err)
			}
			if ok2 {
				if err := server.ReceiveSyncMessage(theirs, m2); err != nil {
					t.Fatalf("server receive: %v", err)
				}
			}
			if !ok1 && !ok2 {
				return
			}
		}
		t.Fatal("no quiescence")
	}
	exchange(server)

	// The replica drops the document and loads it again: round robin puts
	// it on the other instance. Meanwhile the peer types.
	saved, _ = server.Save()
	server.Close()
	// Instances are handed out in turn; with two, one document made in
	// between puts the reload on the instance the server was not on.
	spacer, _ := e.New()
	defer spacer.Close()
	server, _ = e.Load(saved)
	defer server.Close()
	if _, err := peer.Reconcile(map[string]any{"note": "first, then more"}, shape, crdt.Change{}); err != nil {
		t.Fatal(err)
	}
	exchange(server)
	got, _ := server.JSON()
	if got["note"] != "first, then more" {
		t.Errorf("server holds %v after the reload", got)
	}
}
