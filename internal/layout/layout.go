// Package layout is the port through which the workspace graph is placed
// for drawing. The engine asks a Layout where each node goes and every
// interface draws what it is told, so the graph looks the same in each;
// the force adapter beside this package is the one implementation today.
package layout

// Node is one thing to place.
type Node struct {
	// Group gathers nodes that belong together (the engine passes a
	// kind's place in its order); a layout keeps a group near itself.
	Group int
}

// Edge joins two nodes, by their index in the nodes passed.
type Edge struct {
	S, T int
}

// Point is where a node goes, in units an interface scales to its screen.
type Point struct {
	X, Y float64
}

// Layout places a graph. The same nodes and edges in the same order are
// always placed the same way, so a graph asked for twice, or by two
// interfaces, is drawn alike.
type Layout interface {
	// Place returns one point per node, in the order of nodes.
	Place(nodes []Node, edges []Edge) []Point
}

// Func adapts a function to the Layout interface.
type Func func(nodes []Node, edges []Edge) []Point

// Place calls f.
func (f Func) Place(nodes []Node, edges []Edge) []Point { return f(nodes, edges) }
