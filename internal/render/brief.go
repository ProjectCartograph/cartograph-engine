package render

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
)

// The project charter is a brief (TAXONOMY.md D55): what a sponsor approves,
// in a few pages. Each register prints the rows a decision turns on and
// counts the rest, which stay in Cartograph; nothing in it is prose typed
// for the document.

// uniqueRoles are the roles a project names once, so the charter can name
// the party by its role ("Project manager") rather than its full post.
var uniqueRoles = map[string]bool{"sponsor": true, "manager": true, "technicalLead": true, "dataSteward": true, "serviceOwner": true}

// who names a party briefly: a project role held once by its role, any
// other by what it is called.
func (p plan) who(v any) string {
	r := obj(v)
	if str(r["local"]) == "resources" {
		for _, res := range list(p.spec["resources"]) {
			if str(res["id"]) == str(r["id"]) && uniqueRoles[str(res["role"])] {
				return label("role", str(res["role"]))
			}
		}
	}
	return p.item(v)
}

// readinessBrief says in one line that every part is ready, and lists
// only the parts that are not.
func (d *doc) readinessBrief(items []engine.ProjectCheckItem) {
	ok, open := 0, 0
	for _, it := range items {
		if it.State == "ok" {
			ok++
		} else {
			open++
		}
	}
	if len(items) == 0 {
		return
	}
	if open == 0 {
		d.flush()
		d.b.WriteString("<p class=\"ready-line\">")
		d.b.WriteString(chipHTML("readiness", "ready"))
		d.b.WriteString(" ")
		d.b.WriteString(esc(fmt.Sprintf("Every part of the definition is ready: %d checks met.", ok)))
		d.b.WriteString("</p>\n")
		return
	}
	var rows [][]string
	for _, a := range readinessAreas {
		state, met := "ready", 0
		var notes []string
		for _, it := range items {
			if !hasString(a.steps, it.Section) {
				continue
			}
			switch it.State {
			case "ok":
				met++
			case "block":
				state = "blocking"
				notes = append(notes, it.Message)
			default:
				if state != "blocking" {
					state = "open"
				}
				notes = append(notes, it.Message)
			}
		}
		if state == "ready" {
			continue
		}
		rows = append(rows, []string{a.label, chip("readiness", state), strings.Join(notes, " ")})
	}
	d.h3("Definition readiness")
	d.p(fmt.Sprintf("%d of %d checks met. Parts with points still open:", ok, ok+open))
	d.table([]string{"Part of the definition", "Status", "Still open"}, rows)
}

// keyMilestones prints every milestone, each with why it matters where
// it does: on the chain that sets the end date, still to be set by an
// event, late, or held back by an approval condition (#74).
func (d *doc) keyMilestones(p plan, items []engine.ScheduleItem) {
	gated := map[string]bool{}
	for _, c := range list(p.spec["conditions"]) {
		on := obj(obj(c["gates"])["on"])
		if str(on["local"]) == "milestones" {
			gated[str(on["id"])] = true
		}
	}
	placed := map[string]engine.ScheduleItem{}
	for _, it := range items {
		placed[it.ID] = it
	}
	ms := list(p.spec["milestones"])
	var rows [][]string
	for i, m := range ms {
		id := str(m["id"])
		it := placed[id]
		var why []string
		if it.Critical {
			why = append(why, "Sets the end date")
		}
		if it.Pending {
			why = append(why, "Set by an event")
		}
		if gated[id] {
			why = append(why, "Held by a condition")
		}
		if it.Late {
			why = append(why, "Late")
		}
		at := fmt.Sprintf("/spec/milestones/%d", i)
		rows = append(rows, []string{itemNumber("M", id, i), d.at(str(m["name"]), at+"/name"), d.from(p.timing(m["timing"]), at+"/timing"), d.at(p.who(m["owner"]), at+"/owner"),
			strings.Join(why, "; "), d.from(p.status("milestones", id), "/spec/events")})
	}
	if len(rows) == 0 {
		return
	}
	d.h3("Milestones")
	d.table([]string{"No.", "Milestone", "When", "Owner", "Why it matters", "Status"}, rows)
	d.addRow("/spec/milestones", "Milestone")
}

