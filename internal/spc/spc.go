// Package spc is statistical process control for a measure read once a
// period (TAXONOMY.md D58): the individuals and moving range chart, its
// natural process limits, the signals that the process has changed, and
// how capable it is of meeting its specification limits. It is pure
// arithmetic over readings.
package spc

import (
	"fmt"
	"math"
)

// Point is one reading on the chart.
type Point struct {
	Period string  `json:"period"`
	Value  float64 `json:"value"`
	// Signals are the rules it breaks: beyond (a limit), run (the eighth
	// in a row on one side of the centre), trend (the sixth rising or
	// falling in a row).
	Signals     []string `json:"signals,omitempty"`
	Provisional bool     `json:"provisional,omitempty"`
}

// Chart is a measure's readings with their limits: an individuals and
// moving range (XmR) chart.
type Chart struct {
	KPI    string  `json:"kpi"`
	Points []Point `json:"points"`
	// Centre is the mean; Lower and Upper the natural process limits,
	// the centre less and plus 2.66 average moving ranges.
	Centre float64 `json:"centre"`
	Lower  float64 `json:"lower"`
	Upper  float64 `json:"upper"`
	// Sigma is the process's spread estimated from the moving ranges.
	Sigma float64 `json:"sigma"`
	// Stable is no reading signalling; Enough is twenty readings or more,
	// the fewest the limits are usually trusted from.
	Stable bool `json:"stable"`
	Enough bool `json:"enough"`
	// SpecLower and SpecUpper are the KPI's specification limits, when it
	// has them; Cp and Cpk its capability against them, and SigmaLevel
	// the short-term sigma level, three times Cpk.
	SpecLower  *float64 `json:"specLower,omitempty"`
	SpecUpper  *float64 `json:"specUpper,omitempty"`
	Cp         *float64 `json:"cp,omitempty"`
	Cpk        *float64 `json:"cpk,omitempty"`
	SigmaLevel *float64 `json:"sigmaLevel,omitempty"`
	// Note says what the chart cannot yet tell.
	Note string `json:"note,omitempty"`
}

// XmR works out a chart's limits, the signals that the process has
// changed, and, where it has specification limits, its capability. The
// points are in the order they were read.
func XmR(cc *Chart) {
	n := len(cc.Points)
	if n < 2 {
		cc.Note = "Two readings at least are needed for limits."
		return
	}
	sum, mr := 0.0, 0.0
	for i, p := range cc.Points {
		sum += p.Value
		if i > 0 {
			mr += math.Abs(p.Value - cc.Points[i-1].Value)
		}
	}
	cc.Centre = sum / float64(n)
	avgMR := mr / float64(n-1)
	cc.Upper, cc.Lower = cc.Centre+2.66*avgMR, cc.Centre-2.66*avgMR
	cc.Sigma = avgMR / 1.128
	cc.Stable, cc.Enough = true, n >= 20
	side, run, dir, trend := 0, 0, 0, 1
	for i := range cc.Points {
		p := &cc.Points[i]
		if p.Value > cc.Upper || p.Value < cc.Lower {
			p.Signals = append(p.Signals, "beyond")
		}
		s := 0
		if p.Value > cc.Centre {
			s = 1
		} else if p.Value < cc.Centre {
			s = -1
		}
		if s != 0 && s == side {
			run++
		} else {
			side, run = s, 1
		}
		if s != 0 && run >= 8 {
			p.Signals = append(p.Signals, "run")
		}
		if i > 0 {
			d := 0
			if p.Value > cc.Points[i-1].Value {
				d = 1
			} else if p.Value < cc.Points[i-1].Value {
				d = -1
			}
			if d != 0 && d == dir {
				trend++
			} else {
				dir, trend = d, 2
			}
			if d != 0 && trend >= 6 {
				p.Signals = append(p.Signals, "trend")
			}
		}
		if len(p.Signals) > 0 {
			cc.Stable = false
		}
	}
	if cc.Sigma > 0 && (cc.SpecLower != nil || cc.SpecUpper != nil) {
		var cpk float64 = math.Inf(1)
		if cc.SpecUpper != nil {
			cpk = math.Min(cpk, (*cc.SpecUpper-cc.Centre)/(3*cc.Sigma))
		}
		if cc.SpecLower != nil {
			cpk = math.Min(cpk, (cc.Centre-*cc.SpecLower)/(3*cc.Sigma))
		}
		cpk = math.Round(cpk*100) / 100
		level := math.Round(3*cpk*100) / 100
		cc.Cpk, cc.SigmaLevel = &cpk, &level
		if cc.SpecLower != nil && cc.SpecUpper != nil {
			cp := math.Round((*cc.SpecUpper-*cc.SpecLower)/(6*cc.Sigma)*100) / 100
			cc.Cp = &cp
		}
	}
	switch {
	case !cc.Enough:
		cc.Note = fmt.Sprintf("%d readings: the limits settle at twenty or more; read them as provisional until then.", n)
	case !cc.Stable:
		cc.Note = "The process has signalled a change: capability means little until it is stable again."
	}
}
