package activity

import (
	"math"
	"sort"
	"strings"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/spc"
)

// Lean Six Sigma over people's work (docs/EVALUATING_PEOPLE.md): acts are
// gathered into tasks, a task's opportunities come from its kind's flow,
// each defect and each kind of waste is counted against what it could
// have been, and tasks are timed from start to finish.

// Task types.
const (
	Define = "define" // a record taken through its flow to a version
	Change = "change" // a record that had a version, saved again
	Review = "review" // a change set from opened to rolled in or closed
	Find   = "find"   // a record searched for and picked
)

// Task statuses.
const (
	Finished = "finished"
	GivenUp  = "given-up"
	Open     = "open" // still being worked on when the trace ends
)

// The answers' budgets, from DESIGN_RULES.md ("The interface answers").
const (
	PressBudget   = 100 * time.Millisecond
	RequestBudget = time.Second
	// RepeatWithin is how close three presses on one thing must be to
	// count as pressing again because nothing seemed to happen.
	RepeatWithin = time.Second
)

// Task is one piece of a person's work rebuilt from the trace.
type Task struct {
	Type      string    `json:"type"`
	Status    string    `json:"status"`
	Person    string    `json:"person,omitempty"`
	Session   string    `json:"session,omitempty"`
	Kind      string    `json:"kind,omitempty"`
	Record    string    `json:"record,omitempty"`
	ChangeSet string    `json:"changeSet,omitempty"`
	Start     time.Time `json:"start"`
	End       time.Time `json:"end"`
	// Opportunities are the flow's fields and the decision to save.
	Opportunities int `json:"opportunities,omitempty"`
	// Defects are the problems refused versions named, and checks left
	// open.
	Defects int `json:"defects"`
	// Left are the checks left open, counted in Defects too.
	Left int `json:"left,omitempty"`
	// Refused are the fields refused versions named, with their steps.
	Refused []Cause `json:"refused,omitempty"`
	// Interactions are the presses and answers given while it was open.
	Interactions int `json:"interactions"`
	Screens      int `json:"screens"`
	Switches     int `json:"switches"`
	Backs        int `json:"backs"`
	Reworked     int `json:"reworked"`
	Hunting      int `json:"hunting"`
	Drafts       int `json:"drafts"`
	// OpenAtVersion are the checks left open on the version that
	// finished it.
	OpenAtVersion []string `json:"openAtVersion,omitempty"`
	// Build is the server build its last act was recorded on.
	Build string `json:"build,omitempty"`
	// Submitted is when a change set was sent for review.
	Submitted time.Time `json:"submitted,omitzero"`

	guides map[string]bool
	set    map[string]int
	last   time.Time
}

// Seconds is how long the task took, start to end.
func (t Task) Seconds() float64 { return t.End.Sub(t.Start).Seconds() }

// Cause is one place defects fell: a field of a kind, in a step.
type Cause struct {
	Kind  string `json:"kind"`
	Step  string `json:"step,omitempty"`
	Field string `json:"field"`
	Count int    `json:"count"`
}

// TaskStats is one task type on one kind.
type TaskStats struct {
	Type           string  `json:"type"`
	Kind           string  `json:"kind,omitempty"`
	Started        int     `json:"started"`
	Finished       int     `json:"finished"`
	GivenUp        int     `json:"givenUp"`
	Open           int     `json:"open"`
	Completion     float64 `json:"completion"`
	FirstPassYield float64 `json:"firstPassYield"`
	// RolledThroughputYield is the product of each step's yield along the
	// kind's flow; Define and Change only.
	RolledThroughputYield float64 `json:"rolledThroughputYield,omitempty"`
	Opportunities         int     `json:"opportunities,omitempty"`
	Defects               int     `json:"defects"`
	DPMO                  float64 `json:"dpmo"`
	Sigma                 float64 `json:"sigma"`
	MedianSeconds         float64 `json:"medianSeconds"`
	P95Seconds            float64 `json:"p95Seconds"`
	MedianInteractions    float64 `json:"medianInteractions"`
	// ShortestPath is the budget's fewest interactions for the task, when
	// a budget was given; Extra the median ratio of interactions to it.
	ShortestPath int     `json:"shortestPath,omitempty"`
	Extra        float64 `json:"extra,omitempty"`
}

