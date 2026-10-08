package engine

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/document"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/structure"
)

// Porting a document is an application service (TAXONOMY.md D56, docs/
// adr/0027): the document is brought whole, its pieces of work are laid
// out as records, its registers are written into them as the porting map
// says, and every record is then written from it in one pass. Whoever
// drives it, an agent through MCP or anything else, gets the same port.

// WholeFile refuses a document's text that is not its whole file: a
// summary or an excerpt loses the registers and sections a port is
// written from, and no later step can tell. The file's size is given
// beside the text; line endings and a final newline may differ.
func WholeFile(text string, fileSize int) error {
	if fileSize <= 0 {
		return fmt.Errorf("%w: give fileSize, the file's size as one number of bytes (os.path.getsize(path) in a script), beside its text", ErrBadEdit)
	}
	got := len(text)
	slack := fileSize/50 + 64
	if got < fileSize-slack || got > fileSize+slack {
		return fmt.Errorf("%w: the text is %d bytes and the file %d: send the file's whole text, read straight from it by a script "+
			"(json.dump({\"title\": ..., \"text\": open(path).read(), \"fileSize\": os.path.getsize(path)})), never a summary or a part", ErrBadEdit, got, fileSize)
	}
	return nil
}

// writtenFromTheDocument are checks on what a document itself states: its
// objective, problem and change, who it serves, its scope, what it hands
// over, how success is known, its schedule and what authorises it. A
// writer working from documents without its person writes these; it
// cannot leave them open as not available (docs/adr/0027), so a port is
// never proposed hollow with its content waived.
var writtenFromTheDocument = map[string]bool{
	"goals-objective": true, "aim-problem-change": true, "beneficiaries-named": true, "scope-in": true,
	"deliverables-count": true, "success-criteria": true, "timeline-start-phases": true, "aim-mandate": true,
}

// WrittenFromTheDocument reports whether a check is on what a document
// itself states, which a port writes and never waives.
func WrittenFromTheDocument(check string) bool { return writtenFromTheDocument[check] }

// MayLeave refuses leaving open, with no person to ask, a check the
// document answers.
func MayLeave(check, asked string) error {
	if writtenFromTheDocument[check] && strings.EqualFold(strings.TrimSpace(asked), "not available") {
		return fmt.Errorf("%w: %s is what the document itself says: write it from the document, rather than leave it open. "+
			"If the document truly does not say it, do not propose: tell your person what it lacks", ErrBadEdit, check)
	}
	return nil
}

// Draft is a record a port drafted, as it was saved.
type Draft struct {
	Kind, ID string
	Text     []byte
}

// RegisterResult is what a port wrote from one of the document's
// registers.
type RegisterResult struct {
	Section   string    `json:"section"`
	Heading   string    `json:"heading,omitempty"`
	Field     string    `json:"field"`
	Rows      int       `json:"rows"`
	Added     int       `json:"added"`
	Refused   []Problem `json:"refused,omitempty"`
	Drafted   int       `json:"drafted,omitempty"`
	NotPorted []string  `json:"notPorted,omitempty"`
	Error     string    `json:"error,omitempty"`
}

// FillItem is one thing a record still needs: the check it meets or the
// field its schema requires, what to write, and the document's sections
// that say it.
type FillItem struct {
	Check string      `json:"check"`
	Do    string      `json:"do"`
	Field string      `json:"field,omitempty"`
	Read  []string    `json:"read,omitempty"`
	Shape *FieldShape `json:"shape,omitempty"`
}

// RecordFill is everything one record of a port still needs.
type RecordFill struct {
	Record string     `json:"record"`
	Name   string     `json:"name,omitempty"`
	Fill   []FillItem `json:"fill"`
}

// PortLayout is a port's pieces laid out: the structure, or the problems
// that kept it from being drafted; the records drafted; the registers
// written; and what each record still needs.
type PortLayout struct {
	Structure structure.Result
	Problems  []string
	Drafts    []Draft
	Registers []RegisterResult
	Records   []RecordFill
}

