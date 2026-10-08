package evaluate

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
	Closed bool   `json:"closed"`
}

// StreakOf counts, from the latest run back, the scored runs that pass on
// the latest run's build. A run on another build, a failed run or one not
// yet scored ends the count: a change to the code resets it.
func StreakOf(runs []Run, bar int) Streak {
	s := Streak{Bar: bar}
	if len(runs) == 0 {
		return s
	}
	s.Commit = runs[len(runs)-1].Commit
	for i := len(runs) - 1; i >= 0; i-- {
		r := runs[i]
		if r.Commit != s.Commit || r.Score == nil || !r.Score.Pass {
			break
		}
		s.InARow++
	}
	s.Closed = bar > 0 && s.InARow >= bar
	return s
}
