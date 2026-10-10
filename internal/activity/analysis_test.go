package activity

import (
	"fmt"
	"testing"
	"time"
)

// trace is one person's afternoon: a goal defined after one refused
// version, a team started and left, a change set rolled in, a search
// given up, and a few presses that went nowhere.
func trace(t0 time.Time) []Event {
	at := func(d time.Duration) time.Time { return t0.Add(d) }
	ui := func(d time.Duration, e Event) Event {
		e.At, e.Source, e.Session, e.Person = at(d), Interface, "w1", "p"
		return e
	}
	sv := func(d time.Duration, e Event) Event {
		e.At, e.Source, e.Person = at(d), Server, "p"
		return e
	}
	return []Event{
		ui(0, Event{Name: FlowOpen, Kind: "Goal", Record: "g1", Target: "new"}),
		ui(time.Second, Event{Name: StepEnter, Kind: "Goal", Record: "g1", Step: "aim"}),
		ui(2*time.Second, Event{Name: FieldSet, Kind: "Goal", Record: "g1", Field: "/metadata/name"}),
		ui(3*time.Second, Event{Name: FieldSet, Kind: "Goal", Record: "g1", Field: "/spec/objective"}),
		ui(4*time.Second, Event{Name: FieldSet, Kind: "Goal", Record: "g1", Field: "/spec/objective"}),
		ui(5*time.Second, Event{Name: Press, Surface: "goals/$id", Target: "action", Millis: 40}),
		ui(5*time.Second+300*time.Millisecond, Event{Name: Press, Surface: "goals/$id", Target: "action", Millis: 40}),
		ui(5*time.Second+600*time.Millisecond, Event{Name: Press, Surface: "goals/$id", Target: "action", Millis: 180}),
		ui(10*time.Second, Event{Name: Press, Surface: "goals/$id", Target: "none", Millis: 20}),
		ui(20*time.Second, Event{Name: GuideOpen, Kind: "Goal", Record: "g1", Field: "/spec/keyResults"}),
		sv(60*time.Second, Event{Name: VersionSave, Kind: "Goal", Record: "g1", Outcome: Refused, Problems: 2,
			Paths: []string{"/spec/objective", "/spec/keyResults/-/target"}}),
		ui(70*time.Second, Event{Name: StepBack, Kind: "Goal", Record: "g1", Step: "measures"}),
		ui(80*time.Second, Event{Name: PickerOpen, Field: "/spec/parent"}),
		ui(85*time.Second, Event{Name: PickerClose, Field: "/spec/parent", Target: "none"}),
		sv(120*time.Second, Event{Name: VersionSave, Kind: "Goal", Record: "g1", Outcome: OK}),

		ui(3*time.Minute, Event{Name: FlowOpen, Kind: "Team", Record: "t1", Target: "new"}),
		ui(4*time.Minute, Event{Name: FieldSet, Kind: "Team", Record: "t1", Field: "/metadata/name"}),
		sv(5*time.Minute, Event{Name: DraftSave, Kind: "Team", Record: "t1", Outcome: OK}),

		sv(6*time.Minute, Event{Name: ChangeSetStart, ChangeSet: "cs1", Outcome: OK}),
		sv(10*time.Minute, Event{Name: ChangeSetSubmit, ChangeSet: "cs1", Outcome: OK}),
		sv(40*time.Minute, Event{Name: ChangeSetAccept, ChangeSet: "cs1", Outcome: OK}),

		ui(50*time.Minute, Event{Name: FindOpen}),
		ui(51*time.Minute, Event{Name: FindClose}),
		ui(52*time.Minute, Event{Name: Request, Surface: "projects", Millis: 1500, Outcome: OK}),
		ui(53*time.Minute, Event{Name: ScreenShow, Surface: "projects", Millis: 200}),
		ui(54*time.Minute, Event{Name: ScreenDeadEnd, Surface: "projects"}),
		ui(3*time.Hour, Event{Name: Press, Surface: "projects", Target: "action", Millis: 30}),
	}
}