// PortPieces lays out a port's pieces of work in its change set: it
// classifies them (refusing a workstream of the document's, or a
// component that is no row of its deliverable register), drafts every
// record, and writes every register the document has into the main
// project, a component's own rows into the component.
func (e *Engine) PortPieces(ctx context.Context, set string, src Source, pieces []structure.Piece) (PortLayout, error) {
	var out PortLayout
	st := structure.Classify(pieces)
	// The document's own workstream plan says which names are workstreams:
	// none of them is a piece (D49), however it is called. A service, an
	// output or work another body leads may carry the name; a project may
	// not.
	for _, p := range pieces {
		if p.Ongoing || p.OutOfScope || p.Policy || strings.TrimSpace(p.OutputOf) != "" {
			continue
		}
		for _, ws := range document.WorkstreamNames(src.Sections) {
			// The same name, spelt otherwise: an exact rule, so a refusal.
			if sameName(p.Name, ws) {
				st.Problems = append(st.Problems, fmt.Sprintf("%q is the document's workstream %q, and Cartograph keeps no workstreams (TAXONOMY.md D49): "+
					"send what it hands over as pieces with outputOf the work, and each piece inside it with a change of its own (a survey, a system) as a piece by itself", p.Name, ws))
				break
			}
		}
	}
	st.Problems = append(st.Problems, e.componentRows(ctx, set, src, pieces)...)
	out.Structure = st
	if len(st.Problems) > 0 {
		out.Problems = st.Problems
		return out, nil
	}
	for _, d := range st.Drafts() {
		kind, _ := d["kind"].(string)
		id, _ := d["metadata"].(map[string]any)["id"].(string)
		if _, drafted, _ := e.ChangeSetText(ctx, set, kind, id); drafted {
			continue
		}
		text, err := e.codec.Encode(d)
		if err != nil {
			return out, err
		}
		if err := e.SaveInChangeSet(ctx, set, kind, id, text); err != nil {
			return out, err
		}
		out.Drafts = append(out.Drafts, Draft{Kind: kind, ID: id, Text: text})
	}
	// The main project takes the document's registers; each component its
	// own deliverable row, by the code its piece gave.
	main := ""
	recordOf := map[string]string{}
	for _, p := range st.Pieces {
		recordOf[p.Name] = p.Record
		if main == "" && p.Kind == "Project" && len(p.Of) == 0 {
			main = p.Record
		}
	}
	route := map[string]string{}
	for _, p := range pieces {
		if code := strings.ToLower(strings.TrimSpace(p.Deliverable)); code != "" && recordOf[p.Name] != "" {
			route[code] = recordOf[p.Name]
		}
	}
	if mk, mid, ok := strings.Cut(main, "/"); ok {
		for _, sec := range src.Sections {
			for _, f := range sec.Feeds {
				if f != "/spec/milestones" && f != "/spec/deliverables" && f != "/spec/risks" && f != "/spec/kpis" {
					continue
				}
				reg, err := e.RegisterInto(ctx, set, mk, mid, sec.ID, f, route)
				if err != nil {
					out.Registers = append(out.Registers, RegisterResult{Section: sec.ID, Field: f, Error: err.Error()})
					continue
				}
				if reg.Rows == 0 {
					continue
				}
				reg.Heading = sec.Heading
				out.Registers = append(out.Registers, reg)
			}
		}
	}
	ctx, err := e.InChangeSet(ctx, set)
	if err != nil {
		return out, err
	}
	all, err := e.portWork(ctx, set, st.Work)
	if err != nil {
		return out, err
	}
	out.Records, err = e.PortFills(ctx, set, all)
	return out, err
}

