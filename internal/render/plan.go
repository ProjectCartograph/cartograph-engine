package render

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// The plan parts of a charter (TAXONOMY.md D47 to D53): timings in words,
// milestones and what each waits on, the deliverable register, workstreams,
// responsibilities, costs, procurement, conditions, sign-off and what has
// happened. Each prints only when the definition has it.

// plan reads one project's own lists, so a local reference prints a name.
type plan struct {
	n    names
	spec map[string]any
	// events by the local item they happened to, newest last.
	events map[string][]map[string]any
}

func newPlan(n names, spec map[string]any) plan {
	p := plan{n: n, spec: spec, events: map[string][]map[string]any{}}
	evs := list(spec["events"])
	sort.SliceStable(evs, func(i, j int) bool { return str(evs[i]["date"]) < str(evs[j]["date"]) })
	for _, ev := range evs {
		on := obj(ev["on"])
		key := str(on["local"]) + "/" + str(on["id"])
		if str(on["local"]) == "" {
			key = "ext/" + str(on["external"]) + str(on["kind"]) + str(on["id"])
		}
		p.events[key] = append(p.events[key], ev)
	}
	return p
}

// when prints "2026-09-02" as "2 September 2026" and "2026-09" as
// "September 2026".
func when(s string) string {
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.Format("2 January 2006")
	}
	return month(s)
}

// local names an item of this project's own lists.
func (p plan) local(listName, id string) string {
	for _, it := range list(p.spec[listName]) {
		if str(it["id"]) == id {
			for _, k := range []string{"name", "statement", "action", "label", "description", "category"} {
				if s := str(it[k]); s != "" {
					return s
				}
			}
		}
	}
	return id
}

// item names whatever a reference points at.
func (p plan) item(v any) string {
	r := obj(v)
	if ext := str(r["external"]); ext != "" {
		return ext
	}
	if l := str(r["local"]); l != "" {
		if l == "resources" {
			return p.n.ref(v, "Resource", p.spec)
		}
		return p.local(l, str(r["id"]))
	}
	return p.n.ref(v, "", p.spec)
}

var happensWords = map[string]string{
	"reached": "is reached", "accepted": "is accepted", "met": "is met", "firstReading": "has its first reading",
	"issued": "is issued", "decided": "is decided", "approved": "is approved", "closed": "closes",
	"landed": "is handed over", "occurred": "happens",
}

// event says an event in words: "the Handbook is accepted".
func (p plan) event(v any) string {
	ev := obj(v)
	what := p.item(ev["on"])
	if what == "" {
		return ""
	}
	verb := happensWords[str(ev["happens"])]
	if verb == "" {
		switch str(obj(ev["on"])["local"]) {
		case "milestones":
			verb = "is reached"
		case "deliverables":
			verb = "is accepted"
		case "conditions":
			verb = "is met"
		case "":
			verb = ""
		default:
			verb = "happens"
		}
	}
	return strings.TrimSpace(what + " " + verb)
}

// timing says a timing in words.
func (p plan) timing(v any) string {
	t := obj(v)
	var out string
	switch str(t["form"]) {
	case "date":
		out = when(str(t["date"]))
	case "window":
		from, to := when(str(t["notBefore"])), when(str(t["notAfter"]))
		switch {
		case from != "" && to != "":
			out = "Between " + from + " and " + to
		case from != "":
			out = "Not before " + from
		default:
			out = "By " + to
		}
	case "after":
		lag := ""
		if m := number(t["lagMonths"]); m != "" && m != "0" {
			lag = m + " month" + map[bool]string{true: "", false: "s"}[m == "1"] + " after "
		} else if dd := number(t["lagDays"]); dd != "" && dd != "0" {
			lag = dd + " day" + map[bool]string{true: "", false: "s"}[dd == "1"] + " after "
		} else {
			lag = "After "
		}
		out = capital(lag + p.event(t["event"]))
	case "when":
		out = "When " + p.event(t["event"])
		if e := when(str(t["expectedBy"])); e != "" {
			out += ", expected by " + e
		}
		if by := p.item(t["decidedBy"]); by != "" {
			out += "; set by " + by
		}
	}
	if note := str(t["note"]); note != "" && out != "" {
		out += " (" + note + ")"
	}
	return out
}

