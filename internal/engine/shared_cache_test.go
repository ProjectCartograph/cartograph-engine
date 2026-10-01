package engine_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	codecyaml "github.com/ProjectCartograph/cartograph-engine/v2/internal/codec/yaml"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/crdt/automerge"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	fanoutmemory "github.com/ProjectCartograph/cartograph-engine/v2/internal/fanout/memory"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store/memory"
)

var amOnce = sync.OnceValues(func() (*automerge.Engine, error) { return automerge.New(1) })

// A replica keeps a bounded number of documents. Going over the bound
// drops the least recently used, and a dropped document comes back
// from the store whole the next time it is needed: memory is a cache,
// never the record.
func TestSharedDocumentCacheIsBounded(t *testing.T) {
	am, err := amOnce()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	e, err := engine.New(memory.NewManifestStore(), memory.NewOperationalStore(), engine.WithCodec(codecyaml.New()),
		engine.WithCRDT(am), engine.WithDocStore(memory.NewDocStore()), engine.WithFanout(fanoutmemory.New()), engine.WithDocCache(2))
	if err != nil {
		t.Fatal(err)
	}
	sh := e.Shared()
	ids := make([]string, 3)
	for i := range ids {
		id := fmt.Sprintf("team-%d", i)
		text := fmt.Sprintf("apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  name: Team %d\nspec:\n  purpose: Purpose %d\n", i, i)
		if err := e.PutWorking(ctx, "Team", id, []byte(text)); err != nil {
			t.Fatal(err)
		}
		if ids[i], err = sh.DocumentFor(ctx, "Team", id); err != nil {
			t.Fatal(err)
		}
		st, err := sh.NewSyncState()
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := sh.Generate(ctx, ids[i], st); err != nil {
			t.Fatal(err)
		}
		st.Close()
	}
	if got := sh.Stats().Cached; got != 2 {
		t.Fatalf("cached %d documents, want the bound of 2", got)
	}
	// The first was the least recently used, so it went; it loads again.
	if err := sh.Reconcile(ctx, "Team", "team-0", map[string]any{
		"apiVersion": "cartograph/v1", "kind": "Team",
		"metadata": map[string]any{"name": "Team 0"}, "spec": map[string]any{"purpose": "Purpose 0, revised"},
	}, "test"); err != nil {
		t.Fatal(err)
	}
	if got := sh.Stats(); got.Cached > 2 || got.Stored == 0 {
		t.Errorf("after reloading an evicted document: %+v", got)
	}

	// A peer syncing the evicted document gets all of it, revision included.
	peer, err := am.New()
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	theirs, _ := sh.NewSyncState()
	ours, _ := am.NewSyncState()
	defer theirs.Close()
	defer ours.Close()
	for round := 0; round < 10; round++ {
		msg, ok, err := sh.Generate(ctx, ids[0], theirs)
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			if err := peer.ReceiveSyncMessage(ours, msg); err != nil {
				t.Fatal(err)
			}
		}
		back, ok2, err := peer.GenerateSyncMessage(ours)
		if err != nil {
			t.Fatal(err)
		}
		if ok2 {
			if _, err := sh.Receive(ctx, ids[0], theirs, back, true); err != nil {
				t.Fatal(err)
			}
		}
		if !ok && !ok2 {
			break
		}
	}
	doc, err := peer.JSON()
	if err != nil {
		t.Fatal(err)
	}
	if got := doc["spec"].(map[string]any)["purpose"]; got != "Purpose 0, revised" {
		t.Errorf("the evicted document synced as purpose %v", got)
	}
}
