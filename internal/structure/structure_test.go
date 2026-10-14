package structure_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/structure"
)

// A charter of the shape the agents found hardest: a standing policy, one
// project whose workstreams are partly outputs and partly work with a
// change of its own, the service the work hands over to, and a strand
// another body runs (TAXONOMY.md D56).
func TestClassifyPutsEachPieceWhereItBelongs(t *testing.T) {
	t.Parallel()
	s := structure.Classify([]structure.Piece{
		{Name: "Produce standards", Policy: true},
		{Name: "Standards rollout", None: true},
		{Name: "Grading handbook", OutputOf: "Standards rollout"},
		{Name: "Grader training", OutputOf: "Standards rollout"},
		{Name: "Baseline survey", ChangeOfItsOwn: true, Change: "sets the baseline", DependedOnBy: []string{"Standards rollout"}},
		{Name: "Tablet app", ChangeOfItsOwn: true, Change: "sets the baseline", DependedOnBy: []string{"Baseline survey"}},
		{Name: "Supplier portal", ChangeOfItsOwn: true, Change: "sets the baseline", DependedOnBy: []string{"Standards rollout"}},
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
	s := structure.Classify([]structure.Piece{
		{Name: "Handbook", OutputOf: "Nowhere"},
		{Name: "Survey", ChangeOfItsOwn: true, Change: "sets the baseline"},
		{Name: "A", ChangeOfItsOwn: true, Change: "sets the baseline", DependedOnBy: []string{"B"}},
		{Name: "B", ChangeOfItsOwn: true, Change: "sets the baseline", DependedOnBy: []string{"A"}},
	})
	all := strings.Join(s.Problems, "\n")
	for _, want := range []string{`"Handbook" is an output of "Nowhere"`, `"Survey" has a change of its own but names no piece that needs it`, "depend on each other"} {
		if !strings.Contains(all, want) {
			t.Errorf("problems lack %q: %s", want, all)
		}
	}
}

// A programme lists the projects no other project lists.
func TestClassifyGivesAProgrammeItsProjects(t *testing.T) {
	t.Parallel()
	s := structure.Classify([]structure.Piece{
		{Name: "Cleaner water", CoordinatesProjects: true},
		{Name: "Pipes"},
		{Name: "Treatment works"},
		{Name: "Meters", ChangeOfItsOwn: true, Change: "sets the baseline", DependedOnBy: []string{"Pipes"}},
	})
	if got := strings.Join(s.Order, " > "); got != "Meters > Pipes > Treatment works > Cleaner water" {
		t.Errorf("order %s", got)
	}
}

// Every key structure accepts is an answer StructurePiece holds.
func TestStructureKeysAreThePieceFields(t *testing.T) {
	t.Parallel()
	b, _ := json.Marshal(structure.Piece{Name: "x", OutOfScope: true, Policy: true, Ongoing: true, RunsToday: true, GroupsForFunding: true,
		CoordinatesProjects: true, OutputOf: "y", ChangeOfItsOwn: true, Change: "sets the baseline", DependedOnBy: []string{"z"}, None: true, Deliverable: "D4"})
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if len(m) != len(structure.Keys) {
		t.Fatalf("piece fields %v, keys %v", m, structure.Keys)
	}
	for k := range m {
		if !structure.Keys[k] {
			t.Errorf("%s is a field structure refuses", k)
		}
	}
}

// A piece with no answer was never asked about, and several projects each
// standing as the whole of the work mean their links were not answered:
// both are problems, so a port is never laid out as unrelated projects.
func TestStructureRefusesUnansweredAndUnlinkedPieces(t *testing.T) {
	t.Parallel()
	s := structure.Classify([]structure.Piece{{Name: "Rollout"}, {Name: "Survey"}, {Name: "Checks", None: true}})
	text := strings.Join(s.Problems, "\n")
	if !strings.Contains(text, `"Rollout" has no answers`) || !strings.Contains(text, `"Survey" has no answers`) || !strings.Contains(text, "stand alone") {
		t.Fatalf("problems: %v", s.Problems)
	}
	ok := structure.Classify([]structure.Piece{{Name: "Rollout", None: true}, {Name: "Survey", ChangeOfItsOwn: true, Change: "sets the baseline", DependedOnBy: []string{"Rollout"}}, {Name: "Checks", Ongoing: true}})
	if len(ok.Problems) > 0 {
		t.Fatalf("a linked port: %v", ok.Problems)
	}
}

// A survey that sets a baseline is a change of its own even when it also
// hands over a report: asked first, it is a component project, the piece
// it was said to be an output of depending on it.
func TestAChangeOfItsOwnIsAskedBeforeAnOutput(t *testing.T) {
	t.Parallel()
	s := structure.Classify([]structure.Piece{
		{Name: "Rollout", None: true},
		{Name: "Baseline survey", ChangeOfItsOwn: true, Change: "sets the baseline", OutputOf: "Rollout"},
		{Name: "Handbook", OutputOf: "Rollout"},
	})
	if len(s.Problems) > 0 {
		t.Fatalf("problems: %v", s.Problems)
	}
	kinds := map[string]string{}
	for _, p := range s.Pieces {
		kinds[p.Name] = p.Kind
	}
	if kinds["Baseline survey"] != "Project" || kinds["Handbook"] != structure.PieceDeliverable {
		t.Fatalf("pieces: %+v", s.Pieces)
	}
}

// A workstream is not a piece of work (D49), and a change of its own says
// what it changes: neither is taken on a bare yes.
func TestAWorkstreamIsRefusedAndAChangeIsSaid(t *testing.T) {
	t.Parallel()
	s := structure.Classify([]structure.Piece{
		{Name: "Rollout", None: true},
		{Name: "WS2 Technical materials", ChangeOfItsOwn: true, DependedOnBy: []string{"Rollout"}},
		{Name: "Survey", ChangeOfItsOwn: true, DependedOnBy: []string{"Rollout"}},
		{Name: "Governance and coordination (WS1)", OutputOf: "Rollout"},
	})
	text := strings.Join(s.Problems, "\n")
	if !strings.Contains(text, `"Governance and coordination (WS1)" is a workstream`) {
		t.Errorf("a workstream named in brackets: %v", s.Problems)
	}
	if !strings.Contains(text, `"WS2 Technical materials" is a workstream`) || !strings.Contains(text, `"Survey" is answered a change of its own but says no change`) {
		t.Fatalf("problems: %v", s.Problems)
	}
}

// A person is asked each question in plain words: no code from the
// taxonomy, no field to send, nothing written for an agent (#54).
func TestEveryQuestionHasAPersonsWording(t *testing.T) {
	t.Parallel()
	for _, q := range structure.Questions {
		if q.Person == "" {
			t.Errorf("%q has no wording for a person", q.Field)
		}
		for _, agentOnly := range []string{"(D", "answer ", "true", "give deliverable", ":"} {
			if strings.Contains(q.Person, agentOnly) {
				t.Errorf("%q asks a person with %q: %s", q.Field, agentOnly, q.Person)
			}
		}
	}
}
