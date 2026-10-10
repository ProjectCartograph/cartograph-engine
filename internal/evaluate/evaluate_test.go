package evaluate_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/evaluate"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/trace"
)

// fake answers work_summary and get as a server holding a port of the
// fictional depot charter would.
type fake struct {
	status  string
	records []map[string]any
	yaml    map[string]string
	left    []map[string]any
}

func (f fake) Call(_ context.Context, tool string, args map[string]any) (string, error) {
	switch tool {
	case "work_summary":
		b, _ := json.Marshal(map[string]any{"status": f.status, "records": f.records, "leftForYourPerson": f.left})
		return string(b), nil
	case "get":
		var items []map[string]any
		refs, _ := args["records"].([]string)
		for _, r := range refs {
			items = append(items, map[string]any{"record": r, "yaml": f.yaml[r]})
		}
		b, _ := json.Marshal(map[string]any{"items": items})
		return string(b), nil
	}
	return "", fmt.Errorf("no tool %s", tool)
}

func depotPort(status, note string) fake {
	return fake{status: status,
		records: []map[string]any{
			{"record": "Project/p-main", "name": "Depot checks", "objectives": 1.0, "components": 1.0, "milestones": 4.0, "deliverables": 2.0},
			{"record": "Project/p-survey", "name": "Grading baseline survey", "objectives": 1.0, "deliverables": 1.0},
			{"record": "Operation/o-checks", "name": "Weekly depot inspection"},
			{"record": "KPI/k-graded", "name": "Depots grading to checklist"},
		},
		yaml: map[string]string{
			"Project/p-main":     "spec:\n  summary:\n    scopeOut:\n      - Farm supply scheme (the farmers' union)\n  resources:\n    - role: manager\n      note: " + note + "\n",
			"Project/p-survey":   "spec: {}\n",
			"Operation/o-checks": "spec: {}\n",
			"KPI/k-graded":       "spec: {}\n",
		}}
}

var criteria = evaluate.Criteria{
	Name: "Depot charter port", Agent: "Agent", Runs: 3,
	WholeDocument: true, Proposed: true, OneObjectiveEach: true, NoPeople: true,
	Structure: &evaluate.Structure{Components: []string{"baseline"}, Operations: []string{"inspection"}, ScopeOut: "farm supply", NotComponents: "workstream|phase"},
	Totals:    map[string]int{"deliverables": 3, "milestones": 4},
	KPIs:      &evaluate.KPIs{Require: []string{"grading"}, Forbid: `^\s*(field|requirement)\s*$`},
}

const document = "Depot Checks Charter. The quality lead, Dr. A. Mensah, runs the grading."

func calls(bytes int) []trace.Call {
	return []trace.Call{
		{Tool: "port", Agent: "Agent", Outcome: trace.OK, InputBytes: bytes, Session: "s"},
		{Tool: "get", Agent: "Scorer", Outcome: trace.OK, Session: "s"},
	}
}

// A port that meets every criterion passes, scored from the server alone,
// and the evaluator's own calls are left out of the trace figures.
func TestAPortThatMeetsEveryCriterionPasses(t *testing.T) {
	t.Parallel()
	s, err := evaluate.ScoreRun(context.Background(), depotPort("proposed", "the quality lead"), criteria, "cs1", calls(len(document)), document)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Pass || s.Passed() != 6 {
		t.Fatalf("score %+v", s.Checks)
	}
	if s.Trace.Calls != 1 {
		t.Errorf("the scorer's calls were counted: %d", s.Trace.Calls)
	}
}

// A run fails on what the server holds, whatever the agent says: a
// person named in a note, a change set not proposed, a document sent in
// part.
func TestARunFailsOnWhatTheServerHolds(t *testing.T) {
	t.Parallel()
	s, err := evaluate.ScoreRun(context.Background(), depotPort("open", "Mensah, with the team"), criteria, "cs1", calls(10), document)
	if err != nil {
		t.Fatal(err)
	}
	failed := map[string]bool{}
	for _, c := range s.Checks {
		if !c.Pass {
			failed[c.Name] = true
		}
	}
	for _, want := range []string{"whole document", "no people", "proposed"} {
		if !failed[want] {
			t.Errorf("%s passed: %+v", want, s.Checks)
		}
	}
	if s.Pass {
		t.Error("a failing run passed")
	}
}

