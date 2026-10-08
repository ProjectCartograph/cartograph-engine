package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
)

// The port chain: a document's text to its structure, its registers and every record written.

// structureDescription is the structure tool's description: the questions
// themselves, in order, so an agent answers them as a person would.
// structureExample is a whole structure call, the shape a small agent copies.
const structureExample = `{"pieces":[{"name":"Rollout","none":true},{"name":"Handbook","outputOf":"Rollout"},` +
	`{"name":"Baseline survey","changeOfItsOwn":true,"change":"sets the baseline the targets are set from","dependedOnBy":["Rollout"]},{"name":"Compliance checks","ongoing":true},` +
	`{"name":"Standards policy","policy":true},{"name":"Farm supply scheme","outOfScope":true}]}`

func structureDescription() string {
	var b strings.Builder
	b.WriteString("Call this first, before any draft, whenever you port a document or start something new (TAXONOMY.md D56). " +
		"What a document calls a project, a workstream or a programme is never trusted: list every piece of work it names " +
		"(the project itself, each workstream, sub-project, phase, survey, system, service, policy and partner strand), " +
		"then answer these questions for each piece, in order; the first yes decides:\n")
	for i, q := range engine.StructureQuestions {
		if q.Field == "" {
			fmt.Fprintf(&b, "%d. %s %s\n", i+1, q.Question, q.Then)
			continue
		}
		fmt.Fprintf(&b, "%d. %s (%s) %s\n", i+1, q.Question, q.Field, q.Then)
	}
	b.WriteString("Example: " + structureExample + ". Leave out every answer that is no. ")
	b.WriteString("A piece is a piece of work: the whole project, each workstream, phase or sub-project, each survey, system, service, policy or " +
		"scheme the documents name, one piece each. Milestones, risks, KPIs, objectives, costs and roles are not pieces: they go inside the records. ")
	b.WriteString("Give outputOf as the name of the piece it is an output of, and dependedOnBy as the names of the pieces that depend on it. " +
		"The answer says what each piece is, what it belongs to, and the order to write the records in; fix every problem it lists and " +
		"call it again until there are none, then open one change set and write the records in that order.")
	return b.String()
}

// portWrites are the tools that write a record one field or one check at
// a time: in a port, port writes every record at once instead.
var portWrites = map[string]bool{"edit_draft": true, "save_draft": true, "save_drafts": true, "settle": true, "leave_open": true}

// portOnly refuses a one-at-a-time write in a change set that holds a
// document: a port has one path, port with records, so an agent cannot
// wander off it into writes that leave the rest of the port unwritten.
func portOnly(c call, name string, in any) error {
	if !portWrites[name] {
		return nil
	}
	var args struct {
		ChangeSet string `json:"changeSet"`
	}
	if b, err := json.Marshal(in); err == nil {
		_ = json.Unmarshal(b, &args)
	}
	cs, cc, found, err := c.inChangeSet(args.ChangeSet, false)
	if err != nil || !found {
		return nil
	}
	if srcs, err := c.o.Engine.Sources(cc.ctx, cs.ID); err != nil || len(srcs) == 0 {
		return nil
	}
	return fmt.Errorf("%w: change set %s is a port of a document: write its records with port and records, "+
		`[{"record": "Kind/id", "set": {"/spec/...": ...}, "unset": ["/spec/..."], "open": [{"check": "...", "reason": "..."}]}], every record in one call; `+
		"port answers what is still open on each. Call port with records now", engine.ErrBadEdit, cs.ID)
}

// piecesOf reads structure answers as an agent sent them, strictly: a
// piece sent as text, a key the questions do not ask or a kind that is
// not work is refused with what to send instead, never ignored.
func piecesOf(raws []any) ([]engine.StructurePiece, map[string]any) {
	// A small agent sends names where pieces go: say how, by example,
	// rather than refusing in the words of a schema.
	pieces := make([]engine.StructurePiece, 0, len(raws))
	var bad, problems []string
	for _, raw := range raws {
		b, _ := json.Marshal(raw)
		var p engine.StructurePiece
		m, isObject := raw.(map[string]any)
		if !isObject || json.Unmarshal(b, &p) != nil {
			bad = append(bad, string(b))
			continue
		}
		// What a piece is, is the answer, never the agent's to say:
		// a key the questions do not ask is refused, not ignored.
		for k := range m {
			if !engine.StructureKeys[k] {
				problems = append(problems, fmt.Sprintf("%q: %q is not an answer; give only name and the yes answers", p.Name, k))
			}
		}
		if k, _ := m["kind"].(string); k != "" && k != "Project" && k != "Programme" && k != "Portfolio" && k != "Operation" {
			problems = append(problems, fmt.Sprintf("%q is a %s, not a piece of work: leave it out, it is written inside the records", p.Name, k))
		}
		pieces = append(pieces, p)
	}
	if len(bad) > 0 {
		problems = append(problems, "each piece is an object with its name and its yes answers, not text: "+strings.Join(bad, ", "))
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return nil, map[string]any{"problems": problems,
			"next": "Call structure again. A piece is a piece of work (the project, each workstream, phase, survey, system, service, policy or scheme), " +
				"each an object with name and only the answers that are yes, like " + structureExample + "."}
	}
	return pieces, nil
}