func TestAnalyseReadsAPersonsWorkAsAProcess(t *testing.T) {
	t.Parallel()
	flows, err := Flows()
	if err != nil {
		t.Fatal(err)
	}
	goal, team := flows["Goal"], flows["Team"]
	if goal.Opportunities() < 5 || team.Opportunities() < 2 {
		t.Fatalf("opportunities from the flows: goal %d, team %d", goal.Opportunities(), team.Opportunities())
	}
	r := Analyse(trace(time.Date(2026, 10, 9, 13, 0, 0, 0, time.UTC)), flows, Options{Budget: map[string]int{"define/Goal": 4}})

	if r.Opportunities != goal.Opportunities()+team.Opportunities() || r.Defects != 2 {
		t.Fatalf("opportunities %d, defects %d", r.Opportunities, r.Defects)
	}
	if want := float64(2) / float64(r.Opportunities) * 1e6; r.DPMO != float64(int(want+0.5)) {
		t.Errorf("dpmo %v, want %v", r.DPMO, want)
	}
	// Closed: the goal and the review finished, the team and the search
	// given up; only the review passed first time.
	if r.Completion != 0.5 || r.FirstPassYield != 0.25 {
		t.Errorf("completion %v, first pass yield %v", r.Completion, r.FirstPassYield)
	}
	stats := map[string]TaskStats{}
	for _, s := range r.Tasks {
		stats[s.Type+"/"+s.Kind] = s
	}
	g := stats["define/Goal"]
	if g.Finished != 1 || g.Defects != 2 || g.MedianSeconds != 120 || g.FirstPassYield != 0 {
		t.Errorf("define/Goal %+v", g)
	}
	// Three answers and four presses, against a shortest path of four.
	if g.MedianInteractions != 7 || g.Extra != 1.75 {
		t.Errorf("define/Goal interactions %v, extra %v", g.MedianInteractions, g.Extra)
	}
	if g.RolledThroughputYield != 0 {
		t.Errorf("a step refused in the only task makes the flow's yield 0, got %v", g.RolledThroughputYield)
	}
	if s := stats["define/Team"]; s.GivenUp != 1 {
		t.Errorf("a team left for over an hour is given up: %+v", s)
	}
	if s := stats["review/"]; s.Finished != 1 || s.MedianSeconds != 34*60 {
		t.Errorf("review %+v", s)
	}

	want := map[string]int{
		"a version refused": 2, "a draft abandoned": 1, "a request failed": 0, "a dead end": 1,
		"an answer slow on the page": 1, "an answer slow from the server": 1, "a wait with no sign": 1,
		"a guide passed by": 1, "a dead click": 1, "a repeated press": 1, "hunting": 1,
		"a field reworked": 1, "back in a walk": 1,
	}
	for _, c := range r.Waste {
		if n, ok := want[c.Name]; ok && c.Count != n {
			t.Errorf("%s: %d, want %d", c.Name, c.Count, n)
		}
	}
	if len(r.Pareto) != 2 || r.Pareto[0].Kind != "Goal" || r.Pareto[0].Step == "" {
		t.Errorf("pareto %+v", r.Pareto)
	}
	if r.LeadTimes[1].Median != 30*60 {
		t.Errorf("waiting for review %+v", r.LeadTimes[1])
	}
	if r.Inventory[0].Count != 0 || r.Inventory[1].Count != 0 {
		t.Errorf("inventory %+v", r.Inventory)
	}
}

func TestStepForTakesTheLongestField(t *testing.T) {
	t.Parallel()
	f := Flow{StepOf: map[string]string{"/spec": "a", "/spec/keyResults": "measures"}}
	if s := f.StepFor("/spec/keyResults/-/target"); s != "measures" {
		t.Fatalf("got %q", s)
	}
	if s := f.StepFor("/metadata/name"); s != "" {
		t.Fatalf("got %q", s)
	}
}

