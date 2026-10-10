package project

import (
	"strings"
	"testing"
)

func TestAffectsNamesEachSideOnceOnThisProject(t *testing.T) {
	t.Parallel()
	spec := map[string]any{
		"milestones":   []any{map[string]any{"id": "pilot"}},
		"deliverables": []any{map[string]any{"id": "app"}},
		"risks": []any{map[string]any{"type": "risk", "description": "late board", "affects": []any{
			map[string]any{"constraint": "schedule", "impact": "high", "on": "pilot"},
			map[string]any{"constraint": "scope", "on": "app"},
		}}},
	}
	if ps := affectsProblems(spec); len(ps) != 0 {
		t.Fatalf("a well placed risk: %+v", ps)
	}
	spec["risks"] = []any{map[string]any{"type": "risk", "description": "x", "affects": []any{
		map[string]any{"constraint": "cost", "on": "nowhere"},
		map[string]any{"constraint": "cost"},
	}}}
	ps := affectsProblems(spec)
	if len(ps) != 2 || !strings.HasSuffix(ps[0].Path, "/affects/0/on") || !strings.HasSuffix(ps[1].Path, "/affects/1/constraint") {
		t.Fatalf("problems %+v", ps)
	}
}
