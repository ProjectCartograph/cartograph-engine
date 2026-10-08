package engine

import (
	"context"
)

// stampEvents records who entered each of a project's events, as a version
// records its author (TAXONOMY.md D52): an event the current version
// already holds keeps its recorder, and a new one takes the actor saving
// it, whatever it says. It returns the document re-encoded when it changed
// anything, and nil when it did not.
func (e *Engine) stampEvents(ctx context.Context, id string, doc map[string]any, actor string) ([]byte, error) {
	spec, _ := doc["spec"].(map[string]any)
	events, _ := spec["events"].([]any)
	if len(events) == 0 {
		return nil, nil
	}
	had := map[string]string{}
	if prev, err := e.loadProjectDoc(ctx, id); err == nil {
		ps, _ := prev["spec"].(map[string]any)
		pe, _ := ps["events"].([]any)
		for _, it := range pe {
			m, _ := it.(map[string]any)
			eid, _ := m["id"].(string)
			by, _ := m["recordedBy"].(string)
			had[eid] = by
		}
	}
	changed := false
	for _, it := range events {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		eid, _ := m["id"].(string)
		want, known := had[eid]
		if !known || want == "" {
			want = actor
		}
		if by, _ := m["recordedBy"].(string); by != want {
			m["recordedBy"] = want
			changed = true
		}
	}
	if !changed {
		return nil, nil
	}
	return e.codec.Encode(doc)
}
