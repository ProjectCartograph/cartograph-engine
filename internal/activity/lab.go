package activity

import (
	"encoding/json"
	"errors"
	"fmt"
)

// A lab run (docs/EVALUATING_PEOPLE.md, "Measure") is one person, or one
// agent driving the browser, doing one scripted task on a fresh vault
// served from a frozen build, scored from the people's trace alone,
// never from what they say they did.

// TaskBrief is a task as data, kept beside the evaluation directory: what
// is asked, what finished means, and the bar a run must meet.
type TaskBrief struct {
	// Name says what is judged.
	Name string `json:"name"`
	// Brief is what the person is told, word for word in every run.
	Brief string `json:"brief"`
	// Seed is a vault copied into each run's fresh vault before it is
	// served, for a task that starts from records; empty starts empty.
	Seed string `json:"seed,omitempty"`
	// Runs is how many passing runs in a row close it: three when zero.
	Runs int `json:"runs,omitempty"`
	Bar  Bar `json:"bar"`
}

// Bar is what a run must meet. Type and Kind name the task; the rest are
// limits, each checked only when set.
type Bar struct {
	Type string `json:"type"`
	Kind string `json:"kind,omitempty"`
	// MaxDefects is the most defects the task may meet: 0 means it must
	// pass first time. Absent, defects are not judged.
	MaxDefects *int `json:"maxDefects,omitempty"`
	// MaxSeconds is the longest the task may take, start to finish.
	MaxSeconds float64 `json:"maxSeconds,omitempty"`
	// MaxExtra is the most interactions the task may take, as a multiple
	// of its shortest path (paths.json).
	MaxExtra float64 `json:"maxExtra,omitempty"`
	// MaxSlowPresses is the largest share of presses answered after
	// 100 ms.
	MaxSlowPresses *float64 `json:"maxSlowPresses,omitempty"`
	// MaxDeadClicks is the most presses on nothing.
	MaxDeadClicks *int `json:"maxDeadClicks,omitempty"`
}

// ParseTaskBrief reads a task from its JSON. Where the JSON comes from (a
// file beside an evaluation directory) is the caller's business: the
// domain reads no files.
func ParseTaskBrief(b []byte) (TaskBrief, error) {
	var t TaskBrief
	if err := json.Unmarshal(b, &t); err != nil {
		return t, err
	}
	if t.Bar.Type == "" {
		return t, errors.New("the bar names no task type (define, change, review, find)")
	}
	if t.Runs == 0 {
		t.Runs = 3
	}
	return t, nil
}

// Check is one criterion of a run, judged.
type Check struct {
	Name string `json:"name"`
	Pass bool   `json:"pass"`
	Why  string `json:"why"`
}

// ScoreRun judges a run's report against the bar: the task must be
// finished, and each limit the bar sets met by the first such task.
func ScoreRun(r Report, b Bar) []Check {
	var task *Task
	for i := range r.Rebuilt {
		t := &r.Rebuilt[i]
		if t.Type == b.Type && (b.Kind == "" || t.Kind == b.Kind) && t.Status == Finished {
			task = t
			break
		}
	}
	what := b.Type
	if b.Kind != "" {
		what += " " + b.Kind
	}
	if task == nil {
		return []Check{{Name: "finished", Why: "no " + what + " task was finished"}}
	}
	out := []Check{{Name: "finished", Pass: true, Why: fmt.Sprintf("%s finished in %.0f s", what, task.Seconds())}}
	if b.MaxDefects != nil {
		out = append(out, Check{Name: "defects", Pass: task.Defects <= *b.MaxDefects,
			Why: fmt.Sprintf("%d defects, at most %d", task.Defects, *b.MaxDefects)})
	}
	if b.MaxSeconds > 0 {
		out = append(out, Check{Name: "time", Pass: task.Seconds() <= b.MaxSeconds,
			Why: fmt.Sprintf("%.0f s, at most %.0f s", task.Seconds(), b.MaxSeconds)})
	}
	if b.MaxExtra > 0 {
		for _, s := range r.Tasks {
			if s.Type == task.Type && s.Kind == task.Kind && s.ShortestPath > 0 {
				x := float64(task.Interactions) / float64(s.ShortestPath)
				out = append(out, Check{Name: "extra interactions", Pass: x <= b.MaxExtra,
					Why: fmt.Sprintf("%d interactions against a shortest path of %d: %.2fx, at most %.2fx", task.Interactions, s.ShortestPath, x, b.MaxExtra)})
			}
		}
	}
	for _, c := range r.Waste {
		switch {
		case c.Name == "an answer slow on the page" && b.MaxSlowPresses != nil:
			out = append(out, Check{Name: "slow answers", Pass: c.Rate <= *b.MaxSlowPresses,
				Why: fmt.Sprintf("%d of %d presses answered after %d ms, at most %.0f%%", c.Count, c.Base, PressBudget.Milliseconds(), *b.MaxSlowPresses*100)})
		case c.Name == "a dead click" && b.MaxDeadClicks != nil:
			out = append(out, Check{Name: "dead clicks", Pass: c.Count <= *b.MaxDeadClicks,
				Why: fmt.Sprintf("%d presses on nothing, at most %d", c.Count, *b.MaxDeadClicks)})
		}
	}
	return out
}