// RegisterInto writes a section's register into a record's list, row by
// row as an agent's write is applied: the rows the porting map does not
// port are answered under NotPorted with the reason, a row a component
// owns (by route, its code to the component) goes into the component,
// and a row's refused cells are dropped so the rest of it is kept.
func (e *Engine) RegisterInto(ctx context.Context, set, kind, id, section, field string, route map[string]string) (RegisterResult, error) {
	res := RegisterResult{Section: section, Field: field}
	items, err := e.RegisterOf(ctx, set, section, field)
	if err != nil {
		return res, err
	}
	res.Rows = len(items)
	main, mainID := kind, id
	for _, it := range items {
		// What the rows before drafted is in play for this one, so a body
		// named on ten rows is one record, not ten.
		if inSet, err := e.InChangeSet(ctx, set); err == nil {
			ctx = inSet
		}
		rowID, _ := it["id"].(string)
		if why, skip := it["_skip"].(string); skip {
			res.NotPorted = append(res.NotPorted, strings.ToUpper(rowID)+": "+why)
			continue
		}
		kind, id = main, mainID
		if to, ok := route[rowID]; ok {
			kind, id, _ = strings.Cut(to, "/")
		}
		extra, _ := it["_kpi"].(map[string]any)
		delete(it, "_kpi")
		team, _ := extra["_team"].(string)
		delete(extra, "_team")
		r, created, err := e.WriteFields(ctx, set, kind, id, map[string]any{field + "/-": it}, nil, true)
		// A KPI drafted from the row gets what the row says of it: its
		// source and its cycle, found or drafted by name, the source kept
		// by the body that owns the indicator.
		if err == nil && len(extra) > 0 {
			for _, d := range created {
				if k, kid, ok := strings.Cut(d, "/"); ok && k == "KPI" {
					more, moreCreated, _ := e.WriteFields(ctx, set, "KPI", kid, extra, nil, true)
					r = append(r, more...)
					for _, m := range moreCreated {
						if mk, mid, ok := strings.Cut(m, "/"); ok && mk == "DataSource" && team != "" {
							teamed, teamCreated, _ := e.WriteFields(ctx, set, "DataSource", mid, map[string]any{"/spec/team": team}, nil, true)
							r = append(r, teamed...)
							created = append(created, teamCreated...)
						}
					}
					created = append(created, moreCreated...)
					break
				}
			}
		}
		var invalid *ValidationError
		if errors.As(err, &invalid) {
			// A row is worth more than a cell: the cells refused (an owner
			// named by person, a date that is not one) are dropped and the
			// row tried again, the refusal said.
			trimmed := map[string]any{}
			for k, v := range it {
				trimmed[k] = v
			}
			dropped := false
			for _, p := range invalid.Problems {
				segs := strings.Split(strings.TrimPrefix(p.Path, field+"/"), "/")
				if len(segs) >= 2 {
					if _, ok := trimmed[segs[1]]; ok && segs[1] != "id" && segs[1] != "name" && segs[1] != "description" && segs[1] != "kpi" {
						delete(trimmed, segs[1])
						dropped = true
					}
				}
			}
			if dropped {
				res.Refused = append(res.Refused, invalid.Problems...)
				r, created, err = e.WriteFields(ctx, set, kind, id, map[string]any{field + "/-": trimmed}, nil, true)
			}
		}
		if err != nil {
			if errors.As(err, &invalid) {
				res.Refused = append(res.Refused, invalid.Problems...)
				continue
			}
			return res, err
		}
		res.Refused = append(res.Refused, r...)
		for _, d := range created {
			if !strings.HasPrefix(d, "cut: ") && !strings.HasPrefix(d, "refused: ") {
				res.Drafted++
			}
		}
		res.Added++
	}
	return res, nil
}

// PortRecord is one record of a port written in one go: the fields the
// document gives, those to clear, and the checks it does not answer with
// their reasons.
type PortRecord struct {
	Record string         `json:"record" jsonschema:"the record, as Kind/id"`
	Set    map[string]any `json:"set,omitempty" jsonschema:"every field the document gives, by JSON pointer"`
	Unset  []string       `json:"unset,omitempty" jsonschema:"fields to clear, by JSON pointer"`
	Open   []OpenReason   `json:"open,omitempty" jsonschema:"each check the document does not answer, with the reason your person will read"`
}

