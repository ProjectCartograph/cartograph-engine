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
