package render

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
)

// projectCharter is the project's founding document, in the order the
// standards' own documents take (PRINCE2 PID, PMI charter, World Bank PAD):
// document control and the readiness of each part, the problem, what it
// will achieve and produce, when, who decides and pays, what could go
// wrong, how anyone will know it worked, and who approves it.
func projectCharter(ctx context.Context, e *engine.Engine, id string, vers engine.Version) ([]byte, error) {
	name, spec, err := manifestOf(e, vers, id)
	if err != nil {
		return nil, err
	}
	n := loadNames(ctx, e)
	p := newPlan(n, spec)
	summary := obj(spec["summary"])
	alignment := obj(spec["alignment"])
	resources := list(spec["resources"])
	operation := landsIn(ctx, e, n, str(spec["operation"]))
	timeline := obj(spec["timeline"])
	phaseRows, finish := milestones(str(timeline["start"]), list(timeline["phases"]))
	start := month(str(timeline["start"]))
	if first, last := scheduleSpan(spec); first != "" {
		start, finish = when(first), when(last)
	}
	fundingRows, total := budget(n, list(spec["funding"]))

	kindLine := "Project charter"
	parent := str(alignment["partOf"])
	programmes := n.all("Programme", strs(alignment["programmes"]))
	graph, _ := e.Components(ctx)
	self := engine.Ref{Kind: "Project", ID: id}
	var usedBy, dependsOnRows []string
	for _, ed := range graph.Edges {
		if ed.To == self && ed.From.Kind == "Programme" && !hasString(programmes, n.of("Programme", ed.From.ID)) {
			programmes = append(programmes, n.of("Programme", ed.From.ID))
		}
		if ed.To == self {
			usedBy = append(usedBy, n.of(ed.From.Kind, ed.From.ID))
		}
		if ed.From == self {
			dependsOnRows = append(dependsOnRows, n.of(ed.To.Kind, ed.To.ID))
		}
	}

	var d doc
	d.head(name, kindLine, vers)
	checks, _ := e.ProjectChecks(ctx, id, false)
	d.control(vers, docControl{
		Reference:      aliasOf(e, vers),
		Classification: str(spec["classification"]),
		Owner:          strings.Join(roleNames(n, resources, "manager"), ", "),
		Sponsor:        strings.Join(roleNames(n, resources, "sponsor"), ", "),
		Status:         readinessWord(checks.Items),
	})
	d.contents()

	// At a glance: what anybody opening a charter looks for first (PMI:
	// sponsor, manager, summary schedule and budget), and how ready each
	// part of the definition is.
	d.h2("At a glance")
	if about := str(summary["about"]); about != "" {
		d.lead(capital(about))
	}
	d.facts(
		field{"Sponsor", strings.Join(roleNames(n, resources, "sponsor"), ", ")},
		field{"Project manager", strings.Join(roleNames(n, resources, "manager"), ", ")},
		field{"Lead team", n.of("Team", str(spec["team"]))},
		field{"Programme", strings.Join(programmes, "; ")},
		field{"Start", start},
		field{"End", finish},
		field{"Budget", total},
		field{"Handover to", operation},
	)
	d.readiness(checks.Items)

	// 1. The problem, and what is in and out.
	d.problems(n, list(summary["problems"]), "/spec/summary/problems")
	d.sections(spec, "aim")
	if in, out := strs(summary["scopeIn"]), strs(summary["scopeOut"]); len(in)+len(out) > 0 {
		d.h2("Scope")
		d.scopeColumns(in, out)
	}
	d.sections(spec, "scope")

	// 2. What it will achieve (the results framework) and what it produces.
	objectives := list(spec["objectives"])
	if len(objectives) > 0 {
		d.h2("Objectives and key results")
		for i, o := range objectives {
			d.h3named(fmt.Sprintf("Objective %d. %s", i+1, capital(str(o["objective"]))))
			var rows [][]string
			for _, kr := range list(o["keyResults"]) {
				unit := func(v string) string { return withUnit(kr, v) }
				rows = append(rows, []string{
					capital(str(kr["metric"])),
					chip("direction", str(kr["direction"])),
					baselineWords(p, kr, obj(kr["baseline"])),
					p.target(kr["target"], unit),
					n.of("DataSource", str(kr["source"])),
				})
			}
			d.table([]string{"Key result", "Direction", "Baseline", "Target", "Data source"}, rows)
		}
	}
	d.sections(spec, "measures")

	if len(list(spec["deliverables"])) > 0 {
		d.h2("Deliverables")
		d.register(p)
		d.workstreams(p)
		if wbs := workBreakdown(n, spec); hasTasks(wbs) {
			d.h3("Work breakdown")
			var rows [][]string
			for _, w := range wbs {
				rows = append(rows, []string{w.Code, w.Name, w.Role, w.Note})
			}
			d.table([]string{"Code", "Deliverable or task", "Done by", "Note"}, rows)
		}
	}
	d.sections(spec, "deliverables")

	// 3. When: milestones, or phases from before 2.9.
	if len(list(spec["milestones"])) > 0 {
		d.h2("Schedule and milestones")
		d.milestonePlan(p)
	} else if len(phaseRows) > 0 {
		d.h2("Milestones")
		d.table([]string{"Phase", "Start", "End", "Months"}, phaseRows)
	}
	d.sections(spec, "timeline")

	// 4. What it depends on and what depends on it (TAXONOMY.md D46).
	if len(dependsOnRows)+len(usedBy) > 0 {
		d.h2("Components and dependencies")
		d.dependencyTable(n, graph, self)
	}

	// 5. Why: what it serves, its authority.
	d.h2("Strategic alignment")
	d.fields(
		field{"Outcomes served", strings.Join(n.all("Goal", strs(alignment["goals"])), "; ")},
		field{"Programme", strings.Join(programmes, "; ")},
		field{"Part of", n.of("Project", parent)},
	)
	d.mandate(n, spec, list(spec["mandate"]))
	d.sections(spec, "goals")

	// 6. Who decides, who is involved, who it is for.
	d.h2("Governance and roles")
	var groups []string
	for _, b := range list(summary["beneficiaries"]) {
		groups = append(groups, n.of("BeneficiaryGroup", str(b["group"])))
	}
	d.fields(
		field{"Lead team", n.of("Team", str(spec["team"]))},
		field{"Beneficiaries", strings.Join(groups, ", ")},
		field{"Escalation route", escalation(p, spec)},
	)
	var roles [][]string
	for _, r := range resources {
		roles = append(roles, []string{label("role", str(r["role"])), n.of("Resource", str(r["resource"])), str(r["note"])})
	}
	d.table([]string{"Role", "Post", "Note"}, roles)
	d.raci(p)
	d.sections(spec, "resources")
	d.stakeholders(stakeholders(ctx, e, n, "Project", id))
	d.sections(spec, "stakeholders")
	d.sections(spec, "beneficiaries")

	// 7. Money.
	if len(fundingRows)+len(list(spec["costs"]))+len(list(spec["procurement"])) > 0 {
		d.h2("Resources and budget")
		if len(fundingRows) > 0 {
			if strings.Contains(total, ";") || len(fundingRows) > 1 {
				fundingRows = append(fundingRows, []string{total, "Total", ""})
			}
			d.h3("Funding")
			d.table([]string{"Amount", "Funding source", "Status"}, fundingRows)
		}
		d.costs(p)
	}

	d.data(n, obj(spec["data"]))
	d.sections(spec, "data")
	d.risks(n, spec, list(spec["risks"]))
	d.sections(spec, "risks")

	// 8. How anyone will know it worked, and who runs it afterwards.
	criteria := list(spec["successCriteria"])
	kpis := list(spec["kpis"])
	if len(criteria)+len(kpis) > 0 {
		d.h2("Success criteria and handover")
	}
	for _, w := range []string{"atClosing", "atLanding", "postClosingCycle"} {
		var rows [][]string
		for _, c := range criteria {
			cw := str(c["when"])
			if cw == "" {
				cw = "atClosing"
			}
			if cw != w {
				continue
			}
			rows = append(rows, []string{
				str(c["statement"]), str(c["standard"]),
				n.ref(c["source"], "DataSource", spec), n.ref(c["cycle"], "ReportingCycle", spec),
				n.ref(c["owner"], "Resource", spec), n.ref(c["confirmedBy"], "Resource", spec),
				p.status("successCriteria", str(c["id"])),
			})
		}
		if len(rows) == 0 {
			continue
		}
		d.h3(label("when", w))
		d.table([]string{"Criterion", "Target", "Data source", "Frequency", "Measured by", "Signed off by", "Result"}, rows)
	}
	var measures [][]string
	for _, k := range kpis {
		measures = append(measures, []string{n.of("KPI", str(k["kpi"])), str(k["reason"])})
	}
	if len(measures) > 0 {
		d.h3("Performance indicators")
		d.table([]string{"Indicator", "Rationale"}, measures)
	}
	d.fields(field{"Handover to", operation})
	d.sections(spec, "success")
	d.sections(spec, "landing")

	var assumed []string
	seen := map[string]bool{}
	for _, c := range criteria {
		for _, a := range strs(c["assumes"]) {
			if !seen[a] {
				seen[a] = true
				assumed = append(assumed, n.of("Assumption", a))
			}
		}
	}
	if len(assumed) > 0 {
		d.h2("Assumptions")
		d.list("", assumed)
	}

	var obligations [][]string
	for _, c := range list(spec["compliance"]) {
		obligations = append(obligations, []string{str(c["item"]), chip("status", str(c["status"]))})
	}
	if len(obligations) > 0 {
		d.h2("Compliance")
		d.table([]string{"Requirement", "Status"}, obligations)
	}

	// 9. Approval: the conditions and sign-off lines the definition names,
	// or, for one that names none, the roles the standards expect to sign
	// (GovS 002 6.4.8).
	if !d.approval(p) {
		var approvers []string
		for _, role := range []string{"sponsor", "manager", "serviceOwner"} {
			names := roleNames(n, resources, role)
			if len(names) == 0 && role != "serviceOwner" {
				names = []string{label("role", role)}
			}
			for _, who := range names {
				approvers = append(approvers, label("role", role)+": "+who)
			}
		}
		if parent != "" {
			approvers = append(approvers, "Parent project manager: "+n.of("Project", parent))
		}
		d.signOff(approvers)
	}
	d.sections(spec, "approval")
	d.record(p)
	printed := map[string]bool{}
	for _, s := range []string{"aim", "scope", "measures", "deliverables", "timeline", "goals", "resources", "stakeholders", "beneficiaries", "data", "risks", "success", "landing", "approval"} {
		printed[s] = true
	}
	d.otherSections(spec, printed)
	d.history(ctx, e, "Project", id)
	d.heldElsewhere(spec)
	return d.end(), nil
}