// OpenReason is a check left open, with the reason a person reads.
type OpenReason struct {
	Check  string `json:"check"`
	Reason string `json:"reason"`
}

// PortRecordResult is what writing one record of a port kept and left.
type PortRecordResult struct {
	Record  string    `json:"record"`
	Refused []Problem `json:"refused,omitempty"`
	Left    []string  `json:"left,omitempty"`
	NotLeft []string  `json:"notLeft,omitempty"`
	Error   string    `json:"error,omitempty"`
	// Err is the error behind Error, for an adapter to word.
	Err error `json:"-"`
}

// PortRecords writes every record of a port, each as one write: the
// fields the document gives set, the checks it does not answer left open
// with their reasons. It answers, record by record, what was refused and
// left, and what every record of the port still needs.
func (e *Engine) PortRecords(ctx context.Context, set string, records []PortRecord) ([]PortRecordResult, []RecordFill, error) {
	var results []PortRecordResult
	var work []string
	// Every record the call creates exists before any is written, so one
	// may name another by its id whatever their order in the call.
	for _, r := range records {
		k, id, ok := strings.Cut(r.Record, "/")
		if !ok || id == "" {
			continue
		}
		if _, found, err := e.ChangeSetText(ctx, set, k, id); err != nil || found {
			continue
		}
		start := map[string]any{"/metadata/name": id}
		if name, ok := r.Set["/metadata/name"].(string); ok && strings.TrimSpace(name) != "" {
			start["/metadata/name"] = name
		}
		_, _ = e.EditInChangeSet(ctx, set, k, id, start, nil)
	}
	for _, r := range records {
		// What the records before wrote is in play for this one.
		if inSet, err := e.InChangeSet(ctx, set); err == nil {
			ctx = inSet
		}
		res := PortRecordResult{Record: r.Record}
		k, id, ok := strings.Cut(r.Record, "/")
		if !ok {
			res.Error = "name the record as Kind/id, as the second call listed it"
			results = append(results, res)
			continue
		}
		work = append(work, r.Record)
		refused, created, err := e.WriteFields(ctx, set, k, id, r.Set, r.Unset, true)
		if err != nil {
			res.Error, res.Err = err.Error(), err
			results = append(results, res)
			continue
		}
		for _, cr := range created {
			if why, ok := strings.CutPrefix(cr, "refused: "); ok {
				refused = append(refused, Problem{Message: why})
			}
		}
		res.Refused = refused
		for _, o := range r.Open {
			if strings.TrimSpace(o.Reason) == "" {
				res.NotLeft = append(res.NotLeft, o.Check+": give the reason your person will read")
				continue
			}
			if err := MayLeave(o.Check, "not available"); err != nil {
				res.NotLeft = append(res.NotLeft, err.Error())
				continue
			}
			if err := e.LeaveOpen(ctx, set, k, id, o.Check, o.Reason, false); err != nil {
				res.NotLeft = append(res.NotLeft, o.Check+": "+err.Error())
				continue
			}
			res.Left = append(res.Left, o.Check)
		}
		results = append(results, res)
	}
	e.sourceTeams(ctx, set)
	ctx, err := e.InChangeSet(ctx, set)
	if err != nil {
		return nil, nil, err
	}
	all, err := e.portWork(ctx, set, work)
	if err != nil {
		return nil, nil, err
	}
	still, err := e.PortFills(ctx, set, all)
	return results, still, err
}

