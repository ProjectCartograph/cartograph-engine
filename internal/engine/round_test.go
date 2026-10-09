package engine

import "testing"

func TestFrontierAsksEachRecordsFirstStepSideBySide(t *testing.T) {
	tasks := []Task{
		{Kind: "Project", ID: "a", Phase: "define", Step: "aim", Check: "aim-problem", Stage: 9},
		{Kind: "Project", ID: "a", Phase: "define", Step: "aim", Check: "aim-smart", Stage: 9},
		{Kind: "Project", ID: "a", Phase: "measure", Step: "results", Check: "results", Stage: 9},
		{Kind: "Project", ID: "b", Phase: "define", Step: "aim", Check: "aim-problem", Stage: 9},
	}
	r := frontier(tasks, false)
	if len(r.Ask) != 3 {
		t.Fatalf("asked %d, want the first step of both projects (3): %+v", len(r.Ask), r.Ask)
	}
	if r.Waiting != 1 {
		t.Errorf("waiting %d, want 1: a's results hang on what a is", r.Waiting)
	}
}

func TestFrontierHoldsALaterStageUntilEarlierRecordsAreDefined(t *testing.T) {
	tasks := []Task{
		{Kind: "Goal", ID: "outcome", Phase: "define", Step: "statement", Check: "smart-specific", Stage: 3},
		{Kind: "KPI", ID: "k", Phase: "measure", Step: "target", Check: "kpi-target", Stage: 4},
		{Kind: "Goal", ID: "outcome", Phase: "align", Step: "measures", Check: "outcomes-close-gaps", By: "Gap", Stage: 3},
	}
	r := frontier(tasks, false)
	if len(r.Ask) != 1 || r.Ask[0].ID != "outcome" {
		t.Fatalf("asked %+v, want only the outcome: the KPI names it", r.Ask)
	}
	if len(r.Write) != 0 || r.Waiting != 2 {
		t.Errorf("write %v, waiting %d: the gap and the KPI wait for the outcome", r.Write, r.Waiting)
	}
	// Once the outcome is defined, the KPI is asked and the gap written.
	r = frontier(tasks[1:], false)
	if len(r.Ask) != 1 || r.Ask[0].ID != "k" || len(r.Write) != 1 {
		t.Errorf("asked %+v, write %+v: want the KPI's target asked and the gap to write", r.Ask, r.Write)
	}
}

func TestFrontierLeavesWhatTheDocumentSaysToTheAgent(t *testing.T) {
	tasks := []Task{
		{Kind: "Project", ID: "a", Phase: "define", Step: "summary", Check: "beneficiaries-named", Stage: 9},
		{Kind: "Project", ID: "a", Phase: "define", Step: "summary", Check: "owner-role", Stage: 9},
	}
	if r := frontier(tasks, false); len(r.Settle) != 0 || len(r.Ask) != 2 {
		t.Errorf("no document: settle %+v, ask %+v; every decision is the person's", r.Settle, r.Ask)
	}
	r := frontier(tasks, true)
	if len(r.Settle) != 1 || r.Settle[0].Check != "beneficiaries-named" {
		t.Errorf("settle %+v: what the document states is the agent's to write", r.Settle)
	}
	if len(r.Ask) != 1 || r.Ask[0].Check != "owner-role" {
		t.Errorf("ask %+v: the rest is the person's", r.Ask)
	}
}

func TestFrontierIsDoneWhenNothingIsOpen(t *testing.T) {
	if !frontier(nil, false).Done() {
		t.Error("no open checks: the round is done")
	}
}
