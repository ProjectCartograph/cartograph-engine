package memory_test

import (
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store/conformance"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store/memory"
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

func TestDocStoreConformance(t *testing.T) {
	conformance.RunDocStore(t, func(t *testing.T) store.DocStore { return memory.NewDocStore() })
}

func TestAccessStoreConformance(t *testing.T) {
	conformance.RunAccessStore(t, func(t *testing.T) store.AccessStore { return memory.NewAccessStore() })
}
