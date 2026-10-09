package memory_test

import (
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/activity"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/activity/conformance"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/activity/memory"
)

func TestConformance(t *testing.T) {
	t.Parallel()
	conformance.Run(t, func(*testing.T) (activity.Recorder, activity.Reader) {
		s := memory.New()
		return s, s
	})
}
