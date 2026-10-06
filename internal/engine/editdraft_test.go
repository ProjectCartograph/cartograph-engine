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
	e := seededEngine(t)
	agent := actingAs(identity.Principal{Subject: "ada@example.org", Email: "ada@example.org", Name: "Ada", Agent: "Claude", Grant: "g1"})
	edit := func(set map[string]any) (string, error) {
		t.Helper()
		text, err := e.EditInChangeSet(agent, "", "Team", "t9", set, nil)
		return string(text), err
	}
	if _, err := edit(map[string]any{"/metadata/name": "Team Nine", "/spec/description": "Runs the depots", "/spec/aliases": []any{"a", "b"}}); err != nil {
		t.Fatal(err)
	}
	text, err := edit(map[string]any{"/spec/description": nil})
	if err != nil || strings.Contains(text, "description") {
		t.Fatalf("null did not remove the field: %v\n%s", err, text)
	}
	if text, err = edit(map[string]any{"/spec/aliases/2": "c"}); err != nil || !strings.Contains(text, "- c") {
		t.Fatalf("the next index did not add an item: %v\n%s", err, text)
	}
	_, err = edit(map[string]any{"/spec/aliases/9": "z"})
	if !errors.Is(err, engine.ErrBadEdit) || !strings.Contains(err.Error(), "/spec/aliases/-") {
		t.Fatalf("an index past the end: %v", err)
	}
}

// A gap's measuredBy, read from older manifests and never written, is
// refused in a draft with the fields to use instead.
func TestADraftRefusesAGapsMeasuredBy(t *testing.T) {
	e := seededEngine(t)
	agent := actingAs(identity.Principal{Subject: "ada@example.org", Email: "ada@example.org", Name: "Ada", Agent: "Claude", Grant: "g1"})
	_, err := e.EditInChangeSet(agent, "", "Gap", "gap-x", map[string]any{"/metadata/name": "Gap X", "/spec/measuredBy": "kpi-x"}, nil)
	var ve *engine.ValidationError
	if !errors.As(err, &ve) || len(ve.Problems) != 1 || ve.Problems[0].Path != "/spec/measuredBy" || !strings.Contains(ve.Problems[0].Message, "measure") {
		t.Fatalf("measuredBy in a draft: %v", err)
	}
}