// sectionsFor are the sections of the change set's documents that most
// likely answer a field, when a document was brought in.
func sectionsFor(c call, field string) []string {
	view, ok := engine.ChangeSetOf(c.ctx)
	if !ok {
		return nil
	}
	return c.o.Engine.SectionsFor(c.ctx, view, field)
}

// registerInto settles a section's register into a record's list, row by
// row through the field pipeline: it answers how many rows the section
// holds, how many were added, what was refused and what was drafted.
func registerInto(c call, set, kind, id, section, field string, route map[string]string) (int, int, []engine.Problem, []string, []string, error) {
	items, err := c.o.Engine.RegisterOf(c.ctx, set, section, field)
	if err != nil {
		return 0, 0, nil, nil, nil, err
	}
	var refused []engine.Problem
	var drafted, notPorted []string
	added := 0
	main, mainID := kind, id
	for _, it := range items {
		// What the rows before drafted is in play for this one, so a body
		// named on ten rows is one record, not ten.
		if ctx, err := c.o.Engine.InChangeSet(c.ctx, set); err == nil {
			c.ctx = ctx
		}
		rowID, _ := it["id"].(string)
		if why, skip := it["_skip"].(string); skip {
			notPorted = append(notPorted, strings.ToUpper(rowID)+": "+why)
			continue
		}
		// A component's own row goes into the component.
		kind, id = main, mainID
		if to, ok := route[rowID]; ok {
			kind, id, _ = strings.Cut(to, "/")
		}
		extra, _ := it["_kpi"].(map[string]any)
		delete(it, "_kpi")
		team, _ := extra["_team"].(string)
		delete(extra, "_team")
		r, created, err := applyFields(c, set, kind, id, map[string]any{field + "/-": it}, nil)
		// A KPI drafted from the row gets what the row says of it: its
		// source and its cycle, found or drafted by name.
		if err == nil && len(extra) > 0 {
			for _, d := range created {
				if k, kid, ok := strings.Cut(d, "/"); ok && k == "KPI" {
					more, moreCreated, _ := applyFields(c, set, "KPI", kid, extra, nil)
					r = append(r, more...)
					// A source drafted for it is kept by the body that owns
					// the indicator.
					for _, m := range moreCreated {
						if mk, mid, ok := strings.Cut(m, "/"); ok && mk == "DataSource" && team != "" {
							teamed, teamCreated, _ := applyFields(c, set, "DataSource", mid, map[string]any{"/spec/team": team}, nil)
							r = append(r, teamed...)
							created = append(created, teamCreated...)
						}
					}
					created = append(created, moreCreated...)
					break
				}
			}
		}
		var invalid *engine.ValidationError
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
				refused = append(refused, invalid.Problems...)
				r, created, err = applyFields(c, set, kind, id, map[string]any{field + "/-": trimmed}, nil)
			}
		}
		if err != nil {
			if errors.As(err, &invalid) {
				refused = append(refused, invalid.Problems...)
				continue
			}
			return 0, 0, nil, nil, nil, err
		}
		refused = append(refused, r...)
		for _, d := range created {
			if !strings.HasPrefix(d, "cut: ") && !strings.HasPrefix(d, "refused: ") {
				drafted = append(drafted, d)
			}
		}
		added++
	}
	return len(items), added, refused, drafted, notPorted, nil
}