// target says a target in any of its shapes, with the measure's unit.
func (p plan) target(v any, unit func(string) string) string {
	t := obj(v)
	if sw, ok := t["setWhen"]; ok {
		s := "Set " + strings.TrimPrefix(lowerFirst(p.timing(sw)), "")
		if dir := str(t["direction"]); dir != "" {
			s = capital(dir) + ". " + s
		}
		return s
	}
	val := number(t["value"])
	if val == "" {
		return ""
	}
	val = unit(val)
	if due, ok := t["due"]; ok {
		return val + ", " + lowerFirst(p.timing(due))
	}
	if m := month(str(t["date"])); m != "" {
		val += ", " + m
	}
	return val
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	if len(r) > 1 && r[1] >= 'a' && r[1] <= 'z' {
		r[0] = []rune(strings.ToLower(string(r[0])))[0]
	}
	return string(r)
}

// status is what last happened to a local item, in words: "Reached, 31
// December 2025".
func (p plan) status(listName, id string) string {
	evs := p.events[listName+"/"+id]
	if len(evs) == 0 {
		return ""
	}
	last := evs[len(evs)-1]
	return label("happened", str(last["happened"])) + ", " + when(str(last["date"]))
}

func (p plan) evidence(listName, id, planned string) string {
	evs := p.events[listName+"/"+id]
	for i := len(evs) - 1; i >= 0; i-- {
		if e := str(evs[i]["evidence"]); e != "" {
			return e
		}
	}
	return planned
}

func init() {
	labels["happened"] = map[string]string{
		"reached": "Reached", "slipped": "Slipped", "accepted": "Accepted", "rejected": "Rejected",
		"met": "Met", "notMet": "Not met", "occurred": "Occurred", "issued": "Issued",
		"decided": "Decided", "signed": "Signed",
	}
	labels["costStatus"] = map[string]string{
		"approved": "Approved", "requested": "Requested", "beingCosted": "Being costed", "unfunded": "Not funded",
	}
	labels["stage"] = map[string]string{"definition": "Definition", "closing": "Closure", "handover": "Handover"}
	labels["decision"] = map[string]string{"approve": "Approved", "approveWithConditions": "Approved with conditions", "reject": "Not approved"}
}

// milestones prints the schedule: each milestone, when it falls, what it
// waits on, its owner and what has happened.
func (d *doc) milestonePlan(p plan) {
	ms := list(p.spec["milestones"])
	if len(ms) == 0 {
		return
	}
	var rows [][]string
	for i, m := range ms {
		var waits []string
		for _, w := range list(m["waitsOn"]) {
			waits = append(waits, p.event(w))
		}
		var marks []string
		for _, did := range strs(m["deliverables"]) {
			marks = append(marks, p.local("deliverables", did))
		}
		name := str(m["name"])
		if len(marks) > 0 {
			name += " (" + strings.Join(marks, "; ") + ")"
		}
		rows = append(rows, []string{
			fmt.Sprintf("M%d", i+1), name, p.timing(m["timing"]), strings.Join(waits, "; "),
			p.item(m["owner"]), p.evidence("milestones", str(m["id"]), str(m["evidence"])), p.status("milestones", str(m["id"])),
		})
	}
	d.table([]string{"No.", "Milestone", "When", "Also waits on", "Owner", "Evidence", "Status"}, rows)
}

