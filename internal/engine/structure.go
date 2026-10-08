package engine

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// Structure first (TAXONOMY.md D56). What a document calls a project, a
// workstream or a programme is evidence, not a structure: a person or an
// agent porting it, or starting something new, answers the same few
// questions about each piece of work it names, in order, and the answers
// settle what each piece is in Cartograph, where it sits, and the order to
// write it in. The questions are asked of everyone, so the structure is
// Cartograph's, not the document's.

// StructurePiece is one piece of work a document or a person names, with
// the answers to the structure questions. Each answer is asked plainly in
// structureQuestions.
type StructurePiece struct {
	Name string `json:"name"`
	// OutOfScope: another body leads and funds it to its own plan, and the
	// work only depends on it or mentions it.
	OutOfScope bool `json:"outOfScope,omitempty"`
	// Policy: it is a standing policy or rule with no end date, which the
	// work puts into effect.
	Policy bool `json:"policy,omitempty"`
	// Ongoing: it keeps running with no end date, a service or function
	// (whether it runs today or a project will set it up).
	Ongoing bool `json:"ongoing,omitempty"`
	// RunsToday: for an ongoing piece, it runs already.
	RunsToday bool `json:"runsToday,omitempty"`
	// GroupsForFunding: it groups projects or programmes to decide what to
	// fund and in what order, with no causal link between them.
	GroupsForFunding bool `json:"groupsForFunding,omitempty"`
	// CoordinatesProjects: it coordinates several projects, each with its
	// own sponsor or budget, that together bring about one change.
	CoordinatesProjects bool `json:"coordinatesProjects,omitempty"`
	// OutputOf names the piece whose output this is: a document,
	// materials, a toolkit, a training delivered, an event.
	OutputOf string `json:"outputOf,omitempty"`
	// ChangeOfItsOwn: its result is a change of its own (a survey that sets
	// a baseline, a system or portal people use, an app, a study), not an
	// output handed over.
	ChangeOfItsOwn bool `json:"changeOfItsOwn,omitempty"`
	// DependedOnBy names the pieces that depend on it.
	DependedOnBy []string `json:"dependedOnBy,omitempty"`
}

// StructureKeys are the keys a piece may carry: its name and the answers.
var StructureKeys = map[string]bool{"name": true, "outOfScope": true, "policy": true, "ongoing": true, "runsToday": true, "groupsForFunding": true,
	"coordinatesProjects": true, "outputOf": true, "changeOfItsOwn": true, "dependedOnBy": true}

// StructureQuestion is one question, as people and agents are asked it.
type StructureQuestion struct {
	Field    string `json:"field"`
	Question string `json:"question"`
	Then     string `json:"then"`
}

// StructureQuestions are asked of every piece, in this order; the first
// yes decides.
var StructureQuestions = []StructureQuestion{
	{"outOfScope", "Does another body lead and fund it to its own plan, so the work only depends on it or mentions it?", "Not a record here: a scope-out line of the work, and a dependency risk if the work waits on it."},
	{"policy", "Is it a standing policy or rule with no end date that the work puts into effect?", "A Goal at goal level, its horizon ending at its next review. The rules taking effect go in the project's notes."},
	{"ongoing", "Does it keep running with no end date: a service or a function, whether it runs today or a project will set it up?", "An Operation: running if it runs today, otherwise planned and named by the project that sets it up as where it lands."},
	{"groupsForFunding", "Does it group projects or programmes only to decide what to fund and in what order?", "A Portfolio, with its strategic objectives."},
	{"coordinatesProjects", "Does it coordinate several projects, each with its own sponsor or budget, that together bring about one change?", "A Programme, with its theory of change; the projects are its components."},
	{"outputOf", "Is it an output another piece of work hands over: a document, materials, a toolkit, a training delivered, an event?", "A deliverable of that piece of work, whoever leads it."},
	{"changeOfItsOwn", "Does it bring about a change of its own that another piece of work depends on: a survey that sets a baseline, a system or portal people use, an app, a study?", "A Project of its own, with one objective, listed as a component of each piece that depends on it."},
	{"", "None of these:", "A Project: work that ends, with one objective. A project the others are components of is the parent."},
}

