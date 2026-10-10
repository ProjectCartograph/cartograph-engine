package evaluate

import "sort"

// Run is one evaluation run: its name, the commit of the build it ran
// on, and its score once scored.
type Run struct {
	Name   string `json:"name"`
	Commit string `json:"commit"`
	Score  *Score `json:"score,omitempty"`
}

// Streak is where a feature stands against its bar: the runs that pass
// in a row at the end, on the latest run's build, and whether that is
// enough to close it (docs/EVALUATING.md, Control).
type Streak struct {
	Commit string `json:"commit"`
	InARow int    `json:"inARow"`
	Bar    int    `json:"bar"`
	// MedianCalls is the streak's median calls per run, held to MaxCalls,
	// the last streak's, when one is set: waste is judged on the streak,
	// since one run in two of an unchanged process is above its median.
	MedianCalls int  `json:"medianCalls,omitempty"`
	MaxCalls    int  `json:"maxCalls,omitempty"`
	Closed      bool `json:"closed"`
}

// StreakOf counts, from the latest run back, the scored runs that pass on
// the latest run's build. A run on another build, a failed run or one not
// yet scored ends the count: a change to the code resets it. With
// maxCalls, the streak closes only when its median calls are within it.
func StreakOf(runs []Run, bar, maxCalls int) Streak {
	s := Streak{Bar: bar, MaxCalls: maxCalls}
	if len(runs) == 0 {
		return s
	}
	s.Commit = runs[len(runs)-1].Commit
	var calls []int
	for i := len(runs) - 1; i >= 0; i-- {
		r := runs[i]
		if r.Commit != s.Commit || r.Score == nil || !r.Score.Pass {
			break
		}
		s.InARow++
		calls = append(calls, r.Score.Trace.Calls)
	}
	if len(calls) > 0 {
		sort.Ints(calls)
		s.MedianCalls = calls[len(calls)/2]
	}
	s.Closed = bar > 0 && s.InARow >= bar && (maxCalls == 0 || s.MedianCalls <= maxCalls)
	return s
}
