package engine

import (
	"context"
	"fmt"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

// Roll-in policies (docs/adr/0024): what a change set needs before it is
// rolled into the record, set by an administrator in the workspace's
// settings and held here, for people and agents alike.

// rollInPolicy is the workspace's policy; none set is none required.
func (e *Engine) rollInPolicy(ctx context.Context) (RollInPolicy, error) {
	s, err := e.settingsOnly(ctx)
	if err != nil {
		return RollInPolicy{}, err
	}
	if s.ChangeControl == nil {
		return RollInPolicy{}, nil
	}
	return s.ChangeControl.RollIn, nil
}

// mayRollIn says whether the principal on ctx may roll the change set in:
// the person it is for, or, where the policy asks for a second reviewer,
// anyone but them. reviewer reports the second case, whose own access is
// then put to every item.
func (e *Engine) mayRollIn(policy RollInPolicy, cs store.ChangeSet, person string) (reviewer bool, err error) {
	own := cs.For == person
	switch {
	case own && policy.SecondReviewer:
		return false, &ValidationError{Problems: []Problem{{Message: "this workspace needs a second reviewer: someone other than you rolls this change set in"}}}
	case !own && !policy.SecondReviewer:
		return false, ErrNotTheirChangeSet
	}
	return !own, nil
}

// rollInProblems are what the policy finds missing in a change set about
// to be rolled in: checks not met, and checks left open or waived.
func (e *Engine) rollInProblems(ctx context.Context, policy RollInPolicy, cs store.ChangeSet) ([]Problem, error) {
	if !policy.ChecksMet && !policy.NothingLeftOpen {
		return nil, nil
	}
	open, err := e.OpenInChangeSet(ctx, cs.ID)
	if err != nil {
		return nil, err
	}
	// checksMet refuses a check unmet with nothing said about it;
	// nothingLeftOpen refuses even those left open or waived with a reason.
	waived := map[string]bool{}
	for _, w := range cs.Waivers {
		waived[w.On+" "+w.Check] = true
	}
	var unmet, left []OpenCheck
	for _, oc := range open {
		switch {
		case oc.Left != "":
			left = append(left, oc)
		case !waived[oc.Kind+"/"+oc.ManifestID+" "+oc.ID]:
			unmet = append(unmet, oc)
		}
	}
	var out []Problem
	if policy.ChecksMet {
		for _, oc := range unmet {
			out = append(out, Problem{Path: oc.Kind + "/" + oc.ManifestID,
				Message: fmt.Sprintf("every check must be met before this change set is rolled in: %s is not (%s)", oc.ID, oc.Message)})
		}
	}
	if policy.NothingLeftOpen {
		said := map[string]bool{}
		for _, oc := range left {
			said[oc.Kind+"/"+oc.ManifestID+" "+oc.ID] = true
			out = append(out, Problem{Path: oc.Kind + "/" + oc.ManifestID,
				Message: fmt.Sprintf("nothing may be left open in this workspace: %s was left open (%s)", oc.ID, oc.Left)})
		}
		// What was waived as it was proposed, unless it was left open as
		// the work went and is said above already.
		for _, w := range cs.Waivers {
			if said[w.On+" "+w.Check] {
				continue
			}
			out = append(out, Problem{Path: w.On,
				Message: fmt.Sprintf("nothing may be left open in this workspace: %s was waived (%s)", w.Check, w.Reason)})
		}
	}
	return out, nil
}
