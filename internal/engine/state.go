package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/internal/store"
)

// ErrNoState is returned by the state methods when the manifest store has
// no apply gate: every manifest it holds is live, so there is nothing to
// apply, exclude or recover.
var ErrNoState = errors.New("this store has no state manifest")

// State is the apply gate as the API presents it: the state manifest's id
// and what it includes.
type State struct {
	Name     string
	Included []string
}

// UnappliedRef is one manifest that could be applied.
type UnappliedRef = store.UnappliedRef

// HasState reports whether the store offers an apply gate.
func (e *Engine) HasState() bool { return e.state != nil }

// GetState returns the state manifest: its name and its include list.
func (e *Engine) GetState(ctx context.Context) (State, error) {
	if e.state == nil {
		return State{}, ErrNoState
	}
	included, err := e.state.Included(ctx)
	if err != nil {
		return State{}, err
	}
	if included == nil {
		included = []string{}
	}
	return State{Name: e.state.Name(), Included: included}, nil
}

// ListUnapplied lists every manifest present but not yet included and not
// excluded. An empty list, never nil.
func (e *Engine) ListUnapplied(ctx context.Context) ([]UnappliedRef, error) {
	if e.state == nil {
		return nil, ErrNoState
	}
	refs, err := e.state.ListUnapplied(ctx)
	if err != nil {
		return nil, err
	}
	if refs == nil {
		refs = []UnappliedRef{}
	}
	return refs, nil
}

// Apply includes refs ("Kind/id") in the live state, then reloads and
// reindexes once for the whole batch. A malformed ref, or one the store
// cannot find, is a ValidationError naming it; nothing is applied then.
// Applying what is already applied is quiet.
func (e *Engine) Apply(ctx context.Context, refs []string) (State, error) {
	if e.state == nil {
		return State{}, ErrNoState
	}
	if len(refs) == 0 {
		return State{}, &ValidationError{Problems: []Problem{{Message: "no ref given; pass ref or refs"}}}
	}
	for _, ref := range refs {
		kind, id, ok := strings.Cut(ref, "/")
		if !ok || kind == "" || id == "" {
			return State{}, &ValidationError{Problems: []Problem{{Message: fmt.Sprintf("invalid ref format: %s", ref)}}}
		}
	}
	added, err := e.state.Apply(ctx, refs)
	if err != nil {
		var nf *store.NotFoundError
		if errors.As(err, &nf) {
			return State{}, &ValidationError{Problems: []Problem{{Message: fmt.Sprintf("manifest not found: %s/%s", nf.Kind, nf.ID)}}}
		}
		return State{}, err
	}
	// Both of these walk the whole store, so they run once for the
	// batch, and only when something changed.
	if added > 0 {
		if err := e.state.Rehydrate(ctx); err != nil {
			return State{}, fmt.Errorf("rehydrate after apply: %w", err)
		}
		if err := e.Reindex(ctx); err != nil {
			return State{}, fmt.Errorf("reindex after apply: %w", err)
		}
	}
	return e.GetState(ctx)
}

// ListExcluded lists every excluded manifest, newest first.
func (e *Engine) ListExcluded(ctx context.Context) ([]store.Exclusion, error) {
	if e.state == nil {
		return nil, ErrNoState
	}
	out, err := e.manifests.ListExcluded(ctx)
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = []store.Exclusion{}
	}
	return out, nil
}

// Recover re-includes an excluded manifest, then reloads and reindexes.
func (e *Engine) Recover(ctx context.Context, ref, actor, reason string) (State, error) {
	if e.state == nil {
		return State{}, ErrNoState
	}
	kind, id, ok := strings.Cut(ref, "/")
	if !ok || kind == "" || id == "" {
		return State{}, &ValidationError{Problems: []Problem{{Message: fmt.Sprintf("invalid ref format: %s", ref)}}}
	}
	if reason == "" {
		reason = "recovered"
	}
	if err := e.manifests.Recover(ctx, kind, id, reason, actor); err != nil {
		return State{}, &ValidationError{Problems: []Problem{{Message: fmt.Sprintf("recover failed: %v", err)}}}
	}
	if err := e.state.Rehydrate(ctx); err != nil {
		return State{}, fmt.Errorf("rehydrate after recover: %w", err)
	}
	if err := e.Reindex(ctx); err != nil {
		return State{}, fmt.Errorf("reindex after recover: %w", err)
	}
	return e.GetState(ctx)
}