// portPieces runs the rest of a port once its pieces are answered: it
// decides the structure, drafts every record, writes every register the
// document has into the main project, and hands over the first record to
// settle. port and start_work both end here, so a port comes out the same
// whichever an agent calls.
func portPieces(c call, set string, src engine.Source, raws []any) (any, error) {
	e := c.o.Engine
	var err error
	pieces, refused := piecesOf(raws)
	if refused != nil {
		return refused, nil
	}
	st := engine.Classify(pieces)
	// The document's own workstream plan says which names are workstreams:
	// none of them is a piece (D49), however it is called.
	for _, p := range pieces {
		// A service, an output or work another body leads may carry a
		// workstream's name; only a project may not.
		if p.Ongoing || p.OutOfScope || p.Policy || strings.TrimSpace(p.OutputOf) != "" {
			continue
		}
		for _, ws := range engine.WorkstreamNames(src) {
			if engine.NearName(p.Name, ws) {
				st.Problems = append(st.Problems, fmt.Sprintf("%q is the document's workstream %q, and Cartograph keeps no workstreams (TAXONOMY.md D49): "+
					"send what it hands over as pieces with outputOf the work, and each piece inside it with a change of its own (a survey, a system) as a piece by itself", p.Name, ws))
				break
			}
		}
	}
	st.Problems = append(st.Problems, componentRows(c, set, src, pieces)...)
	if len(st.Problems) > 0 {
		return map[string]any{"problems": st.Problems, "next": "Fix every problem and call port again with the pieces; nothing was drafted."}, nil
	}
	for _, d := range st.Drafts() {
		kind, _ := d["kind"].(string)
		id, _ := d["metadata"].(map[string]any)["id"].(string)
		if _, drafted, _ := e.ChangeSetText(c.ctx, set, kind, id); drafted {
			continue
		}
		text, err := e.Codec().Encode(d)
		if err != nil {
			return nil, err
		}
		if err := e.SaveInChangeSet(c.ctx, set, kind, id, text); err != nil {
			return nil, err
		}
		c.announce(step{Step: "draft", Kind: kind, ID: id, Text: text, ChangeSet: set})
	}
	// The main project takes the document's registers.
	main := ""
	for _, p := range st.Pieces {
		if p.Kind == "Project" && len(p.Of) == 0 {
			main = p.Record
			break
		}
	}
	// Each component's deliverable row, by the code its piece gave.
	route := map[string]string{}
	recordOf := map[string]string{}
	for _, p := range st.Pieces {
		recordOf[p.Name] = p.Record
	}
	for _, p := range pieces {
		if code := strings.ToLower(strings.TrimSpace(p.Deliverable)); code != "" && recordOf[p.Name] != "" {
			route[code] = recordOf[p.Name]
		}
	}
	var registers []map[string]any
	if mk, mid, ok := strings.Cut(main, "/"); ok {
		for _, sec := range src.Sections {
			for _, f := range sec.Feeds {
				if f != "/spec/milestones" && f != "/spec/deliverables" && f != "/spec/risks" && f != "/spec/kpis" {
					continue
				}
				rows, added, refusedRows, drafted, notPorted, err := registerInto(c, set, mk, mid, sec.ID, f, route)
				if err != nil {
					registers = append(registers, map[string]any{"section": sec.ID, "field": f, "error": err.Error()})
					continue
				}
				if rows == 0 {
					continue
				}
				reg := map[string]any{"section": sec.ID, "heading": sec.Heading, "field": f, "rows": rows, "added": added}
				if len(refusedRows) > 0 {
					reg["refused"] = refusedRows
				}
				if len(drafted) > 0 {
					reg["drafted"] = len(drafted)
				}
				if len(notPorted) > 0 {
					reg["notPorted"] = notPorted
				}
				registers = append(registers, reg)
			}
		}
	}
	if c.ctx, err = e.InChangeSet(c.ctx, set); err != nil {
		return nil, err
	}
	all, err := portWork(c, set, st.Work)
	if err != nil {
		return nil, err
	}
	records, err := fillsOf(c, set, all)
	if err != nil {
		return nil, err
	}
	return map[string]any{"changeSet": set, "work": st.Work, "pieces": st.Pieces, "registers": registers, "records": records,
		"next": "The structure is drafted and the registers written. Now call port a third time with records: every record listed here, " +
			"each with set (every field in its fill, written from the sections read names) and open (each check the document does not answer, with its reason). " +
			"One call for all of them; then propose; report from work_summary."}, nil
}

// onlySources reports whether a change set holds documents and nothing
// drafted yet.
func onlySources(view engine.ChangeSetView) bool {
	for _, it := range view.Items {
		if !strings.HasPrefix(it.Item.Kind, "_") {
			return false
		}
	}
	return true
}