// PortFills is what each record of a port still needs, field by field:
// the checks open on it, each with the field that meets it, what to
// write and the sections that say it, the fields its schema requires, and
// the shape of each list or object once. A port writes every record from
// it in one call.
func (e *Engine) PortFills(ctx context.Context, set string, work []string) ([]RecordFill, error) {
	var out []RecordFill
	for _, w := range work {
		k, id, ok := strings.Cut(w, "/")
		if !ok {
			continue
		}
		wk, err := e.Work(ctx, []Ref{{Kind: k, ID: id}}, "")
		if err != nil {
			return nil, err
		}
		var fill []FillItem
		seen := map[string]bool{}
		for _, t := range wk.Tasks {
			if t.Kind != k || t.ID != id {
				continue
			}
			f := FillItem{Check: t.Check, Do: t.Do}
			if t.Field != "" {
				f.Field = t.Field
				f.Read = e.sectionsIn(ctx, t.Field)
				if sh, ok := e.FieldShapeAt(k, t.Field); ok && !seen[t.Field] {
					seen[t.Field] = true
					switch sh.Example.(type) {
					case []any, map[string]any:
						f.Shape = &sh
					}
				}
			}
			fill = append(fill, f)
		}
		// What the schema requires is filled the same way: a field a
		// draft lacks is in the fill beside the checks.
		problems, err := e.DraftProblems(ctx, k, id)
		if err != nil {
			return nil, err
		}
		for _, p := range problems {
			if p.Keyword != "required" {
				fill = append(fill, FillItem{Check: "schema", Field: p.Path, Do: p.Message})
				continue
			}
			for _, m := range quotedName.FindAllStringSubmatch(p.Message, -1) {
				field := strings.TrimSuffix(p.Path, "/") + "/" + m[1]
				if seen[field] {
					continue
				}
				seen[field] = true
				f := FillItem{Check: "required", Field: field, Do: "Required: write " + m[1] + " from the document.", Read: e.sectionsIn(ctx, field)}
				if sh, ok := e.FieldShapeAt(k, field); ok {
					f.Shape = &sh
					// A reference is written as the name the document
					// gives; Cartograph finds or drafts what it names.
					if _, text := sh.Example.(string); text && slices.Contains(sh.Optional, "kind") && slices.Contains(sh.Optional, "id") {
						f.Do = "Required: write " + m[1] + " as the name the document gives the role or body (Steering Committee); Cartograph finds it or drafts it."
						f.Shape = nil
					}
					if len(sh.OneOf) > 0 {
						f.Do = "Required: write " + m[1] + ", one of " + strings.Join(sh.OneOf, ", ") + ", as the document says it."
					}
				}
				fill = append(fill, f)
			}
		}
		if len(fill) == 0 {
			continue
		}
		rec := RecordFill{Record: w, Fill: fill}
		if text, found, _ := e.ChangeSetText(ctx, set, k, id); found {
			var doc struct {
				Metadata struct{ Name string } `json:"metadata"`
			}
			if e.codec.DecodeInto(text, &doc) == nil {
				rec.Name = doc.Metadata.Name
			}
		}
		out = append(out, rec)
	}
	return out, nil
}

// quotedName is a name a schema message quotes ('team').
var quotedName = regexp.MustCompile(`'([^']+)'`)

// sectionsIn are the sections of the change set's documents that most
// likely answer a field, when a document was brought in.
func (e *Engine) sectionsIn(ctx context.Context, field string) []string {
	view, ok := ChangeSetOf(ctx)
	if !ok {
		return nil
	}
	return e.SectionsFor(ctx, view, field)
}

// portWork is a port's work and every other record its change set
// drafted that is not valid yet (a data source a register named), so
// the records step finishes them all.
func (e *Engine) portWork(ctx context.Context, set string, work []string) ([]string, error) {
	view, err := e.ViewChangeSet(ctx, set)
	if err != nil {
		return nil, err
	}
	have := map[string]bool{}
	for _, w := range work {
		have[w] = true
	}
	out := append([]string(nil), work...)
	for _, it := range view.Items {
		ref := it.Item.Kind + "/" + it.Item.ID
		if have[ref] || strings.HasPrefix(it.Item.Kind, "_") {
			continue
		}
		problems, err := e.DraftProblems(ctx, it.Item.Kind, it.Item.ID)
		if err != nil {
			return nil, err
		}
		if len(problems) > 0 {
			have[ref] = true
			out = append(out, ref)
		}
	}
	return out, nil
}

