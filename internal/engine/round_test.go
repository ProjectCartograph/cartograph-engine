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

func TestFrontierHoldsWhatNamesARecordUntilItIsDefined(t *testing.T) {
	tasks := []Task{
		{Kind: "Goal", ID: "outcome", Phase: "define", Step: "statement", Check: "smart-specific", State: "block", Stage: 3},
		{Kind: "KPI", ID: "k", Phase: "measure", Step: "target", Check: "kpi-target", Stage: 4},
		{Kind: "Goal", ID: "outcome", Phase: "align", Step: "measures", Check: "outcomes-close-gaps", By: "Gap", Stage: 3},
	}
	names := map[Ref][]Ref{{Kind: "KPI", ID: "k"}: {{Kind: "Goal", ID: "outcome"}}}
	r := frontier(tasks, false, names)
	if len(r.Ask) != 1 || r.Ask[0].ID != "outcome" {
		t.Fatalf("asked %+v, want only the outcome: the KPI names it", r.Ask)
	}
	if len(r.Write) != 0 || r.Waiting != 2 {
		t.Errorf("write %v, waiting %d: the gap and the KPI wait for the outcome", r.Write, r.Waiting)
	}
	// Once the outcome is defined, the KPI is asked and the gap written.
	r = frontier(tasks[1:], false, names)
	if len(r.Ask) != 1 || r.Ask[0].ID != "k" || len(r.Write) != 1 {
		t.Errorf("asked %+v, write %+v: want the KPI's target asked and the gap to write", r.Ask, r.Write)
	}
}

// A goal drafted above an outcome holds back only what names it, through
// the outcome: a project naming neither, an operation, and a warning on
// the mission hold nothing back (the narrow rounds of eval run 002).
func TestFrontierHoldsBackOnlyWhatHangsOnAnOpenDefinition(t *testing.T) {
	tasks := []Task{
		{Kind: "Purpose", ID: "default", Phase: "define", Step: "purpose", Check: "purpose-mission", State: "warn", Stage: 0},
		{Kind: "Goal", ID: "goal", Phase: "define", Step: "statement", Check: "horizon", State: "block", Stage: 1},
		{Kind: "Goal", ID: "outcome", Phase: "measure", Step: "measures", Check: "smart-measurable", State: "warn", Stage: 3},
		{Kind: "Project", ID: "survey", Phase: "define", Step: "team", Check: "manager-role", State: "block", Stage: 9},
		{Kind: "Project", ID: "main", Phase: "measure", Step: "results", Check: "success-signoff", State: "block", Stage: 9},
		{Kind: "Operation", ID: "inspection", Phase: "define", Step: "service", Check: "service-owner", State: "block", Stage: 10},
	}
	names := map[Ref][]Ref{
		{Kind: "Goal", ID: "outcome"}:         {{Kind: "Goal", ID: "goal"}},
		{Kind: "Project", ID: "main"}:         {{Kind: "Goal", ID: "outcome"}, {Kind: "Project", ID: "survey"}},
		{Kind: "Goal", ID: "goal"}:            {{Kind: "Purpose", ID: "default"}},
		{Kind: "Operation", ID: "inspection"}: nil,
		{Kind: "Project", ID: "survey"}:       nil,
		{Kind: "Purpose", ID: "default"}:      nil,
	}
	r := frontier(tasks, false, names)
	asked := map[string]bool{}
	for _, q := range r.Ask {
		asked[q.ID] = true
	}
	for _, want := range []string{"default", "goal", "survey", "inspection"} {
		if !asked[want] {
			t.Errorf("%s not asked: %+v", want, r.Ask)
		}
	}
	if asked["outcome"] || asked["main"] || r.Waiting != 2 {
		t.Errorf("asked %+v, waiting %d: the outcome names the open goal, the main project the outcome and the survey", r.Ask, r.Waiting)
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
