package layered_test

import (
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/layout"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/layout/conformance"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/layout/layered"
)

func TestConformance(t *testing.T) { conformance.Run(t, layered.New()) }

// Each group is a band, from the top down in group order, so an edge
// from a later group to an earlier one always runs upwards; and a node
// sits under what it names, so edges between bands do not cross.
func TestBandsRunTopDown(t *testing.T) {
	nodes := []layout.Node{{Group: 2}, {Group: 0}, {Group: 1}, {Group: 0}, {Group: 1}}
	// 2 names 3; 4 names 1; 0 names 4.
	p := layered.New().Place(nodes, []layout.Edge{{S: 2, T: 3}, {S: 4, T: 1}, {S: 0, T: 4}})
	if !(p[1].Y < p[2].Y && p[2].Y < p[0].Y && p[1].Y == p[3].Y && p[2].Y == p[4].Y) {
		t.Fatalf("bands out of order: %v", p)
	}
	if (p[1].X < p[3].X) != (p[4].X < p[2].X) {
		t.Fatalf("edges cross between the first two bands: %v", p)
	}
}

// A band too wide to read wraps onto rows, and the next band starts
// below its last row.
func TestAWideBandWraps(t *testing.T) {
	nodes := make([]layout.Node, 40)
	nodes[39].Group = 1
	p := layered.New().Place(nodes, nil)
	widest := 0.0
	for _, pt := range p[:39] {
		if pt.X > widest {
			widest = pt.X
		}
		if pt.Y >= p[39].Y {
			t.Fatalf("the next band at %v is not below %v", p[39], pt)
		}
	}
	if widest > 14*120 {
		t.Fatalf("a row runs %v wide", widest)
	}
}