// StepStats is one step of a flow: how many tasks passed it first time,
// with no field it asks refused and no step back out of it.
type StepStats struct {
	Kind   string  `json:"kind"`
	Step   string  `json:"step"`
	Tasks  int     `json:"tasks"`
	Passed int     `json:"passed"`
	Yield  float64 `json:"yield"`
}

// Criterion is one waste criterion's figure: Count of Base, as a Rate
// per the unit Per names.
type Criterion struct {
	Waste string  `json:"waste"`
	Name  string  `json:"name"`
	Count int     `json:"count"`
	Base  int     `json:"base"`
	Rate  float64 `json:"rate"`
	Per   string  `json:"per"`
}

// Stock is work in progress at the end of the trace: how many, and how
// old in the median.
type Stock struct {
	Name          string  `json:"name"`
	Count         int     `json:"count"`
	MedianAgeSecs float64 `json:"medianAgeSeconds"`
}

// LeadTime is how long work waited, in seconds.
type LeadTime struct {
	Name    string  `json:"name"`
	Count   int     `json:"count"`
	Median  float64 `json:"medianSeconds"`
	P95     float64 `json:"p95Seconds"`
	Average float64 `json:"averageSeconds"`
}

// Report is the analysis of a set of acts.
type Report struct {
	Acts int       `json:"acts"`
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
	// The whole: defects per million opportunities over Define and
	// Change tasks closed (finished or given up), and its sigma level.
	Opportunities  int     `json:"opportunities"`
	Defects        int     `json:"defects"`
	DPMO           float64 `json:"dpmo"`
	Sigma          float64 `json:"sigma"`
	FirstPassYield float64 `json:"firstPassYield"`
	Completion     float64 `json:"completion"`
	// ValueAddedRatio is the share of acts in tasks that changed the
	// record and were kept.
	ValueAddedRatio float64        `json:"valueAddedRatio"`
	Value           map[string]int `json:"value"`
	Tasks           []TaskStats    `json:"tasks"`
	Steps           []StepStats    `json:"steps,omitempty"`
	Waste           []Criterion    `json:"waste"`
	// CrossCutting are the figures for what bears on several sections at
	// once (risks, stances): raised where it bears, and placed when saved.
	CrossCutting []Criterion `json:"crossCutting"`
	LeadTimes    []LeadTime  `json:"leadTimes,omitempty"`
	Inventory    []Stock     `json:"inventory"`
	Pareto       []Cause     `json:"pareto,omitempty"`
	// NotMeasured are the criteria the document names that this build
	// does not count yet.
	NotMeasured []string `json:"notMeasured"`
	// Rebuilt are the tasks, for whoever reads further.
	Rebuilt []Task `json:"-"`
}

// Options tune the analysis.
type Options struct {
	// Idle is how long a task may go untouched, before the trace ends,
	// and still count as open rather than given up. An hour when zero.
	Idle time.Duration
	// Budget is each task's shortest path, by "type/Kind" (see Paths).
	Budget map[string]int
}

// NotMeasured are the document's criteria this analysis does not count
// yet, said in the report so a reader never takes a missing figure for a
// clean one.
var NotMeasured = []string{
	"defects: a version undone",
	"overproduction: a record defined twice",
	"overproduction: a record left alone",
	"non-used knowledge: typed again",
	"non-used knowledge: a reference made by hand",
	"extra processing: a diff with noise",
	"extra processing: a review of nothing",
}

// recordActs are the acts that name a record and belong to a Define or
// Change task.
var recordActs = map[string]bool{
	FlowOpen: true, StepEnter: true, StepBack: true, FieldSet: true, GuideOpen: true,
	DraftSave: true, DraftDiscard: true, VersionSave: true, CheckLeave: true,
}

