package trace

import (
	"math"
	"sort"
	"strings"
)

// Lean Six Sigma over the calls (docs/adr/0028): every call is an
// opportunity and every refusal or failure a defect, so each tool, and
// the whole, has a defect rate in parts per million and a sigma level;
// a call repeated on the same record after a defect is rework; reads,
// writes and defects are the value stream; and the paths sessions take
// show where agents vary.

// Value classes of a call, as a value stream counts them.
const (
	ValueAdding = "value-adding" // a write that was kept
	Necessary   = "necessary"    // a read the work needs
	Waste       = "waste"        // a defect, or a read repeated as it was
)

// writes are the tools that change a change set.
var writes = map[string]bool{"start_work": true, "save_draft": true, "save_drafts": true, "edit_draft": true, "settle": true,
	"leave_open": true, "discard_draft": true, "propose": true, "propose_item": true, "propose_save": true, "propose_set": true, "propose_state": true}

// ToolStats is one tool's line in the report.
type ToolStats struct {
	Tool      string  `json:"tool"`
	Calls     int     `json:"calls"`
	Defects   int     `json:"defects"`
	DPMO      float64 `json:"dpmo"`
	Sigma     float64 `json:"sigma"`
	P50Millis int64   `json:"p50Millis"`
	P95Millis int64   `json:"p95Millis"`
	// FirstPassYield is the share of this tool's steps (a tool on a
	// record in a session) that were right the first time.
	FirstPassYield float64        `json:"firstPassYield"`
	MeanBytes      int            `json:"meanInputBytes"`
	ByDefect       map[string]int `json:"byDefect,omitempty"`
}

// SessionStats is one session's line.
type SessionStats struct {
	Session  string `json:"session"`
	Calls    int    `json:"calls"`
	Defects  int    `json:"defects"`
	Rework   int    `json:"rework"`
	Proposed bool   `json:"proposed"`
	Writes   int    `json:"writes"`
}

// Report is the analysis of a set of calls.
type Report struct {
	Calls   int     `json:"calls"`
	Defects int     `json:"defects"`
	DPMO    float64 `json:"dpmo"`
	Sigma   float64 `json:"sigma"`
	// FirstPassYield is the share of calls that were not refused and were
	// not a retry of one that was.
	FirstPassYield float64 `json:"firstPassYield"`
	// RolledThroughputYield is the chance a session goes through every
	// write step it takes right the first time: the product of each
	// write tool's first-pass yield.
	RolledThroughputYield float64 `json:"rolledThroughputYield"`
	// Rework counts calls that repeat a tool on the same record after a
	// defect there; MeanAttempts is how many tries such a step took.
	Rework       int            `json:"rework"`
	MeanAttempts float64        `json:"meanAttempts"`
	Value        map[string]int `json:"valueStream"`
	// ValueAddedRatio is the share of calls that added value.
	ValueAddedRatio float64 `json:"valueAddedRatio"`
	// CallsPerSessionCV is how much sessions vary in length: the standard
	// deviation of calls per session over its mean.
	CallsPerSessionCV float64        `json:"callsPerSessionCV"`
	Tools             []ToolStats    `json:"tools"`
	Sessions          []SessionStats `json:"sessions"`
	// Transitions are the most taken steps from one tool to the next.
	Transitions []Transition `json:"transitions"`
}

// Transition is a step from one tool to the next within a session.
type Transition struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Count int    `json:"count"`
}