// sourceTeams gives a data source the port drafted with no team (its
// register row named no body that keeps it) the team of the project
// whose indicator reads it: the work's own data, kept by the team doing
// the work until the person says otherwise.
func (e *Engine) sourceTeams(ctx context.Context, set string) {
	view, err := e.ViewChangeSet(ctx, set)
	if err != nil {
		return
	}
	docs := map[string]map[string]any{}
	for _, it := range view.Items {
		text, found, err := e.ChangeSetText(ctx, set, it.Item.Kind, it.Item.ID)
		var doc map[string]any
		if err == nil && found && e.codec.DecodeInto(text, &doc) == nil {
			docs[it.Item.Kind+"/"+it.Item.ID] = doc
		}
	}
	spec := func(d map[string]any) map[string]any { m, _ := d["spec"].(map[string]any); return m }
	// The team of each KPI, from the project that lists it.
	kpiTeam := map[string]any{}
	for ref, d := range docs {
		if !strings.HasPrefix(ref, "Project/") || spec(d)["team"] == nil {
			continue
		}
		kpis, _ := spec(d)["kpis"].([]any)
		for _, k := range kpis {
			if m, ok := k.(map[string]any); ok {
				if id, _ := m["kpi"].(string); id != "" {
					kpiTeam[id] = spec(d)["team"]
				}
			}
		}
	}
	for ref, d := range docs {
		kind, id, _ := strings.Cut(ref, "/")
		if kind != "DataSource" || spec(d)["team"] != nil {
			continue
		}
		for kref, kd := range docs {
			team := kpiTeam[strings.TrimPrefix(kref, "KPI/")]
			if !strings.HasPrefix(kref, "KPI/") || team == nil {
				continue
			}
			if slices.Contains(stringsOf(spec(kd)["sources"]), id) {
				_, _ = e.EditInChangeSet(ctx, set, kind, id, map[string]any{"/spec/team": team}, nil)
				break
			}
		}
	}
}

// componentRows holds each component project to the document's own
// deliverable register, where it has one: a piece with a change of its
// own names the row that hands it over (D4), one row to one piece, so a
// phase, a service or a theme cannot be made a project by renaming it.
func (e *Engine) componentRows(ctx context.Context, set string, src Source, pieces []structure.Piece) []string {
	var rows []map[string]any
	for _, sec := range src.Sections {
		for _, f := range sec.Feeds {
			if f == "/spec/deliverables" {
				if items, err := e.RegisterOf(ctx, set, sec.ID, f); err == nil {
					rows = append(rows, items...)
				}
			}
		}
	}
	if len(rows) == 0 {
		return nil
	}
	codes := map[string]string{}
	var listed []string
	for _, r := range rows {
		id, _ := r["id"].(string)
		name, _ := r["name"].(string)
		if id == "" {
			continue
		}
		code := strings.ToUpper(id)
		codes[code] = name
		listed = append(listed, code+" "+name)
	}
	var problems []string
	used := map[string]string{}
	for _, p := range pieces {
		if !p.ChangeOfItsOwn {
			continue
		}
		code := strings.ToUpper(strings.TrimSpace(p.Deliverable))
		switch {
		case code == "":
			problems = append(problems, fmt.Sprintf("%q has a change of its own: give deliverable, the code of the row in the document's deliverable register that hands it over. "+
				"If no row hands it over, it is not a project: make it an output of the work, a service (ongoing) or leave it out. Rows: %s", p.Name, strings.Join(listed, "; ")))
		case codes[code] == "":
			problems = append(problems, fmt.Sprintf("%q names deliverable %s, which the register does not have. Rows: %s", p.Name, code, strings.Join(listed, "; ")))
		case used[code] != "":
			problems = append(problems, fmt.Sprintf("%q and %q both name deliverable %s: one row is one project", used[code], p.Name, code))
		default:
			used[code] = p.Name
		}
	}
	return problems
}