// StructuredPiece is what one piece is, and where it goes.
type StructuredPiece struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	// Record is Kind/id for a piece that is a record of its own, with an
	// id generated for it: the work to pass to next, as it is.
	Record string   `json:"record,omitempty"`
	Where  string   `json:"where"`
	Of     []string `json:"of,omitempty"`
}

// Structure is the answer for a whole document: each piece, the order to
// write the records in, and anything the answers leave inconsistent.
type Structure struct {
	Pieces []StructuredPiece `json:"pieces"`
	Order  []string          `json:"order"`
	// Work is every record to write, as Kind/id, in the order to write
	// them: what next takes as work, unchanged.
	Work     []string `json:"work"`
	Problems []string `json:"problems,omitempty"`
	Next     string   `json:"next"`
}

// Kinds a structured piece may be besides a kind of record.
const (
	PieceDeliverable = "Deliverable"
	PieceScopeOut    = "ScopeOut"
)

// Classify answers what each piece is from its answers, the first yes
// deciding, and orders the records for writing: goals, then operations,
// then projects with the ones depended on first, then programmes, then
// portfolios. Deliverables and scope-out lines are written inside the
// project they belong to.
func Classify(pieces []StructurePiece) Structure {
	var out Structure
	byName := map[string]int{}
	for i, p := range pieces {
		key := strings.ToLower(strings.TrimSpace(p.Name))
		if key == "" {
			out.Problems = append(out.Problems, fmt.Sprintf("piece %d has no name", i+1))
			continue
		}
		if _, dup := byName[key]; dup {
			out.Problems = append(out.Problems, fmt.Sprintf("%q is named twice: name each piece once", p.Name))
		}
		byName[key] = i
	}
	kindOf := make([]string, len(pieces))
	for i, p := range pieces {
		switch {
		case p.OutOfScope:
			kindOf[i] = PieceScopeOut
		case p.Policy:
			kindOf[i] = "Goal"
		case p.Ongoing:
			kindOf[i] = "Operation"
		case p.GroupsForFunding:
			kindOf[i] = "Portfolio"
		case p.CoordinatesProjects:
			kindOf[i] = "Programme"
		case strings.TrimSpace(p.OutputOf) != "":
			kindOf[i] = PieceDeliverable
		default:
			kindOf[i] = "Project"
		}
	}
	find := func(name string) (int, bool) {
		i, ok := byName[strings.ToLower(strings.TrimSpace(name))]
		return i, ok
	}
	work := func(i int) bool { return kindOf[i] == "Project" || kindOf[i] == "Programme" }
	// Depends on, by piece: the components each piece lists.
	components := make([][]int, len(pieces))
	for i, p := range pieces {
		switch kindOf[i] {
		case PieceDeliverable:
			j, ok := find(p.OutputOf)
			if !ok || !work(j) {
				out.Problems = append(out.Problems, fmt.Sprintf("%q is an output of %q, which is not a project or programme among the pieces", p.Name, p.OutputOf))
			}
			out.Pieces = append(out.Pieces, StructuredPiece{Name: p.Name, Kind: PieceDeliverable, Of: []string{p.OutputOf},
				Where: "A deliverable of " + p.OutputOf + ", with its owner, due date and acceptance."})
			continue
		case PieceScopeOut:
			out.Pieces = append(out.Pieces, StructuredPiece{Name: p.Name, Kind: PieceScopeOut,
				Where: "Not a record: a scope-out line of the work that mentions it, and a dependency risk if the work waits on it."})
			continue
		}
		var of []string
		if kindOf[i] == "Project" && p.ChangeOfItsOwn {
			for _, d := range p.DependedOnBy {
				j, ok := find(d)
				if !ok || !work(j) {
					out.Problems = append(out.Problems, fmt.Sprintf("%q is depended on by %q, which is not a project or programme among the pieces", p.Name, d))
					continue
				}
				components[j] = append(components[j], i)
				of = append(of, pieces[j].Name)
			}
			if len(of) == 0 {
				out.Problems = append(out.Problems, fmt.Sprintf("%q has a change of its own but names nothing that depends on it: say which piece depends on it, or it is the parent project", p.Name))
			}
		}
		out.Pieces = append(out.Pieces, StructuredPiece{Name: p.Name, Kind: kindOf[i], Of: of, Where: whereOf(kindOf[i], p, of)})
	}
	// A programme lists the projects nothing else lists.
	listed := map[int]bool{}
	for _, cs := range components {
		for _, c := range cs {
			listed[c] = true
		}
	}
	for i := range pieces {
		if kindOf[i] != "Programme" {
			continue
		}
		for j := range pieces {
			if kindOf[j] == "Project" && !listed[j] {
				components[i] = append(components[i], j)
			}
		}
	}
	out.Order = writingOrder(pieces, kindOf, components, &out.Problems)
	// Ids are generated, never derived from names (AGENTS.md).
	record := map[string]string{}
	for i := range out.Pieces {
		p := &out.Pieces[i]
		if p.Kind == PieceDeliverable || p.Kind == PieceScopeOut {
			continue
		}
		p.Record = p.Kind + "/" + strings.ToLower(p.Kind) + "-" + shortID()
		record[strings.ToLower(strings.TrimSpace(p.Name))] = p.Record
	}
	out.Work = []string{}
	for _, name := range out.Order {
		if r, ok := record[strings.ToLower(strings.TrimSpace(name))]; ok {
			out.Work = append(out.Work, r)
		}
	}
	if len(out.Problems) > 0 {
		out.Next = "Fix every problem above and call structure again until there are none."
		return out
	}
	out.Next = "Call start_work with a title and these same pieces: it drafts every record here, named and linked, with each deliverable " +
		"in its project and each scope-out line in the work at the top. Then fill them in from the documents as its answer says, passing the work " +
		"list to every save, until every check is met; then propose."
	return out
}

