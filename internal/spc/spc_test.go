package spc_test

import (
	"math"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/spc"
)

func chartOf(values ...float64) spc.Chart {
	c := spc.Chart{}
	for i, v := range values {
		c.Points = append(c.Points, spc.Point{Period: string(rune('a' + i)), Value: v})
	}
	spc.XmR(&c)
	return c
}

// The limits are the centre less and plus 2.66 average moving ranges, and
// sigma is the average moving range over 1.128.
func TestXmRLimits(t *testing.T) {
	t.Parallel()
	c := chartOf(10, 12, 10, 12)
	if c.Centre != 11 || math.Abs(c.Upper-(11+2.66*2)) > 1e-9 || math.Abs(c.Lower-(11-2.66*2)) > 1e-9 || math.Abs(c.Sigma-2/1.128) > 1e-9 {
		t.Fatalf("chart %+v", c)
	}
	if !c.Stable || c.Enough {
		t.Errorf("four steady readings: stable %v, enough %v", c.Stable, c.Enough)
	}
}

// Six rising readings in a row signal a trend, and the process is no
// longer stable.
func TestATrendSignals(t *testing.T) {
	t.Parallel()
	c := chartOf(1, 2, 3, 4, 5, 6)
	last := c.Points[len(c.Points)-1]
	if c.Stable || len(last.Signals) == 0 || last.Signals[len(last.Signals)-1] != "trend" {
		t.Fatalf("chart %+v", c)
	}
}

// Capability is read against the specification limits: Cpk from the
// nearer one, Cp from both, the sigma level three times Cpk.
func TestCapability(t *testing.T) {
	t.Parallel()
	lo, hi := 0.0, 22.0
	c := spc.Chart{SpecLower: &lo, SpecUpper: &hi}
	for i, v := range []float64{10, 12, 10, 12} {
		c.Points = append(c.Points, spc.Point{Period: string(rune('a' + i)), Value: v})
	}
	spc.XmR(&c)
	if c.Cpk == nil || c.Cp == nil || c.SigmaLevel == nil || *c.SigmaLevel != math.Round(3**c.Cpk*100)/100 {
		t.Fatalf("capability %+v", c)
	}
}