// wholeFile refuses a document's text that is not its whole file: a
// summary or an excerpt loses the registers and sections a port is
// written from, and no later step can tell. The file's size is given
// beside the text; line endings and a final newline may differ.
func wholeFile(text string, fileSize int) error {
	if fileSize <= 0 {
		return fmt.Errorf("%w: give fileSize, the file's size as one number of bytes (os.path.getsize(path) in a script), beside its text", engine.ErrBadEdit)
	}
	got := len(text)
	slack := fileSize/50 + 64
	if got < fileSize-slack || got > fileSize+slack {
		return fmt.Errorf("%w: the text is %d bytes and the file %d: send the file's whole text, read straight from it by a script "+
			"(json.dump({\"title\": ..., \"text\": open(path).read(), \"fileSize\": os.path.getsize(path)})), never a summary or a part", engine.ErrBadEdit, got, fileSize)
	}
	return nil
}

// fillsOf is what each record of the work still needs, field by field:
// the checks open on it, each with the field that meets it, what to
// write and the sections that say it, and the shape of each list or
// object once. A port writes every record from it in one call.
func fillsOf(c call, set string, work []string) ([]map[string]any, error) {
	e := c.o.Engine
	var out []map[string]any
	for _, w := range work {
		k, id, ok := strings.Cut(w, "/")
		if !ok {
			continue
		}
		wk, err := e.Work(c.ctx, []engine.Ref{{Kind: k, ID: id}}, "")
		if err != nil {
			return nil, err
		}
		var fill []map[string]any
		seen := map[string]bool{}
		for _, t := range wk.Tasks {
			if t.Kind != k || t.ID != id {
				continue
			}
			f := map[string]any{"check": t.Check, "do": t.Do}
			if t.Field != "" {
				f["field"] = t.Field
				if read := sectionsFor(c, t.Field); len(read) > 0 {
					f["read"] = read
				}
				if sh, ok := e.FieldShapeAt(k, t.Field); ok && !seen[t.Field] {
					seen[t.Field] = true
					switch sh.Example.(type) {
					case []any, map[string]any:
						f["shape"] = sh
					}
				}
			}
			fill = append(fill, f)
		}
		// What the schema requires is filled the same way: a field a
		// draft lacks is in the fill beside the checks.
		problems, err := e.DraftProblems(c.ctx, k, id)
		if err != nil {
			return nil, err
		}
		for _, p := range problems {
			if p.Keyword != "required" {
				fill = append(fill, map[string]any{"check": "schema", "field": p.Path, "do": p.Message})
				continue
			}
			for _, m := range quotedName.FindAllStringSubmatch(p.Message, -1) {
				field := strings.TrimSuffix(p.Path, "/") + "/" + m[1]
				if seen[field] {
					continue
				}
				seen[field] = true
				f := map[string]any{"check": "required", "field": field, "do": "Required: write " + m[1] + " from the document."}
				if read := sectionsFor(c, field); len(read) > 0 {
					f["read"] = read
				}
				if sh, ok := e.FieldShapeAt(k, field); ok {
					f["shape"] = sh
					// A reference is written as the name the document
					// gives; Cartograph finds or drafts what it names.
					if _, text := sh.Example.(string); text && slices.Contains(sh.Optional, "kind") && slices.Contains(sh.Optional, "id") {
						f["do"] = "Required: write " + m[1] + " as the name the document gives the role or body (Steering Committee); Cartograph finds it or drafts it."
						delete(f, "shape")
					}
					if len(sh.OneOf) > 0 {
						f["do"] = "Required: write " + m[1] + ", one of " + strings.Join(sh.OneOf, ", ") + ", as the document says it."
					}
				}
				fill = append(fill, f)
			}
		}
		if len(fill) > 0 {
			rec := map[string]any{"record": w, "fill": fill}
			if text, found, _ := e.ChangeSetText(c.ctx, set, k, id); found {
				var doc struct {
					Metadata struct{ Name string } `json:"metadata"`
				}
				if e.Codec().DecodeInto(text, &doc) == nil && doc.Metadata.Name != "" {
					rec["name"] = doc.Metadata.Name
				}
			}
			out = append(out, rec)
		}
	}
	return out, nil
}

// quotedName is a name a schema message quotes ('team').
var quotedName = regexp.MustCompile(`'([^']+)'`)