// The streak counts passing runs back from the latest, on its build; a
// change of build or a failure ends it.
func TestTheStreakIsCountedOnOneBuild(t *testing.T) {
	t.Parallel()
	pass, fail := &evaluate.Score{Pass: true}, &evaluate.Score{}
	runs := []evaluate.Run{{Commit: "a", Score: pass}, {Commit: "b", Score: fail}, {Commit: "b", Score: pass}, {Commit: "b", Score: pass}}
	st := evaluate.StreakOf(runs, 3)
	if st.InARow != 2 || st.Closed || st.Commit != "b" {
		t.Fatalf("streak %+v", st)
	}
	st = evaluate.StreakOf(append(runs, evaluate.Run{Commit: "b", Score: pass}), 3)
	if !st.Closed {
		t.Fatalf("three in a row not closed: %+v", st)
	}
	if strings.TrimSpace(evaluate.StreakOf(nil, 3).Commit) != "" {
		t.Error("an empty streak has a build")
	}
}

// Rounds are judged from what the change set leaves for the person and
// the agent's calls: each check left with what was asked, none the
// document states, a round before propose, several questions an exchange.
func TestRoundsAreJudgedFromWhatIsLeft(t *testing.T) {
	t.Parallel()
	c := evaluate.Criteria{Agent: "Agent", Runs: 3, Rounds: &evaluate.Rounds{NotLeft: []string{"aim-mandate"}, MaxCalls: 4}}
	asked := "Asked the budget and the go-live month; they will set both at the board"
	good := fake{status: "proposed", left: []map[string]any{
		{"on": "Project/p-main", "check": "costs-figure", "asked": asked},
		{"on": "Project/p-main", "check": "milestone-date", "asked": asked},
	}}
	run := []trace.Call{
		{Tool: "port", Agent: "Agent", Outcome: trace.OK},
		{Tool: "round", Agent: "Agent", Outcome: trace.OK},
		{Tool: "settle", Agent: "Agent", Outcome: trace.OK},
		{Tool: "propose", Agent: "Agent", Outcome: trace.OK},
	}
	s, err := evaluate.ScoreRun(context.Background(), good, c, "cs1", run, document)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Pass {
		t.Fatalf("a run that asked in rounds failed: %+v", s.Checks)
	}
	// One question an exchange, a check left unasked or stated by the
	// document, propose before any round, and too many calls each fail.
	bad := fake{status: "proposed", left: []map[string]any{
		{"on": "Project/p-main", "check": "costs-figure", "asked": "Asked the budget"},
		{"on": "Project/p-main", "check": "milestone-date", "asked": "Asked the go-live month"},
		{"on": "Project/p-main", "check": "aim-mandate", "asked": "not available"},
	}}
	late := append([]trace.Call{run[0], run[3], run[1]}, run[2], run[2])
	s, err = evaluate.ScoreRun(context.Background(), bad, c, "cs1", late, document)
	if err != nil {
		t.Fatal(err)
	}
	failed := map[string]bool{}
	for _, ch := range s.Checks {
		if !ch.Pass {
			failed[ch.Name] = true
		}
	}
	for _, want := range []string{"left with what was asked", "round before propose", "asked together", "no more calls"} {
		if !failed[want] {
			t.Errorf("%s passed: %+v", want, s.Checks)
		}
	}
}

// The triangle is judged from what was left for the person: a side's
// stance or a risk's side may be theirs to decide, but only if they were
// asked (TAXONOMY.md D60).
func TestTheTriangleIsAskedNotLeftSilent(t *testing.T) {
	t.Parallel()
	c := evaluate.Criteria{Agent: "Agent", Runs: 3, Triangle: true}
	run := []trace.Call{{Tool: "propose", Agent: "Agent", Outcome: trace.OK}}
	asked := fake{status: "proposed", records: mainProject, left: []map[string]any{
		{"on": "Project/p-main", "check": "constraints-stated", "asked": "Asked whether the dates or the budget give first; the board decides"},
	}}
	s, err := evaluate.ScoreRun(context.Background(), asked, c, "cs1", run, document)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Pass {
		t.Fatalf("a stance left with the person's answer failed: %+v", s.Checks)
	}
	silent := fake{status: "proposed", records: mainProject, left: []map[string]any{
		{"on": "Project/p-main", "check": "risks-constrained", "asked": "not available"},
	}}
	s, err = evaluate.ScoreRun(context.Background(), silent, c, "cs1", run, document)
	if err != nil {
		t.Fatal(err)
	}
	if s.Pass {
		t.Fatalf("a side left without asking passed: %+v", s.Checks)
	}
}

var mainProject = []map[string]any{{"record": "Project/p-main", "name": "Grading rollout"}}
