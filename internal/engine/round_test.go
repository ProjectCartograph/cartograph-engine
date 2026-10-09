package engine

import "testing"

func TestFrontierAsksEachRecordsFirstStepSideBySide(t *testing.T) {
	tasks := []Task{
		{Kind: "Project", ID: "a", Phase: "define", Step: "aim", Check: "aim-problem", Stage: 9},
		{Kind: "Project", ID: "a", Phase: "define", Step: "aim", Check: "aim-smart", Stage: 9},
		{Kind: "Project", ID: "a", Phase: "measure", Step: "results", Check: "results", Stage: 9},
		{Kind: "Project", ID: "b", Phase: "define", Step: "aim", Check: "aim-problem", Stage: 9},
	}
	r := frontier(tasks, false, nil)
	if len(r.Ask) != 3 {
		t.Fatalf("asked %d, want the first step of both projects (3): %+v", len(r.Ask), r.Ask)
	}
	if r.Waiting != 1 {
		t.Errorf("waiting %d, want 1: a's results hang on what a is", r.Waiting)
	}
}

func TestFrontierHoldsAnAnswerNamingARecordUntilItIsDefined(t *testing.T) {
	tasks := []Task{
		{Kind: "Goal", ID: "outcome", Phase: "define", Step: "statement", Check: "smart-specific", State: "block", Stage: 3},
		{Kind: "KPI", ID: "k", Phase: "align", Step: "result", Check: "kpi-aligned", Field: "/spec/goals", Stage: 4},
		{Kind: "KPI", ID: "k", Phase: "measure", Step: "target", Check: "kpi-target", Field: "/spec/target", Stage: 4},
		{Kind: "Goal", ID: "outcome", Phase: "align", Step: "measures", Check: "outcomes-close-gaps", By: "Gap", Stage: 3},
	}
	names := map[Ref][]Named{{Kind: "KPI", ID: "k"}: {{Ref: Ref{Kind: "Goal", ID: "outcome"}, Path: "/spec/goals/0"}}}
	r := frontier(tasks, false, names)
	asked := map[string]bool{}
	for _, q := range r.Ask {
		asked[q.Check] = true
	}
	if !asked["smart-specific"] || !asked["kpi-target"] || asked["kpi-aligned"] {
		t.Fatalf("asked %+v: the outcome and the KPI's target, not the aims it names", r.Ask)
	}
	if len(r.Write) != 0 || r.Waiting != 2 {
		t.Errorf("write %v, waiting %d: the gap and the KPI's aims wait for the outcome", r.Write, r.Waiting)
	}
	// Once the outcome is defined, its aims are asked and the gap written.
	r = frontier(tasks[1:], false, names)
	if len(r.Ask) != 2 || len(r.Write) != 1 {
		t.Errorf("asked %+v, write %+v: want the KPI asked and the gap to write", r.Ask, r.Write)
	}
}

// A component still being defined, a goal drafted above an outcome and a
// mission open as a warning hold back only answers that name them: a
// project's risks, a survey's manager and an operation's owner are asked
// in the same round (eval runs 002 and 003).
func TestFrontierHoldsBackOnlyWhatHangsOnAnOpenDefinition(t *testing.T) {
	tasks := []Task{
		{Kind: "Purpose", ID: "default", Phase: "define", Step: "purpose", Check: "purpose-mission", State: "warn", Stage: 0},
		{Kind: "Goal", ID: "goal", Phase: "define", Step: "statement", Check: "horizon", State: "block", Stage: 1},
		{Kind: "Goal", ID: "outcome", Phase: "define", Step: "statement", Check: "goal-parent", State: "block", Field: "/spec/parent", Stage: 3},
		{Kind: "Project", ID: "survey", Phase: "define", Step: "resources", Check: "resources-sponsor-lead", State: "block", Stage: 9},
		{Kind: "Project", ID: "main", Phase: "define", Step: "risks", Check: "risk-mitigation", State: "block", Field: "/spec/risks", Stage: 9},
		{Kind: "Project", ID: "main", Phase: "define", Step: "goals", Check: "project-outcome", State: "block", Field: "/spec/goals", Stage: 9},
		{Kind: "Operation", ID: "inspection", Phase: "define", Step: "service", Check: "service-owner", State: "block", Stage: 10},
	}
	names := map[Ref][]Named{
		{Kind: "Goal", ID: "outcome"}: {{Ref: Ref{Kind: "Goal", ID: "goal"}, Path: "/spec/parent"}},
		{Kind: "Project", ID: "main"}: {{Ref: Ref{Kind: "Goal", ID: "outcome"}, Path: "/spec/goals/0"}, {Ref: Ref{Kind: "Project", ID: "survey"}, Path: "/spec/components/0/project"}},
		{Kind: "Goal", ID: "goal"}:    {{Ref: Ref{Kind: "Purpose", ID: "default"}, Path: "/spec/purpose"}},
	}
	r := frontier(tasks, false, names)
	asked := map[string]bool{}
	for _, q := range r.Ask {
		asked[q.Check] = true
	}
	for _, want := range []string{"purpose-mission", "horizon", "resources-sponsor-lead", "risk-mitigation", "service-owner"} {
		if !asked[want] {
			t.Errorf("%s not asked: %+v", want, r.Ask)
		}
	}
	if asked["goal-parent"] || asked["project-outcome"] || r.Waiting != 2 {
		t.Errorf("asked %+v, waiting %d: the outcome's parent and the project's outcome name records still open", r.Ask, r.Waiting)
	}
}

func TestFrontierLeavesWhatTheDocumentSaysToTheAgent(t *testing.T) {
	tasks := []Task{
		{Kind: "Project", ID: "a", Phase: "define", Step: "summary", Check: "beneficiaries-named", Stage: 9},
		{Kind: "Project", ID: "a", Phase: "define", Step: "summary", Check: "owner-role", Stage: 9},
	}
	if r := frontier(tasks, false, nil); len(r.Settle) != 0 || len(r.Ask) != 2 {
		t.Errorf("no document: settle %+v, ask %+v; every decision is the person's", r.Settle, r.Ask)
	}
	r := frontier(tasks, true, nil)
	if len(r.Settle) != 1 || r.Settle[0].Check != "beneficiaries-named" {
		t.Errorf("settle %+v: what the document states is the agent's to write", r.Settle)
	}
	if len(r.Ask) != 1 || r.Ask[0].Check != "owner-role" {
		t.Errorf("ask %+v: the rest is the person's", r.Ask)
	}
}

func TestFrontierIsDoneWhenNothingIsOpen(t *testing.T) {
	if !frontier(nil, false, nil).Done() {
		t.Error("no open checks: the round is done")
	}
}
