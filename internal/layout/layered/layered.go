// Package layered is a Layout for a directed acyclic graph: each group is
// a layer, drawn as a band from the top down in the order the engine
// numbers them, so every edge runs from a band to one above it or along
// its own. Within a band, nodes are ordered by where their neighbours sit
// (the barycentre heuristic of Sugiyama's method), sweeping down and back
// up a few times, so edges cross as little as that cheap pass allows. A
// band too wide to read wraps onto rows of its own. Deterministic: the
// same input is always placed the same way.
package layered

import (
	"math"
	"sort"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/layout"
)

const (
	column = 120.0 // between neighbours in a row
	row    = 90.0  // between the rows a wide band wraps onto
	band   = 180.0 // between one band's last row and the next band
	wide   = 14    // the most nodes in a row
	sweeps = 4
)

// Layout is the layered layout.
type Layout struct{}

// New returns the layered layout.
func New() Layout { return Layout{} }

// Place puts each group on a band of its own, in group order.
func (Layout) Place(nodes []layout.Node, edges []layout.Edge) []layout.Point {
	n := len(nodes)
	out := make([]layout.Point, n)
	if n == 0 {
		return out
	}
	near := make([][]int, n)
	for _, e := range edges {
		if e.S < 0 || e.T < 0 || e.S >= n || e.T >= n || e.S == e.T {
			continue
		}
		near[e.S] = append(near[e.S], e.T)
		near[e.T] = append(near[e.T], e.S)
	}

	// The bands, in group order, each in the order the nodes came.
	var groups []int
	members := map[int][]int{}
	for i, nd := range nodes {
		if _, ok := members[nd.Group]; !ok {
			groups = append(groups, nd.Group)
		}
		members[nd.Group] = append(members[nd.Group], i)
	}
	sort.Ints(groups)
	bands := make([][]int, len(groups))
	for i, g := range groups {
		bands[i] = members[g]
	}

	// Where each node sits in its band, from 0 to 1, so bands of
	// different widths compare.
	pos := make([]float64, n)
	measure := func(b []int) {
		for k, i := range b {
			pos[i] = (float64(k) + 0.5) / float64(len(b))
		}
	}
	for _, b := range bands {
		measure(b)
	}
	inBand := make([]int, n)
	for bi, b := range bands {
		for _, i := range b {
			inBand[i] = bi
		}
	}
	// reorder sorts one band by the mean position of each node's
	// neighbours in the bands on one side of it; a node with none there
	// keeps its place.
	reorder := func(bi int, above bool) {
		b := bands[bi]
		key := make(map[int]float64, len(b))
		for _, i := range b {
			sum, count := 0.0, 0
			for _, j := range near[i] {
				if (above && inBand[j] < bi) || (!above && inBand[j] > bi) {
					sum += pos[j]
					count++
				}
			}
			if count == 0 {
				key[i] = pos[i]
			} else {
				key[i] = sum / float64(count)
			}
		}
		sort.SliceStable(b, func(x, y int) bool { return key[b[x]] < key[b[y]] })
		measure(b)
	}
	for s := 0; s < sweeps; s++ {
		for bi := 1; bi < len(bands); bi++ {
			reorder(bi, true)
		}
		for bi := len(bands) - 2; bi >= 0; bi-- {
			reorder(bi, false)
		}
	}

	y := 0.0
	for bi, b := range bands {
		if bi > 0 {
			y += band
		}
		rows := int(math.Ceil(float64(len(b)) / wide))
		per := int(math.Ceil(float64(len(b)) / float64(rows)))
		for k, i := range b {
			r, c := k/per, k%per
			inRow := per
			if r == rows-1 {
				inRow = len(b) - r*per
			}
			// Alternate rows shift by half a column, so an edge to the
			// row behind is not hidden by the node in front.
			shift := 0.0
			if r%2 == 1 {
				shift = column / 2
			}
			out[i] = layout.Point{X: (float64(c)-float64(inRow-1)/2)*column + shift, Y: y + float64(r)*row}
		}
		y += float64(rows-1) * row
	}
	return out
}
