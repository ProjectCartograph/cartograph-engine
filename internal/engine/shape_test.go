package engine_test

import (
	"slices"
	"testing"
)

// The shape is read from the schema, so the engine and every interface
// agree on it. These pin the rules on real kinds: identified lists are
// keyed, prose is text, and ids, references and choices are scalars.
func TestShapeFollowsTheSchema(t *testing.T) {
	t.Parallel()
	e := newTestEngine(t)

	goal := e.Shape("Goal")
	if goal.ListKeys["/spec/keyResults"] != "id" {
		t.Errorf("Goal key results keyed by %q, want id", goal.ListKeys["/spec/keyResults"])
	}
	for _, p := range []string{"/metadata/name", "/spec/objective", "/spec/whyItMatters"} {
		if !slices.Contains(goal.Texts, p) {
			t.Errorf("Goal %s is not a text; prose merges character by character", p)
		}
	}
	for _, p := range []string{"/spec/level", "/spec/parent", "/apiVersion", "/kind"} {
		if slices.Contains(goal.Texts, p) {
			t.Errorf("Goal %s is a text; a choice, a reference or a constant is a scalar", p)
		}
	}

	project := e.Shape("Project")
	for list, key := range map[string]string{
		"/spec/deliverables":            "id",
		"/spec/objectives/*/keyResults": "id",
		"/spec/timeline/phases":         "id",
	} {
		if project.ListKeys[list] != key {
			t.Errorf("Project %s keyed by %q, want %q", list, project.ListKeys[list], key)
		}
	}
	if !slices.Contains(project.Texts, "/spec/deliverables/*/description") {
		t.Error("a deliverable's description is not a text")
	}

	if s := e.Shape("NoSuchKind"); len(s.Texts) != 0 || len(s.ListKeys) != 0 {
		t.Errorf("an unknown kind has a shape: %+v", s)
	}
}