func whereOf(kind string, p StructurePiece, of []string) string {
	switch kind {
	case "Goal":
		return "A Goal at goal level: the standing policy, its horizon ending at its next review; the rules taking effect go in the project's notes."
	case "Operation":
		if p.RunsToday {
			return "An Operation with status running, recorded as it stands."
		}
		return "An Operation with status planned, written before the project that sets it up, which names it as where it lands."
	case "Portfolio":
		return "A Portfolio, with its strategic objectives; its decisions are written after the work it holds."
	case "Programme":
		return "A Programme with its theory of change; it lists the projects as its components."
	}
	if len(of) > 0 {
		return "A Project of its own with one objective, listed as a component of " + strings.Join(of, " and ") + "."
	}
	return "A Project with one objective; it lists as components the projects that depend-on answers name."
}

// writingOrder is the order to write records in: goals, operations,
// projects with the ones depended on first, programmes, portfolios. A
// loop of dependencies is a problem, and its pieces go last.
func writingOrder(pieces []StructurePiece, kindOf []string, components [][]int, problems *[]string) []string {
	rank := map[string]int{"Goal": 0, "Operation": 1, "Project": 2, "Programme": 3, "Portfolio": 4}
	var order []string
	for _, k := range []string{"Goal", "Operation"} {
		for i := range pieces {
			if kindOf[i] == k {
				order = append(order, pieces[i].Name)
			}
		}
	}
	state := make([]int, len(pieces)) // 0 new, 1 visiting, 2 done
	var visit func(i int) bool
	var projects []string
	visit = func(i int) bool {
		switch state[i] {
		case 1:
			return false
		case 2:
			return true
		}
		state[i] = 1
		for _, c := range components[i] {
			if !visit(c) {
				*problems = append(*problems, fmt.Sprintf("%q and %q depend on each other: one of them must not list the other", pieces[i].Name, pieces[c].Name))
			}
		}
		state[i] = 2
		if kindOf[i] == "Project" {
			projects = append(projects, pieces[i].Name)
		}
		return true
	}
	idx := make([]int, 0, len(pieces))
	for i := range pieces {
		if kindOf[i] == "Project" || kindOf[i] == "Programme" {
			idx = append(idx, i)
		}
	}
	sort.SliceStable(idx, func(a, b int) bool { return rank[kindOf[idx[a]]] < rank[kindOf[idx[b]]] })
	for _, i := range idx {
		visit(i)
	}
	order = append(order, projects...)
	for _, k := range []string{"Programme", "Portfolio"} {
		for i := range pieces {
			if kindOf[i] == k {
				order = append(order, pieces[i].Name)
			}
		}
	}
	return order
}

