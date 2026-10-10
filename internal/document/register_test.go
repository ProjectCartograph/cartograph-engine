package document_test

import (
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/document"
)

const milestones = "    No.     Milestone                    Owner            Dependency     Completion Evidence\n\n" +
	"    M1      Checklist agreed             Quality team                    Completed - minutes on file\n\n" +
	"    M2      Pilot graded                 Quality team                    Completed - pilot forms\n\n" +
	"    M3      Graders trained              Quality team     M2             Planned\n\n" +
	"    Field                        Requirement           Project Response\n\n" +
	"    Reporting Frequency          Mandatory             Monthly report to the board\n"

// A register's rows are read by their columns, and the register ends
// where another table begins.
func TestARegisterEndsWhereAnotherTableBegins(t *testing.T) {
	t.Parallel()
	rows := document.ReadRegister(milestones)
	if len(rows) != 3 {
		t.Fatalf("rows %+v", rows)
	}
	if got := rows[0].Cells["Completion Evidence"]; got != "Completed - minutes on file" {
		t.Errorf("evidence read as %q", got)
	}
}

// The porting map is applied as the rows are read: a completed milestone
// nothing still to come waits on is not ported; one that is waited on is.
func TestACompletedMilestoneIsNotPortedUnlessWaitedOn(t *testing.T) {
	t.Parallel()
	skipped := map[string]bool{}
	for _, it := range document.RegisterItems("/spec/milestones", document.ReadRegister(milestones)) {
		if _, skip := it["_skip"]; skip {
			skipped[it["id"].(string)] = true
		}
	}
	if !skipped["m1"] || skipped["m2"] || skipped["m3"] {
		t.Errorf("skipped %v", skipped)
	}
}

// A document splits at its headings, each section saying what it feeds.
func TestADocumentSplitsAtItsHeadings(t *testing.T) {
	t.Parallel()
	text := "Depot Checks Charter\n\nG1. Milestone Plan\n" + milestones
	secs := document.Split("Depot Checks Charter", text)
	if len(secs) != 2 || secs[1].Heading != "G1. Milestone Plan" || len(secs[1].Feeds) == 0 {
		t.Fatalf("sections %+v", secs)
	}
}

// A long text is cut at a sentence, else a clause, else a word, and
// never ends on a joining word.
func TestClipCutsAtABoundary(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		in   string
		n    int
		want string
	}{
		{"Agree the grading checklist. Then train every grader at every depot", 40, "Agree the grading checklist"},
		{"Agree the grading checklist with the depots, then train every grader", 50, "Agree the grading checklist with the depots"},
		{"Agree the grading checklist and train every grader at the depots", 32, "Agree the grading checklist"},
		{"Short enough", 40, "Short enough"},
		{"Unbreakablelongwordwithoutanyspacesatall", 10, "Unbreakabl"},
	} {
		if got := document.Clip(c.in, c.n); got != c.want {
			t.Errorf("Clip(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}

const riskRegister = "    No.   Risk                               Schedule Impact   Cost Impact   Likelihood   Response Strategy   Mitigation\n\n" +
	"    R1    The board meets after the season   High              Low           Medium       Reduce              Put the results to the board early\n\n" +
	"    R2    Forms run short at the depots      Medium                          Low          Accept              Hold a reserve of forms\n"

// A register that scores each side on its own places each risk on the
// triple constraint, and reads its response strategy (TAXONOMY.md D60).
func TestARiskRegisterPlacesRisksOnTheTripleConstraint(t *testing.T) {
	t.Parallel()
	items := document.RegisterItems("/spec/risks", document.ReadRegister(riskRegister))
	if len(items) != 2 {
		t.Fatalf("items %+v", items)
	}
	first := items[0]
	affects, _ := first["affects"].([]any)
	if len(affects) != 2 || affects[0].(map[string]any)["constraint"] != "schedule" || affects[0].(map[string]any)["impact"] != "high" ||
		affects[1].(map[string]any)["constraint"] != "cost" || affects[1].(map[string]any)["impact"] != "low" {
		t.Errorf("affects %+v", first["affects"])
	}
	if first["response"] != "mitigate" || items[1]["response"] != "accept" {
		t.Errorf("responses %v, %v", first["response"], items[1]["response"])
	}
	if first["mitigation"] != "Put the results to the board early" {
		t.Errorf("mitigation %v", first["mitigation"])
	}
}
