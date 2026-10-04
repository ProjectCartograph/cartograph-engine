package engine

import (
	"context"
	"fmt"
)

// stakeholderMapChecksOf asks what a stakeholder map is for: who has a
// stake in the work, and where each sits on the grid of influence and
// interest (Mendelow, TAXONOMY.md D25), from which the approach to each
// follows. Advisory, like a programme's: a map is filled in over time.
func (e *Engine) stakeholderMapChecksOf(_ context.Context, _ string, doc map[string]any) ([]ProgrammeCheck, error) {
	spec, _ := doc["spec"].(map[string]any)
	entries, _ := spec["entries"].([]any)
	var out []ProgrammeCheck
	add := func(checkID, section, state, message string) {
		out = append(out, ProgrammeCheck{ID: checkID, Section: section, State: state, Message: message})
	}
	if len(entries) == 0 {
		add("stakeholders-identified", "identify", programmeCheckWarn, "No stakeholder identified yet.")
		return out, nil
	}
	add("stakeholders-identified", "identify", programmeCheckOK,
		fmt.Sprintf("%d stakeholder%s identified.", len(entries), plural(len(entries))))
	unplaced := 0
	for _, en := range entries {
		m, _ := en.(map[string]any)
		_, hasInfluence := m["influence"]
		_, hasInterest := m["interest"]
		if !hasInfluence || !hasInterest {
			unplaced++
		}
	}
	if unplaced == 0 {
		add("stakeholders-placed", "assess", programmeCheckOK, "Every stakeholder is placed by influence and interest.")
	} else {
		add("stakeholders-placed", "assess", programmeCheckWarn,
			fmt.Sprintf("%d of %d stakeholders not placed by influence and interest yet.", unplaced, len(entries)))
	}
	if pc, ok := pendingCheck(doc); ok {
		out = append(out, pc)
	}
	return out, nil
}
