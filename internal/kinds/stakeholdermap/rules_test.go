package stakeholdermap

import (
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/kinds/kit"
)

// An entry names a resource or a beneficiary group, exactly one, each
// scored once, and its owner is never the map's own local form
// (TAXONOMY.md D42).
func TestAnEntryNamesAResourceOrAGroup(t *testing.T) {
	scope := map[string]any{"kind": "Project", "id": "p1"}
	check := func(entries ...any) []kit.Problem {
		return Rules(map[string]any{"spec": map[string]any{"scope": scope, "entries": entries}}, kit.RuleContext{})
	}
	if p := check(map[string]any{"resource": "r1"}, map[string]any{"group": "g1", "stake": "Wants shorter queues", "owner": map[string]any{"kind": "Resource", "id": "r2"}}); len(p) != 0 {
		t.Fatalf("a resource and a group: %+v", p)
	}
	for name, entries := range map[string][]any{
		"both":          {map[string]any{"resource": "r1", "group": "g1"}},
		"neither":       {map[string]any{"stake": "Something"}},
		"a group twice": {map[string]any{"group": "g1"}, map[string]any{"group": "g1"}},
		"a local owner": {map[string]any{"group": "g1", "owner": map[string]any{"local": "resources", "id": "lead"}}},
	} {
		if p := check(entries...); len(p) != 1 {
			t.Errorf("%s: %+v", name, p)
		}
	}
	// A resource and a group may share an id: they are different things.
	if p := check(map[string]any{"resource": "same"}, map[string]any{"group": "same"}); len(p) != 0 {
		t.Fatalf("a resource and a group sharing an id: %+v", p)
	}
}
