// Package force is a force-directed Layout: edges are springs, nodes push
// apart at short range, and each group drifts towards its own place on a
// ring, so the same kinds gather and a node sits between what it connects.
// Repulsion only looks at neighbours in a grid of cells, so a step costs
// the number of nodes, not its square. Deterministic: no randomness, and
// the same input is always placed the same way.
package force

import (
	"math"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/layout"
)

const (
	link     = 70.0 // rest length of an edge
	spring   = 0.05
	charge   = 1600.0 // repulsion
	reach    = 200.0  // repulsion's range, and the grid's cell
	anchor   = 0.012
	centre   = 0.004
	friction = 0.6
	ticks    = 300
	cooling  = 0.985
)

// Layout is the force layout.
type Layout struct{}

// New returns the force layout.
func New() Layout { return Layout{} }

type body struct{ x, y, vx, vy float64 }

// Place runs the layout until it is nearly still.
func (Layout) Place(nodes []layout.Node, edges []layout.Edge) []layout.Point {
	n := len(nodes)
	out := make([]layout.Point, n)
	if n == 0 {
		return out
	}
	// Each group's place on a ring, in the order groups first appear.
	var groups []int
	seen := map[int]bool{}
	for _, nd := range nodes {
		if !seen[nd.Group] {
			seen[nd.Group] = true
			groups = append(groups, nd.Group)
		}
	}
	ring := math.Max(260, float64(len(groups))*70)
	anchors := map[int][2]float64{}
	for i, g := range groups {
		a := float64(i)/float64(len(groups))*2*math.Pi - math.Pi/2
		anchors[g] = [2]float64{math.Cos(a) * ring, math.Sin(a) * ring}
	}
	bs := make([]body, n)
	for i, nd := range nodes {
		// A golden-angle spiral round the group's place: spread, and the
		// same every time.
		a := anchors[nd.Group]
		turn := float64(i) * 2.399963
		r := 12 * math.Sqrt(float64(i%97))
		bs[i] = body{x: a[0] + math.Cos(turn)*r, y: a[1] + math.Sin(turn)*r}
	}
	var valid []layout.Edge
	for _, e := range edges {
		if e.S >= 0 && e.S < n && e.T >= 0 && e.T < n && e.S != e.T {
			valid = append(valid, e)
		}
	}
	alpha := 1.0
	for t := 0; t < ticks; t++ {
		step(bs, nodes, valid, anchors, alpha)
		alpha *= cooling
	}
	for i, b := range bs {
		// Whole tenths: as precise as any screen needs, and the same on
		// every platform's floating point.
		out[i] = layout.Point{X: math.Round(b.x*10) / 10, Y: math.Round(b.y*10) / 10}
	}
	return out
}

func step(bs []body, nodes []layout.Node, edges []layout.Edge, anchors map[int][2]float64, alpha float64) {
	cell := func(b body) [2]int { return [2]int{int(math.Floor(b.x / reach)), int(math.Floor(b.y / reach))} }
	cells := map[[2]int][]int{}
	for i, b := range bs {
		c := cell(b)
		cells[c] = append(cells[c], i)
	}
	for i := range bs {
		a := &bs[i]
		c := cell(*a)
		for dx := -1; dx <= 1; dx++ {
			for dy := -1; dy <= 1; dy++ {
				for _, j := range cells[[2]int{c[0] + dx, c[1] + dy}] {
					if j <= i {
						continue
					}
					b := &bs[j]
					x, y := b.x-a.x, b.y-a.y
					d2 := x*x + y*y
					if d2 == 0 {
						// Two on one spot: part them along a fixed direction.
						x, y = 0.1*float64(i%7-3), 0.1
						if x == 0 {
							x = 0.1
						}
						d2 = x*x + y*y
					}
					if d2 > reach*reach {
						continue
					}
					f := charge * alpha / d2
					d := math.Sqrt(d2)
					fx, fy := x/d*f, y/d*f
					a.vx -= fx
					a.vy -= fy
					b.vx += fx
					b.vy += fy
				}
			}
		}
	}
	for _, e := range edges {
		a, b := &bs[e.S], &bs[e.T]
		x, y := b.x-a.x, b.y-a.y
		d := math.Sqrt(x*x + y*y)
		if d == 0 {
			d = 1
		}
		f := (d - link) * spring * alpha
		fx, fy := x/d*f, y/d*f
		a.vx += fx
		a.vy += fy
		b.vx -= fx
		b.vy -= fy
	}
	for i := range bs {
		b := &bs[i]
		an := anchors[nodes[i].Group]
		b.vx += (an[0] - b.x) * anchor * alpha
		b.vy += (an[1] - b.y) * anchor * alpha
		b.vx -= b.x * centre * alpha
		b.vy -= b.y * centre * alpha
		b.vx *= friction
		b.vy *= friction
		b.x += b.vx
		b.y += b.vy
	}
}
