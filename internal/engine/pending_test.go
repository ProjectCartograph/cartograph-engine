package engine_test

import (
	"context"
	"strings"
	"testing"
)

// A person who reaches a reference before what it names exists records a
// placeholder and carries on (TAXONOMY.md D31). It must stand for a real
// reference of the right kind that is still empty; the record's checks then
// say what it waits on, on the step that holds the field, and a handoff
// still waits for the field itself.
func TestAPlaceholderHoldsTheReferenceUntilItIsMade(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	ctx := context.Background()
	project := func(id, meta, spec string) []byte {
		return []byte(strings.Replace(projectYAML(id, spec), "  name: P\nspec:", "  name: P\n"+meta+"spec:", 1))
	}
	waiting := "  pending:\n    - {path: /spec/operation, kind: Operation, name: Quality Check Service}\n"
	problems, err := e.Validate(ctx, "Project", project("p-wait", waiting, ""))
	if err != nil || len(problems) != 0 {
		t.Fatalf("a placeholder for the service a project lands in: %v %v", problems, err)
	}
	mustCommit(t, e, "Project", "p-wait", "local", string(project("p-wait", waiting, "")))
	checks, err := e.ProjectChecks(ctx, "p-wait", false)
	if err != nil {
		t.Fatal(err)
	}
	pending := checksByID(checks.Items)["pending"]
	if pending.State != "warn" || pending.Section != "landing" || !strings.Contains(pending.Message, "Quality Check Service") {
		t.Fatalf("the placeholder's check: %+v", pending)
	}
	if landing := checksByID(checks.Items)["landing-operation"]; landing.State != "block" {
		t.Fatalf("a placeholder does not make the reference: %+v", landing)
	}

	for name, c := range map[string]struct{ meta, spec, want string }{
		"not a reference": {"  pending:\n    - {path: /spec/team2, kind: Team, name: X}\n", "", "is not a reference"},
		"the wrong kind":  {"  pending:\n    - {path: /spec/operation, kind: Team, name: X}\n", "", "names a Operation"},
		"already made":    {"  pending:\n    - {path: /spec/team, kind: Team, name: X}\n", "", "already filled in"},
	} {
		problems, err := e.Validate(ctx, "Project", project("p-bad", c.meta, c.spec))
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, p := range problems {
			found = found || strings.Contains(p.Message, c.want)
		}
		if !found {
			t.Errorf("%s: %v", name, problems)
		}
	}
}
