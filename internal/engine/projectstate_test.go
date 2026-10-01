package engine_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/internal/engine"
)

// I3.3b: project state reduced to draft, defined, handed off, cancelled.
// defined is set automatically at the first snapshot (not tested here).

func TestProjectStateDefaultsToDraft(t *testing.T) {
	e := seededEngine(t)
	ctx := context.Background()
	mustCommit(t, e, "Project", "proj-state", "p1", projectYAML("proj-state", ""))

	st, err := e.GetProjectState(ctx, "proj-state")
	if err != nil {
		t.Fatal(err)
	}
	if st.State != engine.ProjectStateDraft || len(st.History) != 0 {
		t.Fatalf("expected implicit draft with empty history, got %+v", st)
	}
}

func TestProjectStateNotFound(t *testing.T) {
	e := seededEngine(t)
	if _, err := e.GetProjectState(context.Background(), "does-not-exist"); !errors.Is(err, engine.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if _, err := e.TransitionProjectState(context.Background(), "does-not-exist", engine.ProjectStateHandedOff, "p1", ""); !errors.Is(err, engine.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestProjectStateDraftToHandedOffBlockedByChecks(t *testing.T) {
	e := seededEngine(t)
	ctx := context.Background()
	// Minimal project (blocked checks)
	mustCommit(t, e, "Project", "proj-blocked", "p1", projectYAML("proj-blocked", ""))

	_, err := e.TransitionProjectState(ctx, "proj-blocked", engine.ProjectStateHandedOff, "p1", "")
	var ve *engine.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected a *ValidationError, got %v", err)
	}
	if len(ve.Problems) == 0 {
		t.Fatal("expected the blocking checks as problems")
	}

	st, err := e.GetProjectState(ctx, "proj-blocked")
	if err != nil {
		t.Fatal(err)
	}
	if st.State != engine.ProjectStateDraft {
		t.Fatalf("expected the project to remain draft after a refused transition, got %+v", st)
	}
}

func greenProject(t *testing.T, e *engine.Engine, id string) {
	t.Helper()
	mustCommit(t, e, "Project", id, "p1", fullProjectYAML(id))
}

func TestProjectStateDraftToHandedOff(t *testing.T) {
	e := seededEngine(t)
	ctx := context.Background()
	// Complete project (no blocking checks)
	greenProject(t, e, "proj-handoff")

	st, err := e.TransitionProjectState(ctx, "proj-handoff", engine.ProjectStateHandedOff, "p1", "")
	if err != nil {
		t.Fatalf("transition to handed off: %v", err)
	}
	if st.State != engine.ProjectStateHandedOff {
		t.Fatalf("expected handed off, got %+v", st)
	}
}

func TestProjectStateDraftToCancelled(t *testing.T) {
	e := seededEngine(t)
	ctx := context.Background()
	mustCommit(t, e, "Project", "proj-cancel", "p1", projectYAML("proj-cancel", ""))

	// Cancelled requires a reason
	if _, err := e.TransitionProjectState(ctx, "proj-cancel", engine.ProjectStateCancelled, "p1", ""); err == nil {
		t.Fatal("expected cancelling without a reason to be refused")
	}
	st, err := e.TransitionProjectState(ctx, "proj-cancel", engine.ProjectStateCancelled, "p1", "no longer needed")
	if err != nil {
		t.Fatal(err)
	}
	if st.State != engine.ProjectStateCancelled {
		t.Fatalf("expected cancelled, got %+v", st)
	}
	// Cancelled is terminal
	if _, err := e.TransitionProjectState(ctx, "proj-cancel", engine.ProjectStateDraft, "p1", ""); err == nil {
		t.Fatal("expected cancelled to refuse a further transition")
	}
}

func TestProjectStateHandedOffIsFinal(t *testing.T) {
	e := seededEngine(t)
	ctx := context.Background()
	// Complete project (no blocking checks)
	greenProject(t, e, "proj-final")

	if _, err := e.TransitionProjectState(ctx, "proj-final", engine.ProjectStateHandedOff, "p1", ""); err != nil {
		t.Fatal(err)
	}
	// Handed off cannot transition anywhere except to cancelled
	if _, err := e.TransitionProjectState(ctx, "proj-final", engine.ProjectStateDraft, "p1", ""); err == nil {
		t.Fatal("expected handed off to refuse transition back to draft")
	}
	// But can be cancelled
	st, err := e.TransitionProjectState(ctx, "proj-final", engine.ProjectStateCancelled, "p1", "stopping")
	if err != nil {
		t.Fatal(err)
	}
	if st.State != engine.ProjectStateCancelled {
		t.Fatalf("expected cancelled, got %+v", st)
	}
}

func TestProjectStateInvalidTransition(t *testing.T) {
	e := seededEngine(t)
	ctx := context.Background()
	mustCommit(t, e, "Project", "proj-invalid", "p1", projectYAML("proj-invalid", ""))

	// Draft cannot go to unknown states
	if _, err := e.TransitionProjectState(ctx, "proj-invalid", "not-a-state", "p1", ""); err == nil {
		t.Fatal("expected an unknown state name to be refused")
	}
}