// deliverablesBrief prints each deliverable by name, with its owner, when
// it is due and who accepts it.
func (d *doc) deliverablesBrief(p plan) {
	var rows [][]string
	for i, dv := range list(p.spec["deliverables"]) {
		var by []string
		for _, a := range list(dv["acceptance"]) {
			if w := p.who(a["by"]); w != "" && !hasString(by, w) {
				by = append(by, w)
			}
		}
		at := fmt.Sprintf("/spec/deliverables/%d", i)
		rows = append(rows, []string{itemNumber("D", str(dv["id"]), i), d.at(str(dv["name"]), at+"/name"), d.at(p.who(dv["owner"]), at+"/owner"), d.from(p.timing(dv["due"]), at+"/due"),
			d.from(strings.Join(by, ", "), at+"/acceptance"), d.from(p.status("deliverables", str(dv["id"])), "/spec/events")})
	}
	d.table([]string{"No.", "Deliverable", "Owner", "Due", "Accepted by", "Status"}, rows)
	d.addRow("/spec/deliverables", "Deliverable")
}

// costsBrief prints the cost lines; a funding source every line shares is
// said once.
func (d *doc) costsBrief(p plan) {
	cs := list(p.spec["costs"])
	if len(cs) == 0 {
		return
	}
	sources := map[string]bool{}
	for _, c := range cs {
		sources[str(c["source"])] = true
	}
	shared := len(sources) == 1
	var rows [][]string
	for i, c := range cs {
		at := fmt.Sprintf("/spec/costs/%d", i)
		amount := number(c["amount"])
		if amount != "" {
			amount = strings.TrimSpace(str(c["currency"]) + " " + thousands(amount))
		}
		row := []string{d.at(str(c["category"]), at+"/category"), d.at(amount, at+"/amount"), d.at(label("costStatus", str(c["status"])), at+"/status")}
		if !shared {
			row = append(row, d.at(p.n.of("FundingSource", str(c["source"])), at+"/source"))
		}
		rows = append(rows, row)
	}
	headers := []string{"Cost line", "Amount", "Status"}
	if !shared {
		headers = append(headers, "Funding source")
	}
	d.h3("Cost plan")
	d.table(headers, rows)
	d.addRow("/spec/costs", "Cost line")
	for s := range sources {
		if shared && s != "" {
			d.note("Every line is funded from " + p.n.of("FundingSource", s) + ".")
		}
	}
}

// risksBrief prints every risk, issue, dependency and constraint, the
// highest impact first: the charter is the document read, so nothing in
// the register is left out of it (#74).
func (d *doc) risksBrief(p plan, items []map[string]any) {
	moves := movesOf(p.spec)
	// Highest impact first, then likelihood.
	order := map[string]int{"high": 0, "medium": 1, "low": 2}
	// Each keeps its place in the register, for its fields' pointers.
	index := make([]int, len(items))
	for i := range index {
		index[i] = i
	}
	sort.SliceStable(index, func(a, b int) bool {
		ri, rj := order[str(items[index[a]]["impact"])], order[str(items[index[b]]["impact"])]
		if ri != rj {
			return ri < rj
		}
		return order[str(items[index[a]]["likelihood"])] < order[str(items[index[b]]["likelihood"])]
	})
	var rows [][]string
	for _, i := range index {
		r := items[i]
		at := fmt.Sprintf("/spec/risks/%d", i)
		owner := p.who(r["owner"])
		if owner == "" {
			owner = label("role", "manager")
		}
		rows = append(rows, []string{d.at(chip("riskType", str(r["type"])), at+"/type"), d.at(str(r["description"]), at+"/description"), d.at(chip("level", str(r["impact"])), at+"/impact"),
			d.from(moves[str(r["id"])], at), d.at(owner, at+"/owner"), d.from(responseOf(r), at)})
	}
	if len(rows) == 0 && len(items) == 0 && !d.editable {
		return
	}
	d.h2("Key risks")
	if len(rows) > 0 {
		d.table([]string{"Type", "Risk", "Impact", "Moves", "Owner", "Response"}, rows)
	}
	d.addRow("/spec/risks", "Risk")
}

// note is a quiet line under a table.
func (d *doc) note(s string) {
	d.flush()
	d.b.WriteString("<p class=\"note\">")
	d.b.WriteString(esc(s))
	d.b.WriteString("</p>\n")
}

