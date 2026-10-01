package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

// ProjectStateDraft and the other project states: draft, defined (set
// automatically at the first snapshot), handed off, and cancelled.
const (
	ProjectStateDraft     = "draft"
	ProjectStateDefined   = "defined"
	ProjectStateHandedOff = "handed off"
	ProjectStateCancelled = "cancelled"
)

// projectTransitions names, for every non-terminal state, which states it
// may move to next (other than to cancelled, which is allowed from any
// non-terminal state and handled separately below).
var projectTransitions = map[string][]string{
	ProjectStateDraft:     {ProjectStateHandedOff},
	ProjectStateDefined:   {ProjectStateHandedOff},
	ProjectStateHandedOff: {},
}

// terminalStates never transition again, not even to cancelled.
var terminalStates = map[string]bool{
	ProjectStateCancelled: true,
}

var allProjectStates = map[string]bool{
	ProjectStateDraft: true, ProjectStateDefined: true,
	ProjectStateHandedOff: true, ProjectStateCancelled: true,
}

// ProjectStateEntry is one row of a project's state history.
type ProjectStateEntry struct {
	State    string    `json:"state"`
	Actor    string    `json:"actor"`
	Reason   string    `json:"reason,omitempty"`
	On       time.Time `json:"on"`
	Snapshot int       `json:"snapshot,omitempty"`
	Bundle   string    `json:"bundle,omitempty"`
}

// ProjectState is the result of GET and POST .../state: the current state
// (a project with no history is implicitly draft) and its full history,
// oldest first.
type ProjectState struct {
	State   string              `json:"state"`
	History []ProjectStateEntry `json:"history"`
}

// GetProjectState returns a project's current state and its history. The
// project itself must exist (committed or draft-only is not enough: state
// applies to a committed project).
func (e *Engine) GetProjectState(ctx context.Context, id string) (ProjectState, error) {
	if _, found, err := e.manifests.GetCurrent(ctx, "Project", id); err != nil {
		return ProjectState{}, err
	} else if !found {
		return ProjectState{}, fmt.Errorf("%w: Project/%s", ErrNotFound, id)
	}
	rows, err := e.ops.ListProjectStateHistory(ctx, id)
	if err != nil {
		return ProjectState{}, err
	}
	return projectStateFromRows(rows), nil
}

func projectStateFromRows(rows []store.ProjectStateEntry) ProjectState {
	state := ProjectStateDraft
	if len(rows) > 0 {
		state = rows[len(rows)-1].State
	}
	history := make([]ProjectStateEntry, len(rows))
	for i, r := range rows {
		history[i] = ProjectStateEntry{State: r.State, Actor: r.Actor, Reason: r.Reason, On: r.On, Snapshot: r.Snapshot, Bundle: r.Bundle}
	}
	return ProjectState{State: state, History: history}
}

// TransitionProjectState moves a project to state to, recording actor and
// reason. draft to in review is allowed only when the project's checks
// (against its current committed version) have zero blocking items;
// otherwise this returns a *ValidationError carrying those blocking items
// as problems, for a 422 response. cancelled requires a reason, from any
// non-terminal state. Every other move must be exactly the next state in
// the fixed sequence.
func (e *Engine) TransitionProjectState(ctx context.Context, id, to, actor, reason string) (ProjectState, error) {
	if !allProjectStates[to] {
		return ProjectState{}, &ValidationError{Problems: []Problem{{Message: fmt.Sprintf("%q is not a project state", to)}}}
	}
	if _, found, err := e.manifests.GetCurrent(ctx, "Project", id); err != nil {
		return ProjectState{}, err
	} else if !found {
		return ProjectState{}, fmt.Errorf("%w: Project/%s", ErrNotFound, id)
	}
	if p, err := e.checkActor(ctx, actor); err != nil {
		return ProjectState{}, err
	} else if p != nil {
		return ProjectState{}, &ValidationError{Problems: []Problem{*p}}
	}

	rows, err := e.ops.ListProjectStateHistory(ctx, id)
	if err != nil {
		return ProjectState{}, err
	}
	current := projectStateFromRows(rows).State

	if to == ProjectStateCancelled {
		if terminalStates[current] {
			return ProjectState{}, &ValidationError{Problems: []Problem{{Message: fmt.Sprintf("%s cannot move to cancelled", current)}}}
		}
		if reason == "" {
			return ProjectState{}, &ValidationError{Problems: []Problem{{Path: "/reason", Message: "reason is required to cancel"}}}
		}
	} else {
		allowed := false
		for _, s := range projectTransitions[current] {
			if s == to {
				allowed = true
				break
			}
		}
		if !allowed {
			return ProjectState{}, &ValidationError{Problems: []Problem{{Message: fmt.Sprintf("%s cannot move to %s", current, to)}}}
		}
	}

	// draft to handed off requires zero blocking checks
	if current == ProjectStateDraft && to == ProjectStateHandedOff {
		checks, err := e.ProjectChecks(ctx, id, false)
		if err != nil {
			return ProjectState{}, err
		}
		if checks.Blocking > 0 {
			var problems []Problem
			for _, item := range checks.Items {
				if item.State == checkBlock {
					problems = append(problems, Problem{Path: item.Section, Message: item.Message})
				}
			}
			return ProjectState{}, &ValidationError{Problems: problems}
		}
	}

	entry := store.ProjectStateEntry{ProjectID: id, State: to, Actor: actor, Reason: reason, On: timeNow().UTC()}
	if err := e.ops.PutProjectStateTransition(ctx, entry); err != nil {
		return ProjectState{}, err
	}

	rows = append(rows, entry)
	return projectStateFromRows(rows), nil
}

