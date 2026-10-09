package activity

import (
	"fmt"
	"sort"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/spc"
)

// Periods a reading can be taken over.
const (
	Day   = "day"
	Week  = "week"
	Build = "build"
)

// Series is one figure read once a period, with each reading's count
// (the tasks or presses it was worked out from) and its XmR chart.
type Series struct {
	Name   string    `json:"name"`
	Counts []int     `json:"counts"`
	Chart  spc.Chart `json:"chart"`
	// Split is the first period the limits were worked out from, when
	// the readings were split at a release.
	Split string `json:"split,omitempty"`
}

// PeriodOf is the period an instant or a build falls in: its date, its
// ISO week, or the build itself.
func PeriodOf(period string, at time.Time, build string) string {
	switch period {
	case Week:
		y, w := at.UTC().ISOWeek()
		return fmt.Sprintf("%d-W%02d", y, w)
	case Build:
		if build == "" {
			return "unknown"
		}
		return build
	}
	return at.UTC().Format("2006-01-02")
}

// Readings takes one reading per period of each figure that can be
// charted, and charts each series as an individuals and moving range
// chart (internal/spc). A task falls in the period it ended in; an act
// in the period it happened in. Periods are in the order they first
// appear, which for days and weeks is time order and for builds is the
// order they were served in. With split, the limits are worked out from
// that period on: a changed interface is a new process.
func Readings(acts []Event, flows map[string]Flow, o Options, period, split string) []Series {
	r := Analyse(acts, flows, o)
	var order []string
	seen := map[string]bool{}
	note := func(p string) {
		if !seen[p] {
			seen[p] = true
			order = append(order, p)
		}
	}
	tasksIn := map[string][]Task{}
	for _, t := range r.Rebuilt {
		if t.Status == Open {
			continue
		}
		p := PeriodOf(period, t.End, t.Build)
		tasksIn[p] = append(tasksIn[p], t)
	}
	actsIn := map[string][]Event{}
	for _, a := range acts {
		p := PeriodOf(period, a.At, a.Build)
		note(p)
		actsIn[p] = append(actsIn[p], a)
	}
	if period != Build {
		sort.Strings(order)
	}

	type reading struct {
		value float64
		count int
		ok    bool
	}
	figures := []struct {
		name string
		read func(ts []Task, as []Event) reading
	}{
		{"defects per million opportunities", func(ts []Task, _ []Event) reading {
			d, n := 0, 0
			for _, t := range ts {
				d, n = d+t.Defects, n+t.Opportunities
			}
			v, _ := spc.DPMO(d, n)
			return reading{v, len(ts), n > 0}
		}},
		{"first pass yield", func(ts []Task, _ []Event) reading {
			pass := 0
			for _, t := range ts {
				if t.Status == Finished && t.Defects == 0 {
					pass++
				}
			}
			return reading{ratio(pass, len(ts)), len(ts), len(ts) > 0}
		}},
		{"completion", func(ts []Task, _ []Event) reading {
			done := 0
			for _, t := range ts {
				if t.Status == Finished {
					done++
				}
			}
			return reading{ratio(done, len(ts)), len(ts), len(ts) > 0}
		}},
		{"median seconds to define a record", func(ts []Task, _ []Event) reading {
			var secs []float64
			for _, t := range ts {
				if t.Type == Define && t.Status == Finished {
					secs = append(secs, t.Seconds())
				}
			}
			return reading{round(quantile(secs, 0.5), 1), len(secs), len(secs) > 0}
		}},
		{"share of presses answered slowly", func(_ []Task, as []Event) reading {
			slow, n := 0, 0
			for _, a := range as {
				if a.Name == Press {
					n++
					if a.Millis > PressBudget.Milliseconds() {
						slow++
					}
				}
			}
			return reading{ratio(slow, n), n, n > 0}
		}},
		{"dead clicks per thousand presses", func(_ []Task, as []Event) reading {
			dead, n := 0, 0
			for _, a := range as {
				if a.Name == Press {
					n++
					if a.Target == "none" {
						dead++
					}
				}
			}
			if n == 0 {
				return reading{}
			}
			return reading{round(float64(dead)/float64(n)*1e3, 3), n, true}
		}},
	}

	var out []Series
	for _, f := range figures {
		s := Series{Name: f.name, Chart: spc.Chart{KPI: f.name}}
		from := 0
		for _, p := range order {
			rd := f.read(tasksIn[p], actsIn[p])
			if !rd.ok {
				continue
			}
			if split != "" && s.Split == "" && (p == split || period != Build && p >= split) {
				s.Split, from = p, len(s.Chart.Points)
			}
			s.Chart.Points = append(s.Chart.Points, spc.Point{Period: p, Value: rd.value})
			s.Counts = append(s.Counts, rd.count)
		}
		if split != "" && s.Split == "" {
			// Nothing after the split yet: no limits to work out.
			s.Chart.Note = "No readings since " + split + "."
			out = append(out, s)
			continue
		}
		chart := spc.Chart{KPI: f.name, Points: s.Chart.Points[from:]}
		spc.XmR(&chart)
		chart.Points = append(append([]spc.Point(nil), s.Chart.Points[:from]...), chart.Points...)
		s.Chart = chart
		out = append(out, s)
	}
	return out
}
