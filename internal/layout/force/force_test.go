package force_test

import (
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/layout/conformance"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/layout/force"
)

func TestConformance(t *testing.T) { conformance.Run(t, force.New()) }