// Analyse reads acts in the order they happened.
func Analyse(acts []Event, flows map[string]Flow, o Options) Report {
	if o.Idle == 0 {
		o.Idle = time.Hour
	}
	sort.SliceStable(acts, func(i, j int) bool { return acts[i].At.Before(acts[j].At) })
	r := Report{Acts: len(acts), Value: map[string]int{}, NotMeasured: NotMeasured}
	if len(acts) == 0 {
		return r
	}
	r.From, r.To = acts[0].At, acts[len(acts)-1].At

	var tasks []*Task
	open := map[string]*Task{}  // person|kind|record → the task on it
	sets := map[string]*Task{}  // change set → its review
	finds := map[string]*Task{} // session → its search
	closeTask := func(t *Task, status string, at time.Time) {
		t.Status, t.End = status, at
	}
	for _, a := range acts {
		switch {
		case recordActs[a.Name] && a.Kind != "" && a.Record != "":
			key := a.Person + "|" + a.Kind + "|" + a.Record
			t := open[key]
			if t == nil {
				t = &Task{Type: Define, Status: Open, Person: a.Person, Kind: a.Kind, Record: a.Record, Start: a.At,
					guides: map[string]bool{}, set: map[string]int{}}
				if f, ok := flows[a.Kind]; ok {
					t.Opportunities = f.Opportunities()
				}
				open[key] = t
				tasks = append(tasks, t)
			}
			t.last = a.At
			t.Build = or(a.Build, t.Build)
			if t.Session == "" && a.Source == Interface {
				t.Session = a.Session
			}
			if a.ChangeSet != "" {
				t.ChangeSet = a.ChangeSet
			}
			switch a.Name {
			case FlowOpen:
				if a.Target == "existing" {
					t.Type = Change
				}
			case StepBack:
				t.Backs++
				r.Value[Waste]++
			case FieldSet:
				t.set[a.Field]++
				t.Interactions++
				if t.set[a.Field] > 1 {
					t.Reworked++
				}
			case GuideOpen:
				t.guides[a.Field] = true
			case DraftSave:
				t.Drafts++
			case DraftDiscard:
				closeTask(t, GivenUp, a.At)
				delete(open, key)
			case CheckLeave:
				t.Defects++
				t.Left++
			case VersionSave:
				if a.Outcome == Refused {
					t.Defects += max(len(a.Paths), 1)
					r.Value[Waste]++
					for _, p := range a.Paths {
						t.Refused = append(t.Refused, Cause{Kind: a.Kind, Step: flows[a.Kind].StepFor(p), Field: p, Count: 1})
					}
				} else if a.Outcome == OK || a.Outcome == "" {
					t.OpenAtVersion = a.Checks
					closeTask(t, Finished, a.At)
					delete(open, key)
				}
			}
		case a.Name == ChangeSetStart || a.Name == ChangeSetSubmit || a.Name == ChangeSetAccept ||
			a.Name == ChangeSetClose || a.Name == ChangeSetReopen:
			if a.ChangeSet == "" || a.Outcome == Failed {
				continue
			}
			t := sets[a.ChangeSet]
			if t == nil {
				t = &Task{Type: Review, Status: Open, Person: a.Person, ChangeSet: a.ChangeSet, Start: a.At}
				sets[a.ChangeSet] = t
				tasks = append(tasks, t)
			}
			t.last = a.At
			t.Build = or(a.Build, t.Build)
			switch {
			case a.Name == ChangeSetSubmit && a.Outcome != Refused:
				t.Submitted = a.At
			case a.Name == ChangeSetAccept && a.Outcome != Refused:
				closeTask(t, Finished, a.At)
				for key, rt := range open {
					if rt.ChangeSet == a.ChangeSet {
						closeTask(rt, Finished, a.At)
						delete(open, key)
					}
				}
			case a.Name == ChangeSetClose:
				closeTask(t, GivenUp, a.At)
				for key, rt := range open {
					if rt.ChangeSet == a.ChangeSet {
						closeTask(rt, GivenUp, a.At)
						delete(open, key)
					}
				}
			case a.Name == ChangeSetReopen:
				t.Status, t.End = Open, time.Time{}
			}
		case a.Name == FindOpen:
			t := &Task{Type: Find, Status: Open, Person: a.Person, Session: a.Session, Start: a.At, last: a.At, Build: a.Build}
			finds[a.Session] = t
			tasks = append(tasks, t)
		case a.Name == FindPick || a.Name == FindClose:
			if t := finds[a.Session]; t != nil {
				closeTask(t, map[bool]string{true: Finished, false: GivenUp}[a.Name == FindPick], a.At)
				delete(finds, a.Session)
			}
		}
	}

	// Drafts saved inside a change set count toward its review too.
	for _, t := range tasks {
		if t.Type != Review && t.ChangeSet != "" {
			if s := sets[t.ChangeSet]; s != nil {
				s.Drafts += t.Drafts
			}
		}
	}
	// Work untouched for longer than Idle before the trace ends was given
	// up; the rest is still open.
	for _, t := range tasks {
		if t.Status != Open {
			continue
		}
		if r.To.Sub(t.last) > o.Idle {
			closeTask(t, GivenUp, t.last)
		} else {
			t.End = r.To
		}
	}
	window(tasks, acts)

	r.Tasks = taskStats(tasks, flows, o.Budget)
	r.Steps = stepStats(tasks, flows)
	closed, passed, finished := 0, 0, 0
	for _, t := range tasks {
		if t.Type == Define || t.Type == Change {
			if t.Status != Open {
				r.Opportunities += t.Opportunities
				r.Defects += t.Defects
			}
			r.Value[ValueAdding] += len(t.set) + boolInt(t.Status == Finished)
			r.Value[Waste] += t.Reworked
		}
		if t.Status == Open {
			continue
		}
		closed++
		if t.Status == Finished {
			finished++
			if t.Defects == 0 {
				passed++
			}
		}
	}
	r.DPMO, r.Sigma = spc.DPMO(r.Defects, r.Opportunities)
	r.FirstPassYield = ratio(passed, closed)
	r.Completion = ratio(finished, closed)
	r.Waste = waste(tasks, acts, r)
	r.CrossCutting = crossCutting(tasks, acts)
	for _, c := range r.Waste {
		switch c.Waste {
		case "motion", "waiting", "transport":
			r.Value[Waste] += c.Count
		}
	}
	for _, a := range acts {
		switch a.Name {
		case StepEnter, ScreenShow, FlowOpen, FindPick, DraftSave:
			r.Value[Necessary]++
		}
	}
	if total := r.Value[ValueAdding] + r.Value[Necessary] + r.Value[Waste]; total > 0 {
		r.ValueAddedRatio = round(float64(r.Value[ValueAdding])/float64(total), 3)
	}
	r.LeadTimes = leadTimes(tasks)
	r.Inventory = inventory(tasks, r.To)
	r.Pareto = pareto(tasks)
	for _, t := range tasks {
		r.Rebuilt = append(r.Rebuilt, *t)
	}
	return r
}