// portWork is a port's work and every other record its change set
// drafted that is not valid yet (a data source a register named), so
// the records step finishes them all.
func portWork(c call, set string, work []string) ([]string, error) {
	view, err := c.o.Engine.ViewChangeSet(c.ctx, set)
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
		problems, err := c.o.Engine.DraftProblems(c.ctx, it.Item.Kind, it.Item.ID)
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

// portRecords writes every record of a port in one call, as one settle
// each: the fields the document gives set, the checks it does not answer
// left open with their reasons. It answers, record by record, what was
// refused and what is still open, so one more call finishes the port.
func portRecords(c call, set string, records []portRecord) (any, error) {
	e := c.o.Engine
	var results []map[string]any
	var work []string
	for _, r := range records {
		k, id, ok := strings.Cut(r.Record, "/")
		if !ok {
			results = append(results, map[string]any{"record": r.Record, "error": "name the record as Kind/id, as the second call listed it"})
			continue
		}
		work = append(work, r.Record)
		res := map[string]any{"record": r.Record}
		refused, created, err := applyFields(c, set, k, id, r.Set, r.Unset)
		if err != nil {
			res["error"] = err.Error()
			results = append(results, res)
			continue
		}
		for _, cr := range created {
			if why, ok := strings.CutPrefix(cr, "refused: "); ok {
				refused = append(refused, engine.Problem{Message: why})
			}
		}
		if len(refused) > 0 {
			res["refused"] = refused
		}
		var left, notLeft []string
		for _, o := range r.Open {
			if strings.TrimSpace(o.Reason) == "" {
				notLeft = append(notLeft, o.Check+": give the reason your person will read")
				continue
			}
			if err := mayLeave(o.Check, "not available"); err != nil {
				notLeft = append(notLeft, err.Error())
				continue
			}
			if err := e.LeaveOpen(c.ctx, set, k, id, o.Check, o.Reason, false); err != nil {
				notLeft = append(notLeft, o.Check+": "+err.Error())
				continue
			}
			left = append(left, o.Check)
		}
		if len(left) > 0 {
			res["left"] = left
		}
		if len(notLeft) > 0 {
			res["notLeft"] = notLeft
		}
		results = append(results, res)
	}
	sourceTeams(c, set)
	var err error
	if c.ctx, err = e.InChangeSet(c.ctx, set); err != nil {
		return nil, err
	}
	all, err := portWork(c, set, work)
	if err != nil {
		return nil, err
	}
	still, err := fillsOf(c, set, all)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"changeSet": set, "records": results}
	if len(still) > 0 {
		out["stillOpen"] = still
		out["next"] = "Call port again with records for what is still open: set it from the document, or open it with the reason the document does not say it. " +
			"Refused fields are sent again in the shape each says. Then propose."
	} else {
		out["next"] = "Every record is settled. Propose the change set with propose; report from work_summary."
	}
	return out, nil
}

// sourceTeams gives a data source the port drafted with no team (its
// register row named no body that keeps it) the team of the project
// whose indicator reads it: the work's own data, kept by the team doing
// the work until the person says otherwise.
func sourceTeams(c call, set string) {
	e := c.o.Engine
	view, err := e.ViewChangeSet(c.ctx, set)
	if err != nil {
		return
	}
	docs := map[string]map[string]any{}
	for _, it := range view.Items {
		text, found, err := e.ChangeSetText(c.ctx, set, it.Item.Kind, it.Item.ID)
		var doc map[string]any
		if err == nil && found && e.Codec().DecodeInto(text, &doc) == nil {
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
			if !strings.HasPrefix(kref, "KPI/") || kpiTeam[strings.TrimPrefix(kref, "KPI/")] == nil {
				continue
			}
			if slices.Contains(stringsOfAny(spec(kd)["sources"]), id) {
				_, _ = e.EditInChangeSet(c.ctx, set, kind, id, map[string]any{"/spec/team": kpiTeam[strings.TrimPrefix(kref, "KPI/")]}, nil)
				break
			}
		}
	}
}

func stringsOfAny(v any) []string {
	list, _ := v.([]any)
	var out []string
	for _, x := range list {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// componentRows holds each component project to the document's own
// deliverable register, where it has one: a piece with a change of its
// own names the row that hands it over (D4), one row to one piece, so a
// phase, a service or a theme cannot be made a project by renaming it.
func componentRows(c call, set string, src engine.Source, pieces []engine.StructurePiece) []string {
	var rows []map[string]any
	for _, sec := range src.Sections {
		for _, f := range sec.Feeds {
			if f == "/spec/deliverables" {
				if items, err := c.o.Engine.RegisterOf(c.ctx, set, sec.ID, f); err == nil {
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