// HandoffResult is returned by Handoff on success: where the charter
// bundle went (HTML, JSON, and PDF when one was printed).
type HandoffResult struct {
	HtmlPath string
	JsonPath string
	PdfPath  string
	Bundle   string // what the state history records: the bundle's location
}

// HandoffRequest carries the rendered charter a handoff stores.
type HandoffRequest struct {
	HtmlBytes []byte // Rendered HTML charter
	JsonBytes []byte // Rendered JSON charter
	PdfBytes  []byte // Rendered PDF charter (optional, can be nil)
}

// HandoffGate is the rule a handoff must pass, and the version it hands
// off: no blocking check, at least one saved version, and that version
// not already handed off. Callers that render a document before handing
// off ask this first, so nothing is rendered for a project that would be
// refused. Handoff asks it again; the rule lives here once.
func (e *Engine) HandoffGate(ctx context.Context, projectID string) (snapshot int, err error) {
	if _, found, err := e.manifests.GetCurrent(ctx, "Project", projectID); err != nil {
		return 0, err
	} else if !found {
		return 0, fmt.Errorf("%w: Project/%s", ErrNotFound, projectID)
	}

	checks, err := e.ProjectChecks(ctx, projectID, false)
	if err != nil {
		return 0, err
	}
	if checks.Blocking > 0 {
		var problems []Problem
		for _, item := range checks.Items {
			if item.State == checkBlock {
				problems = append(problems, Problem{Path: item.Section, Message: item.Message})
			}
		}
		return 0, &ValidationError{Problems: problems}
	}

	versions, err := e.Versions(ctx, "Project", projectID)
	if err != nil {
		return 0, err
	}
	if len(versions) == 0 {
		return 0, &ValidationError{Problems: []Problem{{Message: "save a version first"}}}
	}
	snapshot = versions[len(versions)-1].Number

	rows, err := e.ops.ListProjectStateHistory(ctx, projectID)
	if err != nil {
		return 0, err
	}
	if len(rows) > 0 && rows[len(rows)-1].State == ProjectStateHandedOff && rows[len(rows)-1].Snapshot == snapshot {
		return 0, &ValidationError{Problems: []Problem{{Message: fmt.Sprintf("already handed off at snapshot %d", snapshot)}}}
	}
	return snapshot, nil
}

// Handoff records a project's handoff and stores the charter bundle
// through the BundleStore port. It passes HandoffGate first, writes the
// bundle, then records the state transition with the version and the
// bundle's location. Refused when nowhere keeps bundles.
func (e *Engine) Handoff(ctx context.Context, projectID, actor string, req HandoffRequest) (HandoffResult, error) {
	if e.bundles == nil {
		return HandoffResult{}, fmt.Errorf("handoff needs a bundle store")
	}
	snapshotNum, err := e.HandoffGate(ctx, projectID)
	if err != nil {
		return HandoffResult{}, err
	}

	baseFile := fmt.Sprintf("charter-%s-v%d", projectID, snapshotNum)
	files := map[string][]byte{
		baseFile + ".html": req.HtmlBytes,
		baseFile + ".json": req.JsonBytes,
	}
	if len(req.PdfBytes) > 0 {
		files[baseFile+".pdf"] = req.PdfBytes
	}
	bundle, err := e.bundles.PutBundle(ctx, projectID, snapshotNum, files)
	if err != nil {
		return HandoffResult{}, fmt.Errorf("store handoff bundle: %w", err)
	}

	entry := store.ProjectStateEntry{
		ProjectID: projectID,
		State:     ProjectStateHandedOff,
		Actor:     actor,
		Reason:    "handoff",
		On:        timeNow().UTC(),
		Snapshot:  snapshotNum,
		Bundle:    bundle.Location,
	}
	if err := e.ops.PutProjectStateTransition(ctx, entry); err != nil {
		return HandoffResult{}, err
	}

	return HandoffResult{
		HtmlPath: bundle.Paths[baseFile+".html"],
		JsonPath: bundle.Paths[baseFile+".json"],
		PdfPath:  bundle.Paths[baseFile+".pdf"],
		Bundle:   bundle.Location,
	}, nil
}
