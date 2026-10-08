package engine_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
)

// A charter of the shape the agents found hardest: a standing policy, one
// project whose workstreams are partly outputs and partly work with a
// change of its own, the service the work hands over to, and a strand
// another body runs (TAXONOMY.md D56).
func TestClassifyPutsEachPieceWhereItBelongs(t *testing.T) {
	t.Parallel()
	s := engine.Classify([]engine.StructurePiece{
		{Name: "Produce standards", Policy: true},
		{Name: "Standards rollout"},
		{Name: "Grading handbook", OutputOf: "Standards rollout"},
		{Name: "Grader training", OutputOf: "Standards rollout"},
		{Name: "Baseline survey", ChangeOfItsOwn: true, DependedOnBy: []string{"Standards rollout"}},
		{Name: "Tablet app", ChangeOfItsOwn: true, DependedOnBy: []string{"Baseline survey"}},
		{Name: "Supplier portal", ChangeOfItsOwn: true, DependedOnBy: []string{"Standards rollout"}},
		{Name: "Compliance checks", Ongoing: true},
		{Name: "Farm supply scheme", OutOfScope: true},
	})
	if len(s.Problems) > 0 {
		t.Fatalf("problems %v", s.Problems)
	}
	kind := map[string]string{}
	for _, p := range s.Pieces {
		kind[p.Name] = p.Kind
	}
	want := map[string]string{
		"Produce standards": "Goal", "Standards rollout": "Project", "Grading handbook": "Deliverable", "Grader training": "Deliverable",
		"Baseline survey": "Project", "Tablet app": "Project", "Supplier portal": "Project", "Compliance checks": "Operation", "Farm supply scheme": "ScopeOut",
	}
	for n, k := range want {
		if kind[n] != k {
			t.Errorf("%s: %s, want %s", n, kind[n], k)
		}
	}
	// Goals, operations, then what is depended on before what depends on it.
	order := strings.Join(s.Order, " > ")
	if order != "Produce standards > Compliance checks > Tablet app > Baseline survey > Supplier portal > Standards rollout" {
		t.Errorf("order %s", order)
	}
}

func TestClassifySaysWhatIsInconsistent(t *testing.T) {
	t.Parallel()
	s := engine.Classify([]engine.StructurePiece{
		{Name: "Handbook", OutputOf: "Nowhere"},
		{Name: "Survey", ChangeOfItsOwn: true},
		{Name: "A", ChangeOfItsOwn: true, DependedOnBy: []string{"B"}},
		{Name: "B", ChangeOfItsOwn: true, DependedOnBy: []string{"A"}},
	})
	all := strings.Join(s.Problems, "\n")
	for _, want := range []string{`"Handbook" is an output of "Nowhere"`, `"Survey" has a change of its own but names nothing`, "depend on each other"} {
		if !strings.Contains(all, want) {
			t.Errorf("problems lack %q: %s", want, all)
		}
	}
}

// A programme lists the projects no other project lists.
func TestClassifyGivesAProgrammeItsProjects(t *testing.T) {
	t.Parallel()
	s := engine.Classify([]engine.StructurePiece{
		{Name: "Cleaner water", CoordinatesProjects: true},
		{Name: "Pipes"},
		{Name: "Treatment works"},
		{Name: "Meters", ChangeOfItsOwn: true, DependedOnBy: []string{"Pipes"}},
	})
	if got := strings.Join(s.Order, " > "); got != "Meters > Pipes > Treatment works > Cleaner water" {
		t.Errorf("order %s", got)
	}
}

// Every key structure accepts is an answer StructurePiece holds.
func TestStructureKeysAreThePieceFields(t *testing.T) {
	t.Parallel()
	b, _ := json.Marshal(engine.StructurePiece{Name: "x", OutOfScope: true, Policy: true, Ongoing: true, RunsToday: true, GroupsForFunding: true,
		CoordinatesProjects: true, OutputOf: "y", ChangeOfItsOwn: true, DependedOnBy: []string{"z"}})
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if len(m) != len(engine.StructureKeys) {
		t.Fatalf("piece fields %v, keys %v", m, engine.StructureKeys)
	}
	for k := range m {
		if !engine.StructureKeys[k] {
			t.Errorf("%s is a field structure refuses", k)
		}
	}
}
