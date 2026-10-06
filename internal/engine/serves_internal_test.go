package engine

import (
	"context"
	"testing"
)

// A goal serves an aim when it is that aim or sits beneath it, so a
// project aligned to an outcome serves a programme judged on the objective
// above it (TAXONOMY.md D24, D25).
func TestAGoalServesTheAimsAboveIt(t *testing.T) {
	t.Parallel()
	e := &Engine{}
	r := e.loadedGoals(context.Background(), map[string]map[string]any{
		"g":  {"spec": map[string]any{"level": "goal"}},
		"o":  {"spec": map[string]any{"level": "objective", "parent": "g"}},
		"x":  {"spec": map[string]any{"level": "outcome", "parent": "o"}},
		"o2": {"spec": map[string]any{"level": "objective", "parent": "g"}},
		// A loop must not hang the walk.
		"l1": {"spec": map[string]any{"parent": "l2"}},
		"l2": {"spec": map[string]any{"parent": "l1"}},
	})
	for _, c := range []struct {
		goal, aim string
		want      bool
	}{{"x", "x", true}, {"x", "o", true}, {"x", "g", true}, {"x", "o2", false}, {"o", "x", false}, {"l1", "g", false}} {
		if got := r.serves(c.goal, c.aim); got != c.want {
			t.Errorf("%s serves %s: %v, want %v", c.goal, c.aim, got, c.want)
		}
	}
}
