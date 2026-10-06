package engine_test

import (
	"context"
	"errors"
	codecyaml "github.com/ProjectCartograph/cartograph-engine/v2/internal/codec/yaml"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store/memory"
)

// A store with no apply gate answers ErrNoState on every state method,
// and the API turns that into an empty list or a refusal. The memory
// store is such a store.
func TestStateMethodsRefuseAStoreWithoutAnApplyGate(t *testing.T) {
	t.Parallel()
	e, err := engine.New(memory.NewManifestStore(), memory.NewOperationalStore(), engine.WithCodec(codecyaml.New()))
	if err != nil {
		t.Fatal(err)
	}
	if e.HasState() {
		t.Fatal("the memory store has no apply gate")
	}
	ctx := context.Background()
	if _, err := e.GetState(ctx); !errors.Is(err, engine.ErrNoState) {
		t.Fatalf("GetState: %v", err)
	}
	if _, err := e.ListUnapplied(ctx); !errors.Is(err, engine.ErrNoState) {
		t.Fatalf("ListUnapplied: %v", err)
	}
	if _, err := e.Apply(ctx, []string{"Team/t1"}); !errors.Is(err, engine.ErrNoState) {
		t.Fatalf("Apply: %v", err)
	}
	if _, err := e.Recover(ctx, "Team/t1", "a", ""); !errors.Is(err, engine.ErrNoState) {
		t.Fatalf("Recover: %v", err)
	}
}

// A handoff needs somewhere to keep the bundle; the memory bundle store
// is enough, so a handoff can be tested without a directory.
func TestHandoffUsesTheBundleStore(t *testing.T) {
	t.Parallel()
	bundles := memory.NewBundleStore()
	e, err := engine.New(memory.NewManifestStore(), memory.NewOperationalStore(), engine.WithBundles(bundles), engine.WithCodec(codecyaml.New()))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := e.Handoff(ctx, "missing", "a", engine.HandoffRequest{}); !errors.Is(err, engine.ErrNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}
