package memory_test

import (
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/internal/fanout"
	"github.com/ProjectCartograph/cartograph-engine/internal/fanout/conformance"
	"github.com/ProjectCartograph/cartograph-engine/internal/fanout/memory"
)

func TestConformance(t *testing.T) {
	hub := memory.NewHub()
	conformance.Run(t, func(t *testing.T) fanout.Bus {
		b := hub.Bus()
		t.Cleanup(func() { b.Close() })
		return b
	})
}
