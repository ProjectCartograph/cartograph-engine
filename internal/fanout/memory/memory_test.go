package memory_test

import (
	"testing"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/fanout"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/fanout/conformance"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/fanout/memory"
)

func TestConformance(t *testing.T) {
	hub := memory.NewHub()
	// Delivery happens before Publish returns: a moment proves nothing
	// came.
	conformance.RunWith(t, func(t *testing.T) fanout.Bus {
		b := hub.Bus()
		t.Cleanup(func() { b.Close() })
		return b
	}, conformance.Options{Quiet: 10 * time.Millisecond})
}
