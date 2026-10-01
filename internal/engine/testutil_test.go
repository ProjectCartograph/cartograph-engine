package engine_test

import (
	codecyaml "github.com/ProjectCartograph/cartograph-engine/internal/codec/yaml"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/internal/store/memory"
)

func newTestEngine(t *testing.T) *engine.Engine {
	t.Helper()
	e, err := engine.New(memory.NewManifestStore(), memory.NewOperationalStore(), engine.WithCodec(codecyaml.New()))
	if err != nil {
		t.Fatal(err)
	}
	return e
}