// register prints the deliverable register (TAXONOMY.md D49).
func (d *doc) register(p plan) {
	var rows [][]string
	for i, dv := range list(p.spec["deliverables"]) {
		var tests []string
		for _, a := range list(dv["acceptance"]) {
			who := p.n.ref(a["by"], "Resource", p.spec)
			test := str(a["outcome"])
			if test == "" {
				continue
			}
			if who != "" {
				test = who + ": " + test
			}
			tests = append(tests, capital(test))
		}
		name := str(dv["name"])
		if desc := str(dv["description"]); desc != "" {
			name += ". " + desc
		}
		rows = append(rows, []string{
			fmt.Sprintf("D%d", i+1), name, p.item(dv["owner"]), p.timing(dv["due"]),
			strings.Join(tests, "; "), p.evidence("deliverables", str(dv["id"]), str(dv["evidence"])),
			p.status("deliverables", str(dv["id"])),
		})
	}
	d.table([]string{"No.", "Deliverable", "Owner", "Due", "Accepted when", "Evidence", "Status"}, rows)
}

func (d *doc) workstreams(p plan) {
	ws := list(p.spec["workstreams"])
	if len(ws) == 0 {
		return
	}
	d.h3("Workstreams")
	var rows [][]string
	for i, w := range ws {
		var sup []string
		for _, s := range list(w["supporting"]) {
			sup = append(sup, p.item(s))
		}
		var ds []string
		for _, dv := range list(p.spec["deliverables"]) {
			if str(dv["workstream"]) == str(w["id"]) {
				ds = append(ds, str(dv["name"]))
			}
		}
		purpose := str(w["purpose"])
		if len(ds) > 0 {
			purpose = strings.TrimSpace(purpose + " Delivers: " + strings.Join(ds, "; ") + ".")
		}
		rows = append(rows, []string{fmt.Sprintf("WS%d", i+1), str(w["name"]), purpose, p.item(w["lead"]), strings.Join(sup, ", "), str(w["dependsOn"])})
	}
	d.table([]string{"No.", "Workstream", "Purpose", "Lead", "Supported by", "Needs from outside"}, rows)
}

// raci prints responsibilities as a matrix: one row per decision or
// deliverable (TAXONOMY.md D50).
func (d *doc) raci(p plan) {
	rs := list(p.spec["responsibilities"])
	if len(rs) == 0 {
		return
	}
	d.h3("Responsibilities (RACI)")
	refs := func(v any) string {
		var out []string
		for _, r := range anyList(v) {
			out = append(out, p.item(r))
		}
		return strings.Join(out, "; ")
	}
	var rows [][]string
	for _, r := range rs {
		item := str(r["item"])
		if b, _ := r["decision"].(bool); b {
			item += " (decision)"
		}
		rows = append(rows, []string{item, refs(r["responsible"]), p.item(r["accountable"]), refs(r["consulted"]), refs(r["informed"])})
	}
	d.table([]string{"Decision or deliverable", "Responsible", "Accountable", "Consulted", "Informed"}, rows)
}

func anyList(v any) []any {
	l, _ := v.([]any)
	return l
}

// costs prints the cost lines and procurement (TAXONOMY.md D51).
func (d *doc) costs(p plan) {
	cs := list(p.spec["costs"])
	if len(cs) > 0 {
		d.h3("Cost plan")
		var rows [][]string
		for _, c := range cs {
			amount := number(c["amount"])
			if amount != "" {
				amount = strings.TrimSpace(str(c["currency"]) + " " + thousands(amount))
			}
			rows = append(rows, []string{str(c["category"]), str(c["basis"]), amount, str(c["period"]),
				p.n.of("FundingSource", str(c["source"])), label("costStatus", str(c["status"])), str(c["recurrent"])})
		}
		d.table([]string{"Category", "Basis", "Amount", "Period", "Funding source", "Status", "After the project"}, rows)
	}
	ps := list(p.spec["procurement"])
	if len(ps) > 0 {
		d.h3("Procurement plan")
		var rows [][]string
		for _, pr := range ps {
			value := number(pr["value"])
			if value != "" {
				value = strings.TrimSpace(str(pr["currency"]) + " " + thousands(value))
			} else {
				value = str(pr["valueNote"])
			}
			rows = append(rows, []string{str(pr["requirement"]), value, str(pr["method"]), str(pr["leadTime"]), p.timing(pr["requiredBy"]), p.item(pr["owner"])})
		}
		d.table([]string{"Requirement", "Estimated value", "Method", "Lead time", "Required by", "Owner"}, rows)
	}
}

