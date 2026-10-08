package fake_test

import (
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/decide/conformance"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/decide/fake"
)

func TestConformance(t *testing.T) {
	t.Parallel()
	conformance.Run(t, fake.New())
}