// shortID is a generated id part: a record's id never comes from its name.
func shortID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Drafts are the records a structure says to write, as first drafts in
// its order: each named, with a goal's level, an operation's status, the
// components each piece lists, each deliverable inside the work it is an
// output of, and each scope-out line in the work at the top. What only the
// documents can say is left for the checks to ask for, one at a time.
func (s Structure) Drafts() []map[string]any {
	recordOf := map[string]string{}
	for _, p := range s.Pieces {
		if p.Record != "" {
			recordOf[strings.ToLower(strings.TrimSpace(p.Name))] = p.Record
		}
	}
	idOf := func(name string) (string, string, bool) {
		r, ok := recordOf[strings.ToLower(strings.TrimSpace(name))]
		if !ok {
			return "", "", false
		}
		k, id, _ := strings.Cut(r, "/")
		return k, id, true
	}
	drafts := map[string]map[string]any{}
	specOfDraft := func(r string) map[string]any {
		d := drafts[r]
		sp, _ := d["spec"].(map[string]any)
		return sp
	}
	for _, p := range s.Pieces {
		if p.Record == "" {
			continue
		}
		kind, id, _ := strings.Cut(p.Record, "/")
		spec := map[string]any{}
		switch kind {
		case "Goal":
			spec["level"] = "goal"
		case "Operation":
			spec["status"] = "planned"
			if strings.Contains(p.Where, "running") {
				spec["status"] = "running"
			}
		}
		drafts[p.Record] = map[string]any{"apiVersion": "cartograph/v1", "kind": kind, "metadata": map[string]any{"id": id, "name": p.Name}, "spec": spec}
	}
	var tops []string
	for _, p := range s.Pieces {
		if p.Kind == "Project" && len(p.Of) == 0 {
			tops = append(tops, p.Record)
		}
	}
	for _, p := range s.Pieces {
		switch p.Kind {
		case "Project":
			for _, of := range p.Of {
				if k, _, ok := idOf(of); ok && (k == "Project" || k == "Programme") {
					_, id, _ := strings.Cut(p.Record, "/")
					sp := specOfDraft(recordOf[strings.ToLower(strings.TrimSpace(of))])
					cs, _ := sp["components"].([]any)
					sp["components"] = append(cs, map[string]any{"kind": "Project", "id": id})
				}
			}
		case PieceDeliverable:
			for _, of := range p.Of {
				if k, _, ok := idOf(of); ok && k == "Project" {
					sp := specOfDraft(recordOf[strings.ToLower(strings.TrimSpace(of))])
					ds, _ := sp["deliverables"].([]any)
					sp["deliverables"] = append(ds, map[string]any{"id": fmt.Sprintf("d%d", len(ds)+1), "name": clip(p.Name, 60)})
				}
			}
		case PieceScopeOut:
			for _, r := range tops {
				sp := specOfDraft(r)
				summary, _ := sp["summary"].(map[string]any)
				if summary == nil {
					summary = map[string]any{}
					sp["summary"] = summary
				}
				out, _ := summary["scopeOut"].([]any)
				summary["scopeOut"] = append(out, clip(p.Name, 60))
			}
		}
	}
	out := make([]map[string]any, 0, len(s.Work))
	for _, r := range s.Work {
		if d, ok := drafts[r]; ok {
			out = append(out, d)
		}
	}
	return out
}

// clip shortens text to n characters, whole words where it can.
func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len([]rune(s)) <= n {
		return s
	}
	r := []rune(s)[:n]
	if i := strings.LastIndex(string(r), " "); i > n/2 {
		return string(r[:len([]rune(string(r)[:i]))])
	}
	return string(r)
}
