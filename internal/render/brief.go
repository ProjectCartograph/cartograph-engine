package render

import (
	"fmt"
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

// whoAll names several parties briefly.
func (p plan) whoAll(v any) string {
	var out []string
	for _, r := range anyList(v) {
		out = append(out, p.who(r))
	}
	return strings.Join(out, ", ")
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
		d.b.WriteString("<p class=\"ready-line\">" + chipHTML("readiness", "ready") + " " + esc(fmt.Sprintf("Every part of the definition is ready: %d checks met.", ok)) + "</p>\n")
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

// raciBrief prints the responsibilities with parties named by role.
func (d *doc) raciBrief(p plan) {
	rs := list(p.spec["responsibilities"])
	if len(rs) == 0 {
		return
	}
	var rows [][]string
	for _, r := range rs {
		rows = append(rows, []string{str(r["item"]), p.whoAll(r["responsible"]), p.who(r["accountable"]), p.whoAll(r["consulted"]), p.whoAll(r["informed"])})
	}
	d.h3("Responsibilities")
	d.table([]string{"Decision or deliverable", "Responsible", "Accountable", "Consulted", "Informed"}, rows)
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
	var rows [][]string
	for _, r := range items {
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
		rows = append(rows, []string{chip("riskType", str(r["type"])), str(r["description"]), chip("level", str(r["impact"])), owner, str(r["mitigation"])})
	}
	if len(rows) == 0 && len(items) == 0 {
		return
	}
	d.h2("Key risks")
	if len(rows) > 0 {
		d.table([]string{"Type", "Risk", "Impact", "Owner", "Response"}, rows)
	}
	if rest := len(items) - len(rows); rest > 0 {
		d.note(fmt.Sprintf("%d more risk%s, dependencies and constraints of lower impact in Cartograph.", rest, plural(rest)))
	}
}

// note is a quiet line under a table.
func (d *doc) note(s string) {
	d.flush()
	d.b.WriteString("<p class=\"note\">" + esc(s) + "</p>\n")
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