// heldInCartograph counts what the brief leaves to Cartograph.
func (d *doc) heldInCartograph(spec map[string]any, stakeholders int) {
	type count struct {
		n         int
		one, many string
	}
	tasks := 0
	for _, dv := range list(spec["deliverables"]) {
		tasks += len(list(dv["tasks"]))
	}
	counts := []count{
		{len(list(spec["responsibilities"])), "responsibility (RACI)", "responsibilities (RACI)"},
		{tasks, "task", "tasks"},
		{len(list(spec["procurement"])), "procurement item", "procurement items"},
		{len(list(obj(spec["data"])["produces"])) + len(list(obj(spec["data"])["consumes"])), "data set", "data sets"},
		{stakeholders, "stakeholder", "stakeholders"},
		{len(list(spec["events"])), "recorded event", "recorded events"},
	}
	var parts []string
	for _, c := range counts {
		if c.n == 1 {
			parts = append(parts, "1 "+c.one)
		} else if c.n > 1 {
			parts = append(parts, fmt.Sprintf("%d %s", c.n, c.many))
		}
	}
	if len(parts) == 0 {
		return
	}
	d.note("Also held in Cartograph, with the full registers: " + strings.Join(parts, ", ") + ".")
}

// criticalLine names the critical path across the workspace, and any loop
// to resolve, under the dependency diagram.
func (d *doc) criticalLine(n names, g engine.ComponentGraph) {
	if len(g.CriticalPath) > 1 {
		var chain []string
		for _, r := range g.CriticalPath {
			chain = append(chain, n.of(r.Kind, r.ID))
		}
		d.p(fmt.Sprintf("Critical path: %s, %d months.", strings.Join(chain, " depends on "), g.CriticalMonths))
	}
	for _, loop := range g.Loops {
		var chain []string
		for _, r := range loop {
			chain = append(chain, n.of(r.Kind, r.ID))
		}
		d.p("Loop to resolve: " + strings.Join(chain, " depends on ") + ".")
	}
}

// problemsBrief prints each problem as the sentence it is: who is
// affected, what goes wrong for them and why, then what the project
// changes it to (#79). In an editable charter each part is marked as a
// part of the sentence, edited where it stands and never typed into.
func (d *doc) problemsBrief(n names, items []map[string]any) {
	if len(items) == 0 && d.editable {
		// The first problem, still to say, where it will be read.
		items = []map[string]any{{}}
	}
	if len(items) == 0 {
		return
	}
	d.h2("Problem statement")
	for i, pm := range items {
		d.raw(d.problemSentence(n, pm, fmt.Sprintf("/spec/summary/problems/%d", i)))
	}
	d.addRow("/spec/summary/problems", "Problem")
}

// approvalBrief prints the conditions approval carries and the sign-off
// lines, parties named by role.
func (d *doc) approvalBrief(p plan) bool {
	cds, sos := list(p.spec["conditions"]), list(p.spec["signOffs"])
	if len(cds)+len(sos) == 0 {
		return false
	}
	d.h2("Approval")
	if len(cds) > 0 {
		var rows [][]string
		for i, c := range cds {
			holds := ""
			if on := obj(obj(c["gates"])["on"]); str(on["local"]) == "milestones" {
				for j, m := range list(p.spec["milestones"]) {
					if str(m["id"]) == str(on["id"]) {
						holds = itemNumber("M", str(m["id"]), j)
					}
				}
			}
			at := fmt.Sprintf("/spec/conditions/%d", i)
			rows = append(rows, []string{fmt.Sprintf("%d", i+1), d.at(str(c["action"]), at+"/action"), d.at(p.who(c["owner"]), at+"/owner"), d.from(p.timing(c["due"]), at+"/due"),
				d.from(holds, at+"/gates"), d.from(p.status("conditions", str(c["id"])), "/spec/events")})
		}
		d.h3("Conditions")
		d.table([]string{"No.", "Condition", "Owner", "Due", "Holds back", "Status"}, rows)
		d.addRow("/spec/conditions", "Condition")
	}
	if len(sos) > 0 {
		// Blank cells stay for a wet signature (TAXONOMY.md D52); a line
		// signed in Cartograph shows the decision, who and when.
		d.h3("Sign-off")
		d.flush()
		d.b.WriteString("<table class=\"signoff\">\n<tr><th>Stage</th><th>Signs</th><th>Signature</th><th>Date</th></tr>\n")
		for i, so := range sos {
			at := fmt.Sprintf("/spec/signOffs/%d", i)
			signs := p.who(so["role"])
			if l := str(so["label"]); l != "" {
				signs = l + " (" + signs + ")"
			}
			var signed, date string
			if evs := p.events["signOffs/"+str(so["id"])]; len(evs) > 0 {
				last := evs[len(evs)-1]
				signed, date = label("decision", str(last["decision"]))+": "+str(last["recordedBy"]), when(str(last["date"]))
			}
			d.b.WriteString("<tr><td>")
			d.b.WriteString(valueHTML(d.at(label("stage", str(so["stage"])), at+"/stage")))
			d.b.WriteString("</td><td>")
			d.b.WriteString(valueHTML(d.from(signs, at+"/role")))
			d.b.WriteString("</td><td>")
			d.b.WriteString(esc(signed))
			d.b.WriteString("</td><td>")
			d.b.WriteString(esc(date))
			d.b.WriteString("</td></tr>\n")
		}
		d.b.WriteString("</table>\n")
	}
	return true
}