// thousands groups digits: 171600 reads 171,600.
func thousands(s string) string {
	whole, frac, _ := strings.Cut(s, ".")
	neg := strings.HasPrefix(whole, "-")
	whole = strings.TrimPrefix(whole, "-")
	var b strings.Builder
	for i, r := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	out := b.String()
	if frac != "" {
		out += "." + frac
	}
	if neg {
		out = "-" + out
	}
	return out
}

// approval prints the conditions approval carries and the sign-off lines,
// each with who signed and when, or blank for a wet signature.
func (d *doc) approval(p plan) bool {
	cds, sos := list(p.spec["conditions"]), list(p.spec["signOffs"])
	if len(cds)+len(sos) == 0 {
		return false
	}
	d.h2("Approval")
	if len(cds) > 0 {
		d.h3("Conditions")
		var rows [][]string
		for i, c := range cds {
			rows = append(rows, []string{fmt.Sprintf("%d", i+1), str(c["action"]), p.item(c["owner"]), p.timing(c["due"]), p.event(c["gates"]), p.status("conditions", str(c["id"]))})
		}
		d.table([]string{"No.", "Condition", "Owner", "Due", "Gates", "Status"}, rows)
	}
	if len(sos) > 0 {
		d.h3("Sign-off")
		d.flush()
		d.b.WriteString("<table class=\"signoff\">\n<tr><th>Stage</th><th>Role</th><th>Decision</th><th>Signed by</th><th>Date</th></tr>\n")
		for _, s := range sos {
			role := p.item(s["role"])
			if l := str(s["label"]); l != "" {
				role = l + ": " + role
			}
			var decision, by, date string
			if evs := p.events["signOffs/"+str(s["id"])]; len(evs) > 0 {
				last := evs[len(evs)-1]
				decision, by, date = label("decision", str(last["decision"])), str(last["recordedBy"]), when(str(last["date"]))
			}
			d.b.WriteString("<tr><td>" + esc(label("stage", str(s["stage"]))) + "</td><td>" + esc(role) + "</td><td>" + esc(decision) + "</td><td>" + esc(by) + "</td><td>" + esc(date) + "</td></tr>\n")
		}
		d.b.WriteString("</table>\n")
	}
	return true
}

// record prints what has happened, newest first (TAXONOMY.md D52).
func (d *doc) record(p plan) {
	evs := list(p.spec["events"])
	if len(evs) == 0 {
		return
	}
	sort.SliceStable(evs, func(i, j int) bool { return str(evs[i]["date"]) > str(evs[j]["date"]) })
	d.h2("Record of events")
	var rows [][]string
	for _, ev := range evs {
		what := p.item(ev["on"])
		if v := number(ev["value"]); v != "" {
			what += " (measured " + v + ")"
		}
		rows = append(rows, []string{when(str(ev["date"])), what, label("happened", str(ev["happened"])), str(ev["note"]), str(ev["evidence"]), str(ev["recordedBy"])})
	}
	d.table([]string{"Date", "Item", "What happened", "Note", "Evidence", "Recorded by"}, rows)
}

// sections prints the template's own sections that belong beside a step
// (TAXONOMY.md D53).
func (d *doc) sections(spec map[string]any, step string) {
	for _, s := range list(spec["sections"]) {
		if str(s["step"]) != step {
			continue
		}
		d.h3(str(s["heading"]))
		for _, para := range strings.Split(str(s["text"]), "\n") {
			d.p(para)
		}
	}
}

// otherSections prints sections with no step, or a step the charter does
// not print, together at the end.
func (d *doc) otherSections(spec map[string]any, printed map[string]bool) {
	var rest []map[string]any
	for _, s := range list(spec["sections"]) {
		if !printed[str(s["step"])] {
			rest = append(rest, s)
		}
	}
	if len(rest) == 0 {
		return
	}
	d.h2("Further sections")
	for _, s := range rest {
		d.h3(str(s["heading"]))
		for _, para := range strings.Split(str(s["text"]), "\n") {
			d.p(para)
		}
	}
}
