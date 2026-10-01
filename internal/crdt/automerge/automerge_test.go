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