// budgetWithComponents is the project's own budget and, where the work it
// depends on carries funding of its own, the total with it: a sponsor
// approving the parent approves the whole (TAXONOMY.md D46).
func budgetWithComponents(ctx context.Context, e *engine.Engine, n names, g engine.ComponentGraph, self engine.Ref, own []map[string]any, ownTotal string) string {
	specs := map[string]map[string]any{}
	each(ctx, e, "Project", func(id, _ string, spec map[string]any) { specs[id] = spec })
	seen := map[engine.Ref]bool{self: true}
	var walk func(at engine.Ref)
	var funding []map[string]any
	walk = func(at engine.Ref) {
		for _, ed := range g.Edges {
			if ed.From != at || seen[ed.To] {
				continue
			}
			seen[ed.To] = true
			if ed.To.Kind == "Project" {
				funding = append(funding, list(specs[ed.To.ID]["funding"])...)
			}
			walk(ed.To)
		}
	}
	walk(self)
	if len(funding) == 0 {
		return ownTotal
	}
	_, all := budget(n, append(append([]map[string]any(nil), own...), funding...))
	if ownTotal == "" {
		return all + " across its components"
	}
	return all + " with its components (" + ownTotal + " its own)"
}

// problemSentence writes one problem as a sentence: "Failed deliveries
// are learned of a season late, because checks happen after dispatch.
// This affects delivering members and retail partners. The project will
// change this so that every delivery is checked at intake." Each part
// keeps its own words,
// lower-cased where the sentence runs on (an acronym keeps its capitals).
// In an editable charter the groups are mentions and every part is
// marked by its field (data-part), with what belongs there (data-hint)
// where nothing is said yet.
func (d *doc) problemSentence(n names, pm map[string]any, at string) string {
	problem, change := obj(pm["problem"]), obj(pm["change"])
	groups := n.all("BeneficiaryGroup", strs(pm["groups"]))
	part := func(value, path, hint string) string {
		value = strings.TrimSuffix(strings.TrimSpace(value), ".")
		if !d.editable {
			return esc(value)
		}
		empty := ""
		if value == "" {
			empty = ` data-empty=""`
		}
		return `<span data-field="` + esc(path) + `" data-part="" data-hint="` + esc(hint) + `"` + empty + `>` + esc(value) + `</span>`
	}
	var b strings.Builder
	b.WriteString(`<p class="problem-statement">`)
	// The situation leads, as written: it often names its own subject
	// ("members decide…"), so the groups follow it rather than being set
	// before it, where they doubled the subject (cartograph-engine#81).
	b.WriteString(part(capital(str(problem["situation"])), at+"/problem/situation", "what goes wrong"))
	// Why it happens is the first cause found; a cause written before
	// the list existed reads as the first of it.
	cause, causeAt := "", at+"/problem/causes/0/cause"
	if causes := list(problem["causes"]); len(causes) > 0 {
		cause = str(causes[0]["cause"])
	} else if c := str(problem["cause"]); c != "" {
		cause, causeAt = c, at+"/problem/cause"
	}
	if cause = lowerFirst(cause); cause != "" || d.editable {
		b.WriteString(", because " + part(cause, causeAt, "why it happens"))
	}
	b.WriteString(".")
	switch {
	case d.editable:
		empty := ""
		if len(groups) == 0 {
			empty = ` data-empty=""`
		}
		b.WriteString(` This affects <span data-field="` + esc(at+"/groups") + `" data-part="" data-hint="who it affects"` + empty + `>`)
		for _, g := range groups {
			b.WriteString(`<span class="mention">` + esc(g) + `</span>`)
		}
		b.WriteString(`</span>.`)
	case len(groups) > 0:
		b.WriteString(" This affects " + esc(andList(groups)) + ".")
	}
	if what := lowerFirst(str(change["what"])); what != "" || d.editable {
		b.WriteString(" The project will change this so that " + part(what, at+"/change/what", "what will be true instead"))
		if gain := lowerFirst(str(change["gain"])); gain != "" {
			b.WriteString(", and they gain " + part(gain, at+"/change/gain", "what they gain"))
		}
		b.WriteString(".")
	}
	b.WriteString("</p>\n")
	return b.String()
}

// andList joins names as a sentence does: "a", "a and b", "a, b and c".
func andList(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}
