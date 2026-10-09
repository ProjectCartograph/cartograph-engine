package engine_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
)

// policy sets the workspace's roll-in policy (docs/adr/0024).
func policy(t *testing.T, e *engine.Engine, rollIn string) {
	t.Helper()
	y := "apiVersion: cartograph/v1\nkind: Settings\nmetadata:\n  id: default\n  name: Settings\nspec:\n  changeControl:\n    rollIn:\n" + rollIn
	if _, err := e.Commit(actingAs(ada), "Settings", "default", []byte(y), "ada@example.org", "policy"); err != nil {
		t.Fatal(err)
	}
}

// proposedBy has p change a team in a change set of their own and
// propose it.
func proposedBy(t *testing.T, e *engine.Engine, desc string) string {
	t.Helper()
	ctx := actingAs(ada)
	if _, err := e.Commit(ctx, "Team", "t1", fmt.Appendf(nil, teamText, "first"), "ada@example.org", "seed"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.EditInChangeSet(ctx, "", "Team", "t1", map[string]any{"/spec/description": desc}, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := e.WorkingChangeSet(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.ProposeChangeSet(ctx, cs.ID, "describe the team", nil); err != nil {
		t.Fatal(err)
	}
	return cs.ID
}

func TestASecondReviewerRollsInWhatItsAuthorMayNot(t *testing.T) {
	t.Parallel()
	e := newTestEngine(t)
	policy(t, e, "      secondReviewer: true\n")
	id := proposedBy(t, e, "reviewed")
	var invalid *engine.ValidationError
	if _, err := e.AcceptChangeSet(actingAs(ada), id, ""); !errors.As(err, &invalid) || !strings.Contains(invalid.Problems[0].Message, "second reviewer") {
		t.Fatalf("its author rolled it in: %v", err)
	}
	if _, err := e.AcceptChangeSet(actingAs(sam), id, "read it"); err != nil {
		t.Fatalf("a second reviewer could not roll it in: %v", err)
	}
	if v, _ := e.Get(actingAs(ada), "Team", "t1"); !strings.Contains(string(v.YAML), "reviewed") || v.Actor != "sam@example.org" {
		t.Fatalf("the record after review: %s by %s", v.YAML, v.Actor)
	}
}

func TestWithoutTheSecondReviewerPolicyOnlyItsPersonRollsItIn(t *testing.T) {
	t.Parallel()
	e := newTestEngine(t)
	id := proposedBy(t, e, "mine")
	if _, err := e.AcceptChangeSet(actingAs(sam), id, ""); !errors.Is(err, engine.ErrNotTheirChangeSet) {
		t.Fatalf("someone else rolled it in: %v", err)
	}
	if _, err := e.AcceptChangeSet(actingAs(ada), id, ""); err != nil {
		t.Fatal(err)
	}
}

// gapProposed has Ada draft a gap with its checks open and propose it,
// waiving what waive names.
func gapProposed(t *testing.T, e *engine.Engine, waive map[string]map[string]string) (string, []engine.OpenCheck) {
	t.Helper()
	ctx := actingAs(ada)
	gap := "apiVersion: cartograph/v1\nkind: Gap\nmetadata:\n  id: gap-open\n  name: Faults reach buyers\nspec: {}\n"
	if err := e.SaveInChangeSet(ctx, "", "Gap", "gap-open", []byte(gap)); err != nil {
		t.Fatal(err)
	}
	cs, err := e.WorkingChangeSet(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	open, err := e.OpenInChangeSet(ctx, cs.ID)
	if err != nil || len(open) == 0 {
		t.Fatalf("a bare gap has no open check to test with: %v", err)
	}
	if waive != nil {
		waive["Gap/gap-open"] = map[string]string{}
		for _, oc := range open {
			waive["Gap/gap-open"][oc.ID] = "decided next quarter"
		}
	}
	if _, err := e.ProposeChangeSet(ctx, cs.ID, "the gap", waive); err != nil {
		t.Fatal(err)
	}
	return cs.ID, open
}

func TestNothingLeftOpenRefusesWhatWasWaived(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	policy(t, e, "      checksMet: true\n")
	id, _ := gapProposed(t, e, map[string]map[string]string{})
	if _, err := e.AcceptChangeSet(actingAs(ada), id, ""); err != nil {
		t.Fatalf("every check met allows what was waived with a reason: %v", err)
	}
	e = seededEngine(t)
	policy(t, e, "      nothingLeftOpen: true\n")
	id, _ = gapProposed(t, e, map[string]map[string]string{})
	var invalid *engine.ValidationError
	if _, err := e.AcceptChangeSet(actingAs(ada), id, ""); !errors.As(err, &invalid) || !strings.Contains(invalid.Problems[0].Message, "was waived") {
		t.Fatalf("rolled in with checks waived: %v", err)
	}
}