func hasString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// scheduleSpan is the first and last date a project's milestones name.
func scheduleSpan(spec map[string]any) (first, last string) {
	var dates []string
	for _, m := range list(spec["milestones"]) {
		t := obj(m["timing"])
		for _, k := range []string{"date", "notBefore", "notAfter", "expectedBy"} {
			if s := str(t[k]); s != "" {
				dates = append(dates, s)
			}
		}
	}
	if len(dates) == 0 {
		return "", ""
	}
	sort.Strings(dates)
	return dates[0], dates[len(dates)-1]
}

func withUnit(kr map[string]any, value string) string {
	switch str(kr["kind"]) {
	case "percent":
		return value + "%"
	default:
		if unit := str(kr["unit"]); unit != "" {
			return value + " " + unit
		}
	}
	return value
}

// baselineWords prints a baseline: a value and its month, or why it is not
// known and when it will be.
func baselineWords(p plan, kr, b map[string]any) string {
	if r := str(b["unknownReason"]); r != "" {
		out := "Not yet known: " + lowerFirst(r)
		if ev, ok := b["when"]; ok {
			out += " Known when " + p.event(ev)
		}
		if e := month(str(b["expectedBy"])); e != "" {
			out += ", expected by " + e
		}
		return out
	}
	return reading(kr, b)
}

func escalation(p plan, spec map[string]any) string {
	var out []string
	for _, r := range anyList(spec["escalationRoute"]) {
		if s := p.item(r); s != "" {
			out = append(out, s)
		}
	}
	return strings.Join(out, " → ")
}

// aliasOf is the reference the organisation's register gives the work.
func aliasOf(e *engine.Engine, vers engine.Version) string {
	m, err := e.Codec().Decode(vers.YAML)
	if err != nil {
		return ""
	}
	return str(obj(m["metadata"])["alias"])
}
