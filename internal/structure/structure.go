package structure

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/document"
)

// Package structure lays out a port's or a new piece of work's
// structure first (TAXONOMY.md D56). What a document calls a project, a
// workstream or a programme is evidence, not a structure: a person or an
// agent porting it, or starting something new, answers the same few
// questions about each piece of work it names, in order, and the answers
// settle what each piece is in Cartograph, where it sits, and the order to
// write it in. The questions are asked of everyone, so the structure is
// Cartograph's, not the document's.

// Piece is one piece of work a document or a person names, with
// the answers to the structure questions. Each answer is asked plainly in
// structureQuestions.
type Piece struct {
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
	// Change says, for a change of its own, what it changes that the work
	// depending on it needs: a baseline set, a system in use, a list
	// published. A piece that can only say what it hands over is an
	// output, not a project.
	Change string `json:"change,omitempty"`
	// Deliverable is, for a change of its own in a document with a
	// deliverable register, the code of the row that hands it over (D4):
	// a component project is work the document itself lists, never a
	// phase, a service or a theme renamed.
	Deliverable string `json:"deliverable,omitempty"`
	// None says every question was asked and none is yes: the piece is
	// work that ends with no other answer. A piece says it, or answers.
	None bool `json:"none,omitempty"`
}

// Keys are the keys a piece may carry: its name and the answers.
var Keys = map[string]bool{"name": true, "outOfScope": true, "policy": true, "ongoing": true, "runsToday": true, "groupsForFunding": true,
	"coordinatesProjects": true, "outputOf": true, "changeOfItsOwn": true, "dependedOnBy": true, "none": true, "change": true, "deliverable": true}

// Question is one question: as an agent is asked it, with what to send,
// and as a person is asked it, in plain words (#54).
type Question struct {
	Field    string `json:"field"`
	Question string `json:"question"`
	Then     string `json:"then"`
	Person   string `json:"person"`
}

// Questions are asked of every piece, in this order; the first
// yes decides.
var Questions = []Question{
	{"outOfScope", "Does another body lead and fund it to its own plan, so the work only depends on it or mentions it?", "Not a record here: a scope-out line of the work, and a dependency risk if the work waits on it.", "Does another organisation lead and pay for it, so your work only relies on it?"},
	{"policy", "Is it a standing policy or rule with no end date that the work puts into effect?", "A Goal at goal level, its horizon ending at its next review. The rules taking effect go in the project's notes.", "Is it a standing policy or rule, with no end date, that your work puts into effect?"},
	{"ongoing", "Does it keep running with no end date: a service or a function, whether it runs today or a project will set it up?", "An Operation: running if it runs today, otherwise planned and named by the project that sets it up as where it lands.", "Does it keep running with no end date, like a service or a team's regular work?"},
	{"groupsForFunding", "Does it group projects or programmes only to decide what to fund and in what order?", "A Portfolio, with its strategic objectives.", "Does it group projects or programmes only to decide which to fund, and in what order?"},
	{"coordinatesProjects", "Does it coordinate several projects, each with its own sponsor or budget, that together bring about one change?", "A Programme, with its theory of change; the projects are its components.", "Does it coordinate several projects, each with its own sponsor or budget, towards one change?"},
	{"changeOfItsOwn", "Does it bring about a change of its own that another piece of work depends on: a survey that sets a baseline, a system or portal people use, an app, a study? Yes even when it also hands over a report, a dataset or a list. Where the document has a deliverable register, give deliverable, the code of the row that hands it over (D4).", "A Project of its own, with one objective, listed as a component of each piece that depends on it.", "Does it make a change of its own that other work relies on, such as a survey that sets a baseline, a system people use, or a study?"},
	{"outputOf", "Is it an output another piece of work hands over: a document, materials, a toolkit, a training delivered, an event?", "A deliverable of that piece of work, whoever leads it.", "Is it something another piece of work hands over, such as a report, materials, a training or an event?"},
	{"", "None of these (answer none: true):", "A Project: work that ends, with one objective. A project the others are components of is the parent.", "None of these. It is work with a start and an end."},
}