func TestReadingsChartEachFigurePerPeriod(t *testing.T) {
	t.Parallel()
	flows, err := Flows()
	if err != nil {
		t.Fatal(err)
	}
	// Twenty-two days, one goal defined each day: refused on the first
	// try every fourth day, and from day twelve, a new build, never.
	var acts []Event
	for d := 0; d < 22; d++ {
		t0 := time.Date(2026, 9, 1+d, 10, 0, 0, 0, time.UTC)
		build := "2.10.0"
		if d >= 11 {
			build = "2.11.0"
		}
		rec := fmt.Sprintf("g%d", d)
		if d%4 == 0 && d < 11 {
			acts = append(acts, Event{At: t0, Name: VersionSave, Source: Server, Person: "p", Kind: "Goal", Record: rec, Outcome: Refused, Paths: []string{"/spec/objective"}, Build: build})
		}
		acts = append(acts,
			Event{At: t0.Add(time.Minute), Name: VersionSave, Source: Server, Person: "p", Kind: "Goal", Record: rec, Outcome: OK, Build: build},
			Event{At: t0.Add(2 * time.Minute), Name: Press, Source: Interface, Session: "w", Person: "p", Target: "action", Millis: 50, Build: build})
	}
	series := Readings(acts, flows, Options{}, Day, "")
	byName := map[string]Series{}
	for _, s := range series {
		byName[s.Name] = s
	}
	fpy := byName["first pass yield"]
	if len(fpy.Chart.Points) != 22 || !fpy.Chart.Enough {
		t.Fatalf("first pass yield: %d points, enough %v", len(fpy.Chart.Points), fpy.Chart.Enough)
	}
	if fpy.Chart.Points[0].Value != 0 || fpy.Chart.Points[1].Value != 1 || fpy.Counts[0] != 1 {
		t.Fatalf("first pass yield readings %+v", fpy.Chart.Points[:2])
	}
	// The run of good days after the change signals that the process
	// moved.
	signalled := false
	for _, p := range fpy.Chart.Points {
		if len(p.Signals) > 0 {
			signalled = true
		}
	}
	if !signalled {
		t.Errorf("eleven clean days in a row after the change should signal: %+v", fpy.Chart)
	}

	byBuild := Readings(acts, flows, Options{}, Build, "2.11.0")
	for _, s := range byBuild {
		if s.Name != "first pass yield" {
			continue
		}
		if len(s.Chart.Points) != 2 || s.Split != "2.11.0" || s.Chart.Note == "" {
			// One reading since the split: too few for limits, and said so.
			t.Fatalf("by build, split at 2.11.0: %+v", s)
		}
	}
	if p := PeriodOf(Week, time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC), ""); p != "2026-W41" {
		t.Errorf("week %q", p)
	}
}

func TestScoreRunHoldsTheTaskToItsBar(t *testing.T) {
	t.Parallel()
	flows, err := Flows()
	if err != nil {
		t.Fatal(err)
	}
	r := Analyse(trace(time.Date(2026, 10, 9, 13, 0, 0, 0, time.UTC)), flows, Options{Budget: map[string]int{"define/Goal": 4}})
	zero, slow := 0, 0.5
	checks := ScoreRun(r, Bar{Type: Define, Kind: "Goal", MaxDefects: &zero, MaxSeconds: 300, MaxExtra: 2, MaxSlowPresses: &slow, MaxDeadClicks: &zero})
	got := map[string]bool{}
	for _, c := range checks {
		got[c.Name] = c.Pass
	}
	want := map[string]bool{"finished": true, "defects": false, "time": true, "extra interactions": true, "slow answers": true, "dead clicks": false}
	for name, pass := range want {
		if p, ok := got[name]; !ok || p != pass {
			t.Errorf("%s: pass %v (judged %v), want %v", name, p, ok, pass)
		}
	}
	if c := ScoreRun(r, Bar{Type: Define, Kind: "Project"}); len(c) != 1 || c[0].Pass {
		t.Errorf("no project was defined: %+v", c)
	}
}

// A risk raised beside the milestone it bears on is in context; one
// raised in the register is not; a version saved with the stances unsaid
// counts against the project (TAXONOMY.md D60).
func TestCrossCuttingElementsAreMeasured(t *testing.T) {
	t.Parallel()
	flows, err := Flows()
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC)
	ui := func(d time.Duration, e Event) Event {
		e.At, e.Source, e.Session, e.Person, e.Kind, e.Record = t0.Add(d), Interface, "w", "p", "Project", "p1"
		return e
	}
	acts := []Event{
		ui(0, Event{Name: FlowOpen, Target: "new"}),
		ui(time.Second, Event{Name: FieldSet, Step: "timeline", Field: "/spec/risks/{late-board}/affects"}),
		ui(2*time.Second, Event{Name: FieldSet, Step: "risks", Field: "/spec/risks/{rain}/description"}),
		ui(3*time.Second, Event{Name: FieldSet, Step: "risks", Field: "/spec/risks/{rain}/likelihood"}),
		{At: t0.Add(time.Minute), Name: VersionSave, Source: Server, Person: "p", Kind: "Project", Record: "p1", Outcome: OK,
			Checks: []string{"constraints-stated", "risks-constrained"}},
	}
	r := Analyse(acts, flows, Options{})
	got := map[string]Criterion{}
	for _, c := range r.CrossCutting {
		got[c.Name] = c
	}
	if c := got["a risk raised in the register, not where it bears"]; c.Count != 2 || c.Base != 3 {
		t.Errorf("in the register: %+v", c)
	}
	if c := got["a side with no stance when saved"]; c.Count != 1 || c.Base != 1 {
		t.Errorf("no stance: %+v", c)
	}
	if c := got["a risk on no side when saved"]; c.Count != 1 {
		t.Errorf("no side: %+v", c)
	}
}
