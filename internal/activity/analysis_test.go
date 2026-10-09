package activity

import (
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
