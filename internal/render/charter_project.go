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
	d, err := projectCharterDoc(ctx, e, id, vers)
	if err != nil {
		return nil, err
	}
	return d.end(), nil
}

// projectCharterDoc writes a project's charter, all but its end.
func projectCharterDoc(ctx context.Context, e *engine.Engine, id string, vers engine.Version) (*doc, error) {
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
	d.brand(ctx, e)
	d.head(name, kindLine, vers)
	checks, _ := e.ProjectChecks(ctx, id, false)
	d.control(vers, docControl{
		Reference:      aliasOf(e, vers),
		Classification: str(spec["classification"]),
		Owner:          strings.Join(roleNames(n, resources, "manager"), ", "),
		Sponsor:        strings.Join(roleNames(n, resources, "sponsor"), ", "),
		Status:         readinessWord(checks.Items),
	})

	// A brief, not the definition retyped (TAXONOMY.md D55): what a
	// sponsor approves, in the order they ask it. Every register prints
	// the rows a decision turns on; the rest stay in Cartograph.
	d.h2("At a glance")
	if about := str(summary["about"]); about != "" {
		d.lead(capital(about))
	}
	var authority []string
	for _, m := range list(spec["mandate"]) {
		authority = append(authority, str(m["title"]))
	}
	d.facts(
		field{"Sponsor", strings.Join(roleNames(n, resources, "sponsor"), ", ")},
		field{"Project manager", strings.Join(roleNames(n, resources, "manager"), ", ")},
		field{"Lead team", n.of("Team", str(spec["team"]))},
		field{"Programme", strings.Join(programmes, "; ")},
		field{"Start", start},
		field{"End", finish},
		field{"Budget", budgetWithComponents(ctx, e, n, graph, self, list(spec["funding"]), total)},
		field{"Handover to", operation},
	)
	d.fields(
		field{"Outcomes served", strings.Join(n.all("Goal", strs(alignment["goals"])), "; ")},
		field{"Authority", strings.Join(authority, "; ")},
	)
	d.readinessBrief(checks.Items)

	// The one objective (TAXONOMY.md D54) and how it is measured.
	objectives := list(spec["objectives"])
	if len(objectives) > 0 {
		d.h2("Objective and key results")
		for i, o := range objectives {
			title := capital(str(o["objective"]))
			if len(objectives) > 1 {
				title = fmt.Sprintf("Objective %d. %s", i+1, title)
			}
			d.h3named(title)
			var rows [][]string
			for _, kr := range list(o["keyResults"]) {
				unit := func(v string) string { return withUnit(kr, v) }
				rows = append(rows, []string{
					capital(str(kr["metric"])),
					baselineWords(p, kr, obj(kr["baseline"])),
					p.target(kr["target"], unit),
					n.of("DataSource", str(kr["source"])),
				})
			}
			d.table([]string{"Key result", "Baseline", "Target", "Data source"}, rows)
		}
	}

	d.problemsBrief(n, list(summary["problems"]))
	if in, out := strs(summary["scopeIn"]), strs(summary["scopeOut"]); len(in)+len(out) > 0 {
		d.h2("Scope")
		d.scopeColumns(in, out)
	}

	// The work it depends on, and the chain that sets the end date
	// (TAXONOMY.md D46).
	if len(dependsOnRows)+len(usedBy) > 0 {
		d.h2("Components and critical path")
		d.raw(dependencyDiagram(n, graph, self))
		d.criticalLine(n, graph)
	}

	if len(list(spec["milestones"])) > 0 {
		d.h2("Schedule")
		if items, err := e.Schedule(ctx, id); err == nil {
			labels := map[string]string{}
			for i, m := range list(spec["milestones"]) {
				labels[str(m["id"])] = itemNumber("M", str(m["id"]), i) + "  " + str(m["name"])
			}
			d.raw(scheduleChart(items, labels))
			d.keyMilestones(p, items)
		}
	} else if len(phaseRows) > 0 {
		d.h2("Schedule")
		d.table([]string{"Phase", "Start", "End", "Months"}, phaseRows)
	}

	if len(list(spec["deliverables"])) > 0 {
		d.h2("Deliverables")
		d.deliverablesBrief(p)
	}

	d.h2("Governance")
	var roles [][]string
	for _, r := range resources {
		if uniqueRoles[str(r["role"])] {
			roles = append(roles, []string{label("role", str(r["role"])), n.of("Resource", str(r["resource"]))})
		}
	}
	d.table([]string{"Role", "Post"}, roles)
	d.fields(field{"Escalation route", escalation(p, spec)})

	if len(fundingRows)+len(list(spec["costs"])) > 0 {
		d.h2("Budget")
		if len(fundingRows) > 0 {
			if strings.Contains(total, ";") || len(fundingRows) > 1 {
				fundingRows = append(fundingRows, []string{total, "Total", ""})
			}
			d.table([]string{"Amount", "Funding source", "Status"}, fundingRows)
		}
		d.costsBrief(p)
	}

	d.constraintsBrief(spec)
	d.risksBrief(p, list(spec["risks"]))

	// How anyone will know it worked, and who runs it afterwards.
	criteria := list(spec["successCriteria"])
	if len(criteria) > 0 {
		d.h2("Success criteria and handover")
		var rows [][]string
		results := false
		for _, c := range criteria {
			judged := str(c["when"])
			if judged == "" {
				judged = "atClosing"
			}
			result := p.result(str(c["id"]))
			results = results || result != ""
			rows = append(rows, []string{
				str(c["statement"]), str(c["standard"]), label("when", judged), p.who(c["confirmedBy"]), result,
			})
		}
		heads := []string{"Criterion", "Target", "Judged", "Signed off by", "Result"}
		if !results {
			// Before anything is judged, the charter reads as it always has.
			heads = heads[:4]
			for i := range rows {
				rows[i] = rows[i][:4]
			}
		}
		d.table(heads, rows)
	}
	var kpiNames []string
	for _, k := range list(spec["kpis"]) {
		kpiNames = append(kpiNames, n.of("KPI", str(k["kpi"])))
	}
	d.fields(
		field{"Performance indicators", strings.Join(kpiNames, "; ")},
		field{"Handover to", operation},
	)

	// Approval: the conditions and sign-off lines the definition names,
	// or, for one that names none, the roles the standards expect to sign
	// (GovS 002 6.4.8).
	if !d.approvalBrief(p) {
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
	d.history(ctx, e, "Project", id)
	d.heldInCartograph(spec, len(stakeholders(ctx, e, n, "Project", id)))
	return &d, nil
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