// Placed is what one piece is, and where it goes.
type Placed struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	// Record is Kind/id for a piece that is a record of its own, with an
	// id generated for it: the work to pass to next, as it is.
	Record string   `json:"record,omitempty"`
	Where  string   `json:"where"`
	Of     []string `json:"of,omitempty"`
}

// Result is the answer for a whole document: each piece, the order to
// write the records in, and anything the answers leave inconsistent.
type Result struct {
	Pieces []Placed `json:"pieces"`
	Order  []string `json:"order"`
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
func Classify(pieces []Piece) Result {
	var out Result
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
	// Every piece is answered, or says none is yes: a piece sent with no
	// answer was never asked about, and would stand as a project of its own.
	if len(pieces) > 1 {
		for _, p := range pieces {
			if !answered(p) {
				out.Problems = append(out.Problems, fmt.Sprintf("%q has no answers: ask each question of it and give the yes answers, or none: true when no question is yes", p.Name))
			}
		}
	}
	for _, p := range pieces {
		// A workstream groups work; it is not a piece of it (D49).
		if isWorkstream(p.Name) {
			out.Problems = append(out.Problems, fmt.Sprintf("%q is a workstream, and Cartograph keeps no workstreams (TAXONOMY.md D49): send what it hands over as pieces "+
				"with outputOf the work, any piece inside it with a change of its own as a piece by itself, and leave the workstream out", p.Name))
		}
		if p.ChangeOfItsOwn && strings.TrimSpace(p.Change) == "" {
			out.Problems = append(out.Problems, fmt.Sprintf("%q is answered a change of its own but says no change: give change, what it changes that the work needing it "+
				"depends on (such as \"sets the baseline the targets are set from\"); if all it does is hand something over, it is outputOf that work", p.Name))
		}
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
		case p.ChangeOfItsOwn:
			// A change of its own is a project even when it also hands
			// something over (a survey's report, a portal's list): what it
			// is an output of is what depends on it.
			kindOf[i] = "Project"
			if parent := strings.TrimSpace(p.OutputOf); parent != "" && !containsFold(p.DependedOnBy, parent) {
				pieces[i].DependedOnBy = append(pieces[i].DependedOnBy, parent)
			}
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
			out.Pieces = append(out.Pieces, Placed{Name: p.Name, Kind: PieceDeliverable, Of: []string{p.OutputOf},
				Where: "A deliverable of " + p.OutputOf + ", with its owner, due date and acceptance."})
			continue
		case PieceScopeOut:
			out.Pieces = append(out.Pieces, Placed{Name: p.Name, Kind: PieceScopeOut,
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
				out.Problems = append(out.Problems, fmt.Sprintf("%q has a change of its own but names no piece that needs it. If it is the main piece of work, send %s; "+
					"if another piece needs it, send %s", p.Name, pieceJSON(p.Name, `"none":true`), pieceJSON(p.Name, `"changeOfItsOwn":true,"change":"<what it changes>","dependedOnBy":["<the piece that needs it>"]`)))
			}
		}
		out.Pieces = append(out.Pieces, Placed{Name: p.Name, Kind: kindOf[i], Of: of, Where: whereOf(kindOf[i], p, of)})
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
	// One piece of work at the top: projects nothing depends on and no
	// programme lists are each the whole of the work, and a document's
	// work has one whole. Several mean their links were not answered.
	var tops []string
	for i, p := range pieces {
		if kindOf[i] == "Project" && !listed[i] && !(p.ChangeOfItsOwn && len(p.DependedOnBy) > 0) {
			tops = append(tops, p.Name)
		}
	}
	programmes := 0
	for i := range pieces {
		if kindOf[i] == "Programme" || kindOf[i] == "Portfolio" {
			programmes++
		}
	}
	if len(tops) > 1 && programmes == 0 {
		// Say it with the pieces to send: the main piece is the one that
		// says none, else the first named.
		parent := tops[0]
		for _, p := range pieces {
			if p.None {
				for _, t := range tops {
					if t == p.Name {
						parent = t
					}
				}
			}
		}
		var fix []string
		for _, t := range tops {
			if t != parent {
				fix = append(fix, pieceJSON(t, fmt.Sprintf(`"changeOfItsOwn":true,"change":"<what it changes>","dependedOnBy":[%q]`, parent)))
			}
		}
		out.Problems = append(out.Problems, fmt.Sprintf("%s stand alone, each as the whole of the work, but a piece of work has one whole. If %q is the main piece, "+
			"send it as %s and the others as %s (or outputOf it, for an output it hands over, or ongoing, for a service that keeps running)",
			quoteList(tops), parent, pieceJSON(parent, `"none":true`), strings.Join(fix, " and ")))
	}
	out.Order = writingOrder(pieces, kindOf, components, &out.Problems)
	// Ids are generated, never derived from names (AGENTS.md).
	record := map[string]string{}
	for i := range out.Pieces {
		p := &out.Pieces[i]
		if p.Kind == PieceDeliverable || p.Kind == PieceScopeOut {
			continue
		}
		p.Record = p.Kind + "/" + strings.ToLower(p.Kind) + "-" + NewID()
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

func whereOf(kind string, p Piece, of []string) string {
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
func writingOrder(pieces []Piece, kindOf []string, components [][]int, problems *[]string) []string {
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

// NewID is a generated id part: a record's id never comes from its name.
func NewID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Drafts are the records a structure says to write, as first drafts in
// its order: each named, with a goal's level, an operation's status, the
// components each piece lists, each deliverable inside the work it is an
// output of, and each scope-out line in the work at the top. What only the
// documents can say is left for the checks to ask for, one at a time.
func (s Result) Drafts() []map[string]any {
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
					sp["deliverables"] = append(ds, map[string]any{"id": fmt.Sprintf("d%d", len(ds)+1), "name": document.Clip(p.Name, 60)})
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
				summary["scopeOut"] = append(out, document.Clip(p.Name, 60))
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

// answered reports whether a piece carries any answer, none included.
func answered(p Piece) bool {
	return p.None || p.OutOfScope || p.Policy || p.Ongoing || p.RunsToday || p.GroupsForFunding || p.CoordinatesProjects ||
		strings.TrimSpace(p.OutputOf) != "" || p.ChangeOfItsOwn || len(p.DependedOnBy) > 0
}

// quoteList is names quoted and joined: "a", "b" and "c".
func quoteList(names []string) string {
	q := make([]string, len(names))
	for i, n := range names {
		q[i] = fmt.Sprintf("%q", n)
	}
	if len(q) == 1 {
		return q[0]
	}
	return strings.Join(q[:len(q)-1], ", ") + " and " + q[len(q)-1]
}

// pieceJSON is a piece as an agent sends it, with the answers given.
func pieceJSON(name, answers string) string {
	return fmt.Sprintf(`{"name":%q,%s}`, name, answers)
}

// containsFold reports whether names holds name, ignoring case.
func containsFold(names []string, name string) bool {
	for _, n := range names {
		if strings.EqualFold(strings.TrimSpace(n), name) {
			return true
		}
	}
	return false
}

// isWorkstream reports whether a name is a workstream's: "Workstream 3",
// "WS2 Materials", "Materials (WS2)", "Materials workstream".
func isWorkstream(name string) bool {
	n := strings.ToLower(name)
	if strings.Contains(n, "workstream") || strings.Contains(n, "work stream") {
		return true
	}
	for _, w := range strings.FieldsFunc(n, func(r rune) bool { return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9') }) {
		if len(w) > 2 && w[:2] == "ws" && strings.Trim(w[2:], "0123456789") == "" {
			return true
		}
	}
	return false
}