// Analyse reads a set of calls, in the order they were made.
func Analyse(calls []Call) Report {
	r := Report{Calls: len(calls), Value: map[string]int{}}
	byTool := map[string][]Call{}
	bySession := map[string][]Call{}
	for _, c := range calls {
		byTool[c.Tool] = append(byTool[c.Tool], c)
		bySession[c.Session] = append(bySession[c.Session], c)
		if c.Outcome != OK {
			r.Defects++
		}
	}
	r.DPMO, r.Sigma = dpmo(r.Defects, r.Calls)

	for tool, cs := range byTool {
		ts := ToolStats{Tool: tool, Calls: len(cs), ByDefect: map[string]int{}}
		var ms []int64
		bytes := 0
		for _, c := range cs {
			ms = append(ms, int64(c.Seconds*1000))
			bytes += c.InputBytes
			if c.Outcome != OK {
				ts.Defects++
				ts.ByDefect[or(c.Defect, "other")]++
			}
		}
		ts.DPMO, ts.Sigma = dpmo(ts.Defects, ts.Calls)
		ts.P50Millis, ts.P95Millis = percentile(ms, 50), percentile(ms, 95)
		ts.MeanBytes = bytes / len(cs)
		if len(ts.ByDefect) == 0 {
			ts.ByDefect = nil
		}
		r.Tools = append(r.Tools, ts)
	}
	sort.Slice(r.Tools, func(i, j int) bool {
		if r.Tools[i].Calls != r.Tools[j].Calls {
			return r.Tools[i].Calls > r.Tools[j].Calls
		}
		return r.Tools[i].Tool < r.Tools[j].Tool
	})

	// First attempts at each step, by tool: the first call of a tool on a
	// record in a session either passed or did not.
	firstTries, firstOK := map[string]int{}, map[string]int{}
	for _, cs := range bySession {
		seen := map[string]bool{}
		for _, c := range cs {
			step := c.Tool + "|" + c.Record
			if seen[step] {
				continue
			}
			seen[step] = true
			firstTries[c.Tool]++
			if c.Outcome == OK {
				firstOK[c.Tool]++
			}
		}
	}
	r.RolledThroughputYield = 1
	for i := range r.Tools {
		t := &r.Tools[i]
		if firstTries[t.Tool] > 0 {
			t.FirstPassYield = round(float64(firstOK[t.Tool])/float64(firstTries[t.Tool]), 3)
		}
		if writes[t.Tool] {
			r.RolledThroughputYield *= t.FirstPassYield
		}
	}
	r.RolledThroughputYield = round(r.RolledThroughputYield, 3)
	transitions := map[[2]string]int{}
	firstPass, steps, attempts := 0, 0, 0
	var lengths []float64
	for s, cs := range bySession {
		ss := SessionStats{Session: s, Calls: len(cs)}
		lengths = append(lengths, float64(len(cs)))
		// A step is a tool on a record; it is reworked when it is called
		// again after a defect.
		failedStep := map[string]int{}
		lastRead := map[string]string{}
		for i, c := range cs {
			step := c.Tool + "|" + c.Record
			retry := failedStep[step] > 0
			switch {
			case c.Outcome != OK:
				ss.Defects++
				failedStep[step]++
				r.Value[Waste]++
			case writes[c.Tool]:
				ss.Writes++
				r.Value[ValueAdding]++
			default:
				// A read repeated with the same keys on the same record,
				// with nothing written between, is waste.
				sig := strings.Join(c.Keys, ",")
				if prev, seen := lastRead[step]; seen && prev == sig {
					r.Value[Waste]++
				} else {
					r.Value[Necessary]++
				}
				lastRead[step] = sig
			}
			if writes[c.Tool] && c.Outcome == OK {
				lastRead = map[string]string{}
			}
			if retry {
				ss.Rework++
				if c.Outcome == OK {
					steps++
					attempts += failedStep[step] + 1
					delete(failedStep, step)
				}
			} else if c.Outcome == OK {
				firstPass++
			}
			if strings.HasPrefix(c.Tool, "propose") && c.Outcome == OK {
				ss.Proposed = true
			}
			if i > 0 {
				transitions[[2]string{cs[i-1].Tool, c.Tool}]++
			}
		}
		r.Rework += ss.Rework
		r.Sessions = append(r.Sessions, ss)
	}
	sort.Slice(r.Sessions, func(i, j int) bool { return r.Sessions[i].Session < r.Sessions[j].Session })
	if r.Calls > 0 {
		r.FirstPassYield = round(float64(firstPass)/float64(r.Calls), 3)
		r.ValueAddedRatio = round(float64(r.Value[ValueAdding])/float64(r.Calls), 3)
	}
	if steps > 0 {
		r.MeanAttempts = round(float64(attempts)/float64(steps), 2)
	}
	r.CallsPerSessionCV = round(cv(lengths), 3)
	for k, n := range transitions {
		r.Transitions = append(r.Transitions, Transition{From: k[0], To: k[1], Count: n})
	}
	sort.Slice(r.Transitions, func(i, j int) bool {
		a, b := r.Transitions[i], r.Transitions[j]
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		return a.From+a.To < b.From+b.To
	})
	if len(r.Transitions) > 15 {
		r.Transitions = r.Transitions[:15]
	}
	return r
}

