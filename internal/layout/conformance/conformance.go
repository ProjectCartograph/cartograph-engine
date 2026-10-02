// Package conformance is what every layout.Layout must do: place every
// node somewhere finite, the same way every time, keep what is connected
// nearer than what is not, and keep a group together.
package conformance

import (
	"math"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/layout"
)

// Run checks l.
func Run(t *testing.T, l layout.Layout) {
	t.Helper()
	// Two groups; 0-1-2 a chain in group 0, 3 alone in group 1, 4 and 5
	// joined in group 1.
	nodes := []layout.Node{{Group: 0}, {Group: 0}, {Group: 0}, {Group: 1}, {Group: 1}, {Group: 1}}
	edges := []layout.Edge{{S: 0, T: 1}, {S: 1, T: 2}, {S: 4, T: 5}}
	d := func(p []layout.Point, a, b int) float64 { return math.Hypot(p[a].X-p[b].X, p[a].Y-p[b].Y) }

	t.Run("places every node somewhere finite", func(t *testing.T) {
		p := l.Place(nodes, edges)
		if len(p) != len(nodes) {
			t.Fatalf("%d points for %d nodes", len(p), len(nodes))
		}
		for i, pt := range p {
			if math.IsNaN(pt.X) || math.IsNaN(pt.Y) || math.IsInf(pt.X, 0) || math.IsInf(pt.Y, 0) {
				t.Fatalf("node %d at %v", i, pt)
			}
		}
		for i := range p {
			for j := i + 1; j < len(p); j++ {
				if d(p, i, j) < 1 {
					t.Fatalf("nodes %d and %d on one spot", i, j)
				}
			}
		}
	})
	t.Run("the same way every time", func(t *testing.T) {
		a, b := l.Place(nodes, edges), l.Place(nodes, edges)
		for i := range a {
			if a[i] != b[i] {
				t.Fatalf("node %d at %v, then %v", i, a[i], b[i])
			}
		}
	})
	t.Run("what is connected nearer than what is not", func(t *testing.T) {
		p := l.Place(nodes, edges)
		if d(p, 0, 1) >= d(p, 0, 3) {
			t.Fatalf("an edge's ends %.0f apart, a stranger %.0f", d(p, 0, 1), d(p, 0, 3))
		}
	})
	t.Run("a group together", func(t *testing.T) {
		p := l.Place(nodes, edges)
		if d(p, 4, 5) >= d(p, 4, 0) {
			t.Fatalf("its own group %.0f away, another %.0f", d(p, 4, 5), d(p, 4, 0))
		}
	})
	t.Run("nothing to place", func(t *testing.T) {
		if p := l.Place(nil, nil); len(p) != 0 {
			t.Fatalf("placed %v", p)
		}
	})
	t.Run("an edge to nowhere is ignored", func(t *testing.T) {
		if p := l.Place(nodes[:2], []layout.Edge{{S: 0, T: 9}, {S: 1, T: 1}}); len(p) != 2 {
			t.Fatalf("placed %v", p)
		}
	})
}
