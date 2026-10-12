package engine

import "testing"

// A measure whose good is a value may be aimed at from above or below:
// neither is against its direction (#85).
func TestReachIsNeverTheWrongWay(t *testing.T) {
	t.Parallel()
	for _, target := range []float64{0.8, 1.2} {
		m := measure{direction: "reach", hasBase: true, hasTarget: true, baseValue: 1.0, targetValue: target}
		if m.wrongWay() {
			t.Errorf("reach from 1.0 to %v read as the wrong way", target)
		}
	}
}
