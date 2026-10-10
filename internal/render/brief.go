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

// keyMilestones prints the milestones a sponsor decides on: those on the
// chain that sets the end date, those still to be set by an event, any
// late, and any an approval condition holds back. The chart above shows
// them all.
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
		if !it.Critical && !it.Pending && !it.Late && !gated[id] {
			continue
		}
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
		rows = append(rows, []string{itemNumber("M", id, i), str(m["name"]), p.timing(m["timing"]), p.who(m["owner"]), strings.Join(why, "; "), p.status("milestones", id)})
	}
	if len(rows) == 0 {
		return
	}
	d.h3("Key milestones")
	d.table([]string{"No.", "Milestone", "When", "Owner", "Why it matters", "Status"}, rows)
	if rest := len(ms) - len(rows); rest > 0 {
		d.note(fmt.Sprintf("%d more milestone%s in the chart above and in Cartograph.", rest, plural(rest)))
	}
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
		rows = append(rows, []string{itemNumber("D", str(dv["id"]), i), str(dv["name"]), p.who(dv["owner"]), p.timing(dv["due"]),
			strings.Join(by, ", "), p.status("deliverables", str(dv["id"]))})
	}
	d.table([]string{"No.", "Deliverable", "Owner", "Due", "Accepted by", "Status"}, rows)
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
	for _, c := range cs {
		amount := number(c["amount"])
		if amount != "" {
			amount = strings.TrimSpace(str(c["currency"]) + " " + thousands(amount))
		}
		row := []string{str(c["category"]), amount, label("costStatus", str(c["status"]))}
		if !shared {
			row = append(row, p.n.of("FundingSource", str(c["source"])))
		}
		rows = append(rows, row)
	}
	headers := []string{"Cost line", "Amount", "Status"}
	if !shared {
		headers = append(headers, "Funding source")
	}
	d.h3("Cost plan")
	d.table(headers, rows)
	for s := range sources {
		if shared && s != "" {
			d.note("Every line is funded from " + p.n.of("FundingSource", s) + ".")
		}
	}
}

// risksBrief prints the risks a sponsor must know: high impact, issues
// already happening, and anything escalated. The rest are counted.
func (d *doc) risksBrief(p plan, items []map[string]any) {
	moves := movesOf(p.spec)
	// Highest impact first, then likelihood; at most five, as a charter
	// names the few risks that shape the decision.
	order := map[string]int{"high": 0, "medium": 1, "low": 2}
	sorted := append([]map[string]any(nil), items...)
	sort.SliceStable(sorted, func(i, j int) bool {
		ri, rj := order[str(sorted[i]["impact"])], order[str(sorted[j]["impact"])]
		if ri != rj {
			return ri < rj
		}
		return order[str(sorted[i]["likelihood"])] < order[str(sorted[j]["likelihood"])]
	})
	var rows [][]string
	for _, r := range sorted {
		if len(rows) == 5 {
			break
		}
		escalated := false
		if e, ok := r["escalate"].(map[string]any); ok {
			escalated, _ = e["flag"].(bool)
		}
		if str(r["impact"]) != "high" && str(r["type"]) != "issue" && !escalated {
			continue
		}
		owner := p.who(r["owner"])
		if owner == "" {
			owner = label("role", "manager")
		}
		rows = append(rows, []string{chip("riskType", str(r["type"])), str(r["description"]), chip("level", str(r["impact"])), moves[str(r["id"])], owner, responseOf(r)})
	}
	if len(rows) == 0 && len(items) == 0 {
		return
	}
	d.h2("Key risks")
	if len(rows) > 0 {
		d.table([]string{"Type", "Risk", "Impact", "Moves", "Owner", "Response"}, rows)
	}
	if rest := len(items) - len(rows); rest > 0 {
		d.note(fmt.Sprintf("%d more risk%s, dependencies and constraints of lower impact in Cartograph.", rest, plural(rest)))
	}
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

// problemsBrief prints each problem as three lines: what is wrong, why,
// and the change the project makes, each still naming its field so an
// interface can open it in place.
func (d *doc) problemsBrief(n names, items []map[string]any) {
	if len(items) == 0 {
		return
	}
	d.h2("Problem statement")
	for i, pm := range items {
		problem, change := obj(pm["problem"]), obj(pm["change"])
		at := fmt.Sprintf("/spec/summary/problems/%d", i)
		d.fieldsAt(
			pathed{"Problem", capital(str(problem["situation"])), at + "/problem/situation"},
			pathed{"Cause", capital(str(problem["cause"])), at + "/problem/cause"},
			pathed{"Change", capital(str(change["what"])), at + "/change/what"},
			pathed{"Affects", strings.Join(n.all("BeneficiaryGroup", strs(pm["groups"])), ", "), ""},
		)
	}
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
			rows = append(rows, []string{fmt.Sprintf("%d", i+1), str(c["action"]), p.who(c["owner"]), p.timing(c["due"]), holds, p.status("conditions", str(c["id"]))})
		}
		d.h3("Conditions")
		d.table([]string{"No.", "Condition", "Owner", "Due", "Holds back", "Status"}, rows)
	}
	if len(sos) > 0 {
		// Blank cells stay for a wet signature (TAXONOMY.md D52); a line
		// signed in Cartograph shows the decision, who and when.
		d.h3("Sign-off")
		d.flush()
		d.b.WriteString("<table class=\"signoff\">\n<tr><th>Stage</th><th>Signs</th><th>Signature</th><th>Date</th></tr>\n")
		for _, so := range sos {
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
			d.b.WriteString(esc(label("stage", str(so["stage"]))))
			d.b.WriteString("</td><td>")
			d.b.WriteString(esc(signs))
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
