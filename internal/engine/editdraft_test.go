package engine_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/identity"
)

// Setting a field to null removes it, the next index of a list adds an
// item, and an index further out is refused with the way to add one.
func TestAnEditReadsNullAndTheNextIndexAsMeant(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	agent := actingAs(identity.Principal{Subject: "ada@example.org", Email: "ada@example.org", Name: "Ada", Agent: "Claude", Grant: "g1"})
	edit := func(set map[string]any) (string, error) {
		t.Helper()
		text, err := e.EditInChangeSet(agent, "", "Project", "p9", set, nil)
		return string(text), err
	}
	if _, err := edit(map[string]any{"/metadata/name": "Depot checks", "/spec/summary/about": "Checks at every depot", "/spec/summary/scopeOut": []any{"a", "b"}}); err != nil {
		t.Fatal(err)
	}
	text, err := edit(map[string]any{"/spec/summary/about": nil})
	if err != nil || strings.Contains(text, "about") {
		t.Fatalf("null did not remove the field: %v\n%s", err, text)
	}
	if text, err = edit(map[string]any{"/spec/summary/scopeOut/2": "c"}); err != nil || !strings.Contains(text, "- c") {
		t.Fatalf("the next index did not add an item: %v\n%s", err, text)
	}
	_, err = edit(map[string]any{"/spec/summary/scopeOut/9": "z"})
	if !errors.Is(err, engine.ErrBadEdit) || !strings.Contains(err.Error(), "/spec/summary/scopeOut/-") {
		t.Fatalf("an index past the end: %v", err)
	}
}

// A gap's measuredBy, read from older manifests and never written, is
// refused in a draft with the fields to use instead.
func TestADraftRefusesAGapsMeasuredBy(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	agent := actingAs(identity.Principal{Subject: "ada@example.org", Email: "ada@example.org", Name: "Ada", Agent: "Claude", Grant: "g1"})
	_, err := e.EditInChangeSet(agent, "", "Gap", "gap-x", map[string]any{"/metadata/name": "Gap X", "/spec/measuredBy": "kpi-x"}, nil)
	var ve *engine.ValidationError
	if !errors.As(err, &ve) || len(ve.Problems) != 1 || ve.Problems[0].Path != "/spec/measuredBy" || !strings.Contains(ve.Problems[0].Message, "measure") {
		t.Fatalf("measuredBy in a draft: %v", err)
	}
}

// An agent names a role where one is asked for, never a team, and is told
// what to write instead.
func TestAnAgentNamesARoleWhereOneIsAsked(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	agent := actingAs(identity.Principal{Subject: "ada@example.org", Email: "ada@example.org", Name: "Ada", Agent: "Claude", Grant: "g1"})
	_, err := e.EditInChangeSet(agent, "", "Project", "pr", map[string]any{
		"/metadata/name": "P", "/spec/team": "t1",
		"/spec/risks": []any{map[string]any{"id": "r1", "description": "R", "type": "risk", "owner": map[string]any{"kind": "Team", "id": "t1"}}},
	}, nil)
	var ve *engine.ValidationError
	if !errors.As(err, &ve) || ve.Problems[0].Path != "/spec/risks/0/owner/id" || !strings.Contains(ve.Problems[0].Message, "orgUnit") {
		t.Fatalf("a team as a risk's owner: %v", err)
	}
}
