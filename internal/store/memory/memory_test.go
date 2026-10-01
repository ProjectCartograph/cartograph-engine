package memory_test

import (
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/internal/store"
	"github.com/ProjectCartograph/cartograph-engine/internal/store/conformance"
	"github.com/ProjectCartograph/cartograph-engine/internal/store/memory"
)

func TestManifestStoreConformance(t *testing.T) {
	conformance.RunManifestStore(t, func(t *testing.T) store.ManifestStore {
		return memory.NewManifestStore()
	})
}

func TestOperationalStoreConformance(t *testing.T) {
	conformance.RunOperationalStore(t, func(t *testing.T) store.OperationalStore {
		return memory.NewOperationalStore()
	})
}

func TestOpLogConformance(t *testing.T) {
	conformance.RunOpLog(t, func(t *testing.T) store.OpLog { return memory.NewOpLog() })
}