// dpmo is defects per million opportunities and its sigma level, with
// the customary 1.5 sigma shift. No defects is taken as half of one, so
// a small clean sample does not read as perfect.
func dpmo(defects, calls int) (float64, float64) {
	if calls == 0 {
		return 0, 0
	}
	d := float64(defects)
	if d == 0 {
		d = 0.5
	}
	rate := d / float64(calls)
	if rate >= 1 {
		rate = 1 - 1e-9
	}
	return round(float64(defects)/float64(calls)*1e6, 0), round(normInv(1-rate)+1.5, 2)
}

func percentile(ms []int64, p int) int64 {
	if len(ms) == 0 {
		return 0
	}
	s := append([]int64(nil), ms...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	i := (len(s) - 1) * p / 100
	return s[i]
}

func cv(xs []float64) float64 {
	if len(xs) < 2 {
		return 0
	}
	mean := 0.0
	for _, x := range xs {
		mean += x
	}
	mean /= float64(len(xs))
	if mean == 0 {
		return 0
	}
	v := 0.0
	for _, x := range xs {
		v += (x - mean) * (x - mean)
	}
	return math.Sqrt(v/float64(len(xs)-1)) / mean
}

func round(x float64, places int) float64 {
	p := math.Pow(10, float64(places))
	return math.Round(x*p) / p
}

func or(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// normInv is the standard normal quantile (Acklam's approximation, good
// to about 1e-9), for turning a yield into a sigma level.
func normInv(p float64) float64 {
	a := []float64{-3.969683028665376e+01, 2.209460984245205e+02, -2.759285104469687e+02, 1.383577518672690e+02, -3.066479806614716e+01, 2.506628277459239e+00}
	b := []float64{-5.447609879822406e+01, 1.615858368580409e+02, -1.556989798598866e+02, 6.680131188771972e+01, -1.328068155288572e+01}
	c := []float64{-7.784894002430293e-03, -3.223964580411365e-01, -2.400758277161838e+00, -2.549732539343734e+00, 4.374664141464968e+00, 2.938163982698783e+00}
	d := []float64{7.784695709041462e-03, 3.224671290700398e-01, 2.445134137142996e+00, 3.754408661907416e+00}
	const lo, hi = 0.02425, 1 - 0.02425
	switch {
	case p <= 0:
		return math.Inf(-1)
	case p >= 1:
		return math.Inf(1)
	case p < lo:
		q := math.Sqrt(-2 * math.Log(p))
		return (((((c[0]*q+c[1])*q+c[2])*q+c[3])*q+c[4])*q + c[5]) / ((((d[0]*q+d[1])*q+d[2])*q+d[3])*q + 1)
	case p > hi:
		q := math.Sqrt(-2 * math.Log(1-p))
		return -(((((c[0]*q+c[1])*q+c[2])*q+c[3])*q+c[4])*q + c[5]) / ((((d[0]*q+d[1])*q+d[2])*q+d[3])*q + 1)
	}
	q := p - 0.5
	r := q * q
	return (((((a[0]*r+a[1])*r+a[2])*r+a[3])*r+a[4])*r + a[5]) * q / (((((b[0]*r+b[1])*r+b[2])*r+b[3])*r+b[4])*r + 1)
}