// Value classes of an act, as a value stream counts them.
const (
	ValueAdding = "value-adding" // an answer kept, a version saved
	Necessary   = "necessary"    // moving through what the task needs
	Waste       = "waste"        // anything in the eight wastes
)

// window gives each Define or Change task the interface's acts in its
// session while it was open: the presses, screens, pickers and switches
// that are not about one record.
func window(tasks []*Task, acts []Event) {
	bySession := map[string][]*Task{}
	for _, t := range tasks {
		if (t.Type == Define || t.Type == Change) && t.Session != "" {
			bySession[t.Session] = append(bySession[t.Session], t)
		}
	}
	for _, a := range acts {
		if a.Source != Interface {
			continue
		}
		for _, t := range bySession[a.Session] {
			if a.At.Before(t.Start) || a.At.After(t.End) {
				continue
			}
			switch a.Name {
			case Press:
				t.Interactions++
			case ScreenShow:
				t.Screens++
			case ChangeSetSwitch:
				t.Switches++
			case PickerClose:
				if a.Target == "none" {
					t.Hunting++
				}
			}
		}
	}
}

func taskStats(tasks []*Task, flows map[string]Flow, budget map[string]int) []TaskStats {
	type key struct{ typ, kind string }
	groups := map[key][]*Task{}
	for _, t := range tasks {
		groups[key{t.Type, t.Kind}] = append(groups[key{t.Type, t.Kind}], t)
	}
	steps := stepYields(tasks, flows)
	var out []TaskStats
	for k, ts := range groups {
		s := TaskStats{Type: k.typ, Kind: k.kind, Started: len(ts)}
		var secs, inter, extra []float64
		passed := 0
		for _, t := range ts {
			switch t.Status {
			case Finished:
				s.Finished++
				secs = append(secs, t.Seconds())
				inter = append(inter, float64(t.Interactions))
				if t.Defects == 0 {
					passed++
				}
			case GivenUp:
				s.GivenUp++
			default:
				s.Open++
			}
			if t.Status != Open {
				s.Opportunities += t.Opportunities
				s.Defects += t.Defects
			}
		}
		s.Completion = ratio(s.Finished, s.Finished+s.GivenUp)
		s.FirstPassYield = ratio(passed, s.Finished+s.GivenUp)
		if s.Opportunities > 0 {
			s.DPMO, s.Sigma = spc.DPMO(s.Defects, s.Opportunities)
		}
		s.MedianSeconds, s.P95Seconds = round(quantile(secs, 0.5), 1), round(quantile(secs, 0.95), 1)
		s.MedianInteractions = quantile(inter, 0.5)
		if n := budget[k.typ+"/"+k.kind]; n > 0 {
			s.ShortestPath = n
			for _, x := range inter {
				extra = append(extra, x/float64(n))
			}
			s.Extra = round(quantile(extra, 0.5), 2)
		}
		if (k.typ == Define || k.typ == Change) && len(steps[k.kind]) > 0 {
			s.RolledThroughputYield = 1
			for _, y := range steps[k.kind] {
				s.RolledThroughputYield *= y.Yield
			}
			s.RolledThroughputYield = round(s.RolledThroughputYield, 3)
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Type != out[j].Type {
			return out[i].Type < out[j].Type
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

// stepYields is, per kind, each step's first pass yield over the closed
// Define and Change tasks on it: a step passes when no field it asks was
// refused and the person did not step back out of it.
func stepYields(tasks []*Task, flows map[string]Flow) map[string][]StepStats {
	out := map[string][]StepStats{}
	byKind := map[string][]*Task{}
	for _, t := range tasks {
		if (t.Type == Define || t.Type == Change) && t.Status != Open {
			byKind[t.Kind] = append(byKind[t.Kind], t)
		}
	}
	for kind, ts := range byKind {
		f, ok := flows[kind]
		if !ok {
			continue
		}
		for _, step := range f.Steps {
			s := StepStats{Kind: kind, Step: step, Tasks: len(ts)}
			for _, t := range ts {
				failed := false
				for _, c := range t.Refused {
					if c.Step == step {
						failed = true
					}
				}
				if !failed {
					s.Passed++
				}
			}
			s.Yield = ratio(s.Passed, s.Tasks)
			out[kind] = append(out[kind], s)
		}
	}
	return out
}

func stepStats(tasks []*Task, flows map[string]Flow) []StepStats {
	var out []StepStats
	for _, ss := range stepYields(tasks, flows) {
		out = append(out, ss...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return false
	})
	return out
}

func waste(tasks []*Task, acts []Event, r Report) []Criterion {
	var (
		requests, failedReq, slowReq, unsigned, waits    int
		screens, deadEnds, presses, deadPresses, slowPrs int
		recordTasks, abandoned, sets, discarded          int
		refusedFields, unguided, leftOpen, refused       int
		switches, screensInTasks, backs, reworked, hunt  int
	)
	for _, a := range acts {
		switch a.Name {
		case Request:
			requests++
			if a.Outcome == Failed {
				failedReq++
			}
			if a.Millis > RequestBudget.Milliseconds() {
				slowReq++
				waits++
				if !a.Sign {
					unsigned++
				}
			}
		case ScreenShow:
			screens++
			if a.Millis > RequestBudget.Milliseconds() {
				waits++
				if !a.Sign {
					unsigned++
				}
			}
		case ScreenDeadEnd:
			deadEnds++
		case Press:
			presses++
			if a.Target == "none" {
				deadPresses++
			}
			if a.Millis > PressBudget.Milliseconds() {
				slowPrs++
			}
		}
	}
	for _, t := range tasks {
		switch t.Type {
		case Define, Change:
			recordTasks++
			if t.Status != Open {
				refused += t.Defects - t.Left
				leftOpen += t.Left
			}
			if t.Status == GivenUp && t.Drafts > 0 {
				abandoned++
			}
			for _, c := range t.Refused {
				refusedFields++
				if !t.guided(c.Field) {
					unguided++
				}
			}
			switches += t.Switches
			screensInTasks += t.Screens
			backs += t.Backs
			reworked += t.Reworked
			hunt += t.Hunting
		case Review:
			sets++
			if t.Status == GivenUp && t.Drafts > 0 {
				discarded++
			}
		}
	}
	return []Criterion{
		crit("defects", "a version refused", refused, r.Opportunities, 1e6, "per million opportunities"),
		crit("defects", "a check left open", leftOpen, r.Opportunities, 1e6, "per million opportunities"),
		crit("defects", "a request failed", failedReq, requests, 1e6, "per million requests"),
		crit("defects", "a dead end", deadEnds, screens, 1e3, "per thousand screens"),
		crit("overproduction", "a draft abandoned", abandoned, recordTasks, 1e2, "per hundred tasks"),
		crit("overproduction", "a change set discarded", discarded, sets, 1e2, "per hundred change sets"),
		crit("waiting", "an answer slow on the page", slowPrs, presses, 1, "share of presses"),
		crit("waiting", "an answer slow from the server", slowReq, requests, 1, "share of requests"),
		crit("waiting", "a wait with no sign", unsigned, waits, 1, "share of waits"),
		crit("non-used knowledge", "a guide passed by", unguided, refusedFields, 1, "share of refused fields"),
		crit("transport", "a change set switched", switches, recordTasks, 1, "per task"),
		crit("transport", "screens per task", screensInTasks, recordTasks, 1, "per task"),
		crit("motion", "a dead click", deadPresses, presses, 1e3, "per thousand presses"),
		crit("motion", "a repeated press", repeated(acts), presses, 1e3, "per thousand presses"),
		crit("motion", "hunting", hunt, recordTasks, 1, "per task"),
		crit("extra processing", "a field reworked", reworked, recordTasks, 1, "per task"),
		crit("extra processing", "back in a walk", backs, recordTasks, 1, "per task"),
	}
}

func crit(w, name string, count, base int, scale float64, per string) Criterion {
	c := Criterion{Waste: w, Name: name, Count: count, Base: base, Per: per}
	if base > 0 {
		c.Rate = round(float64(count)/float64(base)*scale, 3)
	}
	return c
}

// repeated counts runs of three or more presses on one thing within
// RepeatWithin of each other, each run once.
func repeated(acts []Event) int {
	type thing struct{ session, surface, field, step string }
	last := map[thing][]time.Time{}
	runs := 0
	for _, a := range acts {
		if a.Name != Press {
			continue
		}
		k := thing{a.Session, a.Surface, a.Field, a.Step}
		ts := append(last[k], a.At)
		// Keep the presses still within reach of this one.
		for len(ts) > 0 && a.At.Sub(ts[0]) > RepeatWithin {
			ts = ts[1:]
		}
		if len(ts) == 3 {
			runs++
		}
		last[k] = ts
	}
	return runs
}

func leadTimes(tasks []*Task) []LeadTime {
	var review, decide []float64
	for _, t := range tasks {
		if t.Type != Review || t.Status == Open {
			continue
		}
		review = append(review, t.Seconds())
		if !t.Submitted.IsZero() {
			decide = append(decide, t.End.Sub(t.Submitted).Seconds())
		}
	}
	return []LeadTime{lead("a change set, opened to rolled in or closed", review), lead("a change set waiting for review", decide)}
}

func lead(name string, xs []float64) LeadTime {
	l := LeadTime{Name: name, Count: len(xs), Median: round(quantile(xs, 0.5), 1), P95: round(quantile(xs, 0.95), 1)}
	if len(xs) > 0 {
		sum := 0.0
		for _, x := range xs {
			sum += x
		}
		l.Average = round(sum/float64(len(xs)), 1)
	}
	return l
}

func inventory(tasks []*Task, at time.Time) []Stock {
	var drafts, sets []float64
	for _, t := range tasks {
		if t.Status != Open {
			continue
		}
		age := at.Sub(t.Start).Seconds()
		switch t.Type {
		case Define, Change:
			if t.Drafts > 0 {
				drafts = append(drafts, age)
			}
		case Review:
			sets = append(sets, age)
		}
	}
	return []Stock{
		{Name: "open drafts", Count: len(drafts), MedianAgeSecs: round(quantile(drafts, 0.5), 1)},
		{Name: "open change sets", Count: len(sets), MedianAgeSecs: round(quantile(sets, 0.5), 1)},
	}
}

// pareto is the refused fields, most refused first.
func pareto(tasks []*Task) []Cause {
	counts := map[Cause]int{}
	for _, t := range tasks {
		for _, c := range t.Refused {
			c.Count = 0
			counts[c]++
		}
	}
	out := make([]Cause, 0, len(counts))
	for c, n := range counts {
		c.Count = n
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Kind+out[i].Field < out[j].Kind+out[j].Field
	})
	return out
}

func quantile(xs []float64, q float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	return s[max(int(math.Ceil(q*float64(len(s))))-1, 0)]
}

func ratio(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return round(float64(n)/float64(d), 3)
}

func round(x float64, places int) float64 {
	p := math.Pow(10, float64(places))
	return math.Round(x*p) / p
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// guided says whether the person opened the guide of field, or of a field
// it sits in (a list's guide covers its items).
func (t *Task) guided(field string) bool {
	for g := range t.guides {
		if field == g || strings.HasPrefix(field, g+"/") {
			return true
		}
	}
	return false
}

func or(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// crossCutting measures what bears on several sections at once
// (docs/EVALUATING_PEOPLE.md, "Cross-cutting elements"): whether a risk
// was raised where it bears or away in the register, and whether a
// project's version left its risks off the triangle or its stances
// unsaid (TAXONOMY.md D60).
func crossCutting(tasks []*Task, acts []Event) []Criterion {
	inRegister, risks := 0, 0
	for _, a := range acts {
		if a.Name != FieldSet || !strings.HasPrefix(a.Field, "/spec/risks") {
			continue
		}
		risks++
		if a.Step == "risks" {
			inRegister++
		}
	}
	versions, unplaced, unstated, accepted, spent := 0, 0, 0, 0, 0
	for _, t := range tasks {
		if t.Kind != "Project" || t.Status != Finished || (t.Type != Define && t.Type != Change) {
			continue
		}
		versions++
		for _, c := range t.OpenAtVersion {
			switch c {
			case "risks-constrained":
				unplaced++
			case "constraints-stated":
				unstated++
			case "risks-held-accepted":
				accepted++
			case "risks-spend-held":
				spent++
			}
		}
	}
	return []Criterion{
		crit("transport", "a risk raised in the register, not where it bears", inRegister, risks, 1, "share of risk answers"),
		crit("defects", "a risk on no side when saved", unplaced, versions, 1e2, "per hundred project versions"),
		crit("defects", "a side with no stance when saved", unstated, versions, 1e2, "per hundred project versions"),
		crit("defects", "a held side's risk only accepted when saved", accepted, versions, 1e2, "per hundred project versions"),
		crit("defects", "a response spending a held side when saved", spent, versions, 1e2, "per hundred project versions"),
	}
}
