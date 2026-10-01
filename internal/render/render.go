package render

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
)

// Charter renders a project charter to HTML and JSON for a given snapshot.
// snapshot=0 means the working copy; snapshot>0 means a specific version.
// Returns byte slices for HTML and JSON, both deterministic.
func Charter(ctx context.Context, e *engine.Engine, projectID string, snapshot int) (html, jsonData []byte, err error) {
	var vers engine.Version
	if snapshot == 0 {
		vers, err = e.Get(ctx, "Project", projectID)
	} else {
		vers, err = e.GetVersion(ctx, "Project", projectID, snapshot)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("get project: %w", err)
	}

	manifest, err := e.Codec().Decode(vers.YAML)
	if err != nil {
		return nil, nil, fmt.Errorf("decode manifest: %w", err)
	}

	projectChecks, err := e.ProjectChecks(ctx, projectID, false)
	if err != nil {
		return nil, nil, fmt.Errorf("get checks: %w", err)
	}

	htmlBytes, err := projectCharter(ctx, e, projectID, vers)
	if err != nil {
		return nil, nil, fmt.Errorf("render html: %w", err)
	}

	// The JSON beside it: manifest, version and checks, with sorted keys so
	// two renders of the same snapshot are byte-identical.
	data := map[string]any{
		"manifest": manifest,
		"version":  vers,
		"checks":   projectChecks.Items,
	}
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal json: %w", err)
	}
	var sorted map[string]any
	if err := json.Unmarshal(jsonBytes, &sorted); err != nil {
		return nil, nil, fmt.Errorf("unmarshal json for formatting: %w", err)
	}
	jsonBytes, err = json.MarshalIndent(sorted, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("marshal json with indent: %w", err)
	}
	return htmlBytes, jsonBytes, nil
}

// projectCharter is the project's founding document, in the order the
// standards' own documents take (PRINCE2 PID, PMI charter, World Bank PAD):
// the problem, what it will achieve and produce, why it is being done, who
// is involved, and how anyone will know it worked. Then the working detail.
func projectCharter(ctx context.Context, e *engine.Engine, id string, vers engine.Version) ([]byte, error) {
	name, spec, err := manifestOf(e, vers, id)
	if err != nil {
		return nil, err
	}
	n := loadNames(ctx, e)
	summary := obj(spec["summary"])
	alignment := obj(spec["alignment"])

	kindLine := "Project"
	parent := str(alignment["partOf"])
	if parent != "" {
		kindLine = "Component of " + n.of("Project", parent)
	}

	operation := ""
	if op := str(spec["operation"]); op == "new" {
		operation = "A new service, not yet defined"
	} else {
		operation = n.of("Operation", op)
	}
	resources := list(spec["resources"])
	timeline := obj(spec["timeline"])
	phaseRows, finish := milestones(str(timeline["start"]), list(timeline["phases"]))
	fundingRows, total := budget(n, list(spec["funding"]))
	programmes := n.all("Programme", strs(alignment["programmes"]))
	partOf := strings.Join(programmes, "; ")
	if parent != "" {
		partOf = n.of("Project", parent)
	}

	var d doc
	d.head(name, kindLine, vers)
	// What anybody opening a charter looks for first (PMI: sponsor, manager,
	// summary schedule and budget).
	d.facts(
		field{"Sponsor", strings.Join(roleNames(n, resources, "sponsor"), ", ")},
		field{"Project manager", strings.Join(roleNames(n, resources, "manager"), ", ")},
		field{"Team", n.of("Team", str(spec["team"]))},
		field{"Programme", partOf},
		field{"Start", month(str(timeline["start"]))},
		field{"End", finish},
		field{"Budget", total},
		field{"Handover to", operation},
	)

	// 1. The problem, and what is in and out.
	d.problems(n, list(summary["problems"]))
	if in, out := strs(summary["scopeIn"]), strs(summary["scopeOut"]); len(in)+len(out) > 0 {
		d.h2("Scope")
		d.list("In scope", in)
		d.list("Out of scope", out)
	}

	// 2. What it will achieve (the results framework) and what it produces.
	objectives := list(spec["objectives"])
	if len(objectives) > 0 {
		d.h2("Objectives and key results")
		for _, o := range objectives {
			d.p(capital(str(o["objective"])))
			var rows [][]string
			for _, kr := range list(o["keyResults"]) {
				rows = append(rows, []string{
					capital(str(kr["metric"])),
					chip("direction", str(kr["direction"])),
					reading(kr, obj(kr["baseline"])),
					reading(kr, obj(kr["target"])),
					n.of("DataSource", str(kr["source"])),
				})
			}
			d.table([]string{"Key result", "Direction", "Baseline", "Target", "Data source"}, rows)
		}
	}

	deliverables := list(spec["deliverables"])
	if len(deliverables) > 0 {
		d.h2("Deliverables")
		var rows [][]string
		for _, dv := range deliverables {
			var tests []string
			for _, a := range list(dv["acceptance"]) {
				who := n.ref(a["by"], "Resource", spec)
				test := str(a["outcome"])
				if test == "" {
					continue
				}
				if who != "" {
					test = who + " " + test
				}
				tests = append(tests, capital(test))
			}
			rows = append(rows, []string{str(dv["name"]), str(dv["description"]), strings.Join(tests, "; ")})
		}
		d.table([]string{"Deliverable", "Description", "Acceptance criteria"}, rows)
	}

	// Components are read back from the projects that name this one as
	// their parent (TAXONOMY.md D15), never stored here.
	var components [][]string
	each(ctx, e, "Project", func(cid, cname string, cspec map[string]any) {
		if str(obj(cspec["alignment"])["partOf"]) != id {
			return
		}
		var objective string
		if objs := list(cspec["objectives"]); len(objs) > 0 {
			objective = capital(str(objs[0]["objective"]))
		}
		var made []string
		for _, dv := range list(cspec["deliverables"]) {
			made = append(made, str(dv["name"]))
		}
		components = append(components, []string{cname, objective, strings.Join(made, "; "), n.of("Team", str(cspec["team"]))})
	})
	if len(components) > 0 {
		d.h2("Components")
		d.table([]string{"Component", "Objective", "Deliverables", "Team"}, components)
	}

	// 3. Why it is being done: what it serves, its authority, its money.
	d.h2("Strategic alignment")
	if parent != "" && len(programmes) == 0 {
		programmes = []string{"Through " + n.of("Project", parent)}
	}
	d.fields(
		field{"Goals", strings.Join(n.all("Goal", strs(alignment["goals"])), "; ")},
		field{"Programme", strings.Join(programmes, "; ")},
	)
	d.mandate(list(spec["mandate"]))

	// 4. Who is involved, who it is for, and who runs it afterwards.
	d.h2("Governance and roles")
	var groups []string
	for _, b := range list(summary["beneficiaries"]) {
		groups = append(groups, n.of("BeneficiaryGroup", str(b["group"])))
	}
	d.fields(
		field{"Team", n.of("Team", str(spec["team"]))},
		field{"Beneficiaries", strings.Join(groups, ", ")},
		field{"Handover to", operation},
	)
	var roles [][]string
	for _, r := range resources {
		roles = append(roles, []string{label("role", str(r["role"])), n.of("Resource", str(r["resource"])), str(r["note"])})
	}
	d.table([]string{"Role", "Post", "Note"}, roles)
	d.stakeholders(stakeholders(ctx, e, n, "Project", id))

	// 5. How anyone will know it worked.
	criteria := list(spec["successCriteria"])
	kpis := list(spec["kpis"])
	if len(criteria)+len(kpis) > 0 {
		d.h2("Success criteria")
	}
	for _, when := range []string{"atClosing", "atLanding", "postClosingCycle"} {
		var rows [][]string
		for _, c := range criteria {
			w := str(c["when"])
			if w == "" {
				w = "atClosing"
			}
			if w != when {
				continue
			}
			rows = append(rows, []string{
				str(c["statement"]), str(c["standard"]),
				n.ref(c["source"], "DataSource", spec), n.ref(c["cycle"], "ReportingCycle", spec),
				n.ref(c["owner"], "Resource", spec), n.ref(c["confirmedBy"], "Resource", spec),
			})
		}
		if len(rows) == 0 {
			continue
		}
		d.h3(label("when", when))
		d.table([]string{"Criterion", "Target", "Data source", "Frequency", "Measured by", "Signed off by"}, rows)
	}
	var measures [][]string
	for _, k := range kpis {
		measures = append(measures, []string{n.of("KPI", str(k["kpi"])), str(k["reason"])})
	}
	if len(measures) > 0 {
		d.h3("Performance indicators")
		d.table([]string{"Indicator", "Rationale"}, measures)
	}

	// The working detail: schedule, money, data, risk.
	d.h2("Milestones")
	d.table([]string{"Phase", "Start", "End", "Months"}, phaseRows)

	if len(fundingRows) > 0 {
		d.h2("Budget")
		if strings.Contains(total, ";") || len(fundingRows) > 1 {
			fundingRows = append(fundingRows, []string{total, "Total", ""})
		}
		d.table([]string{"Amount", "Funding source", "Status"}, fundingRows)
	}

	d.data(n, obj(spec["data"]))
	d.risks(n, spec, list(spec["risks"]))

	// What the success criteria rest on, named once in the register.
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

	// Sign-off: the sponsor and lead approve it, and whoever runs it
	// afterwards accepts the handover (GovS 002 6.4.8).
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

	d.heldElsewhere()
	return d.end(), nil
}

// reading prints a key result's baseline or target as a value with its
// unit and the month it is from: "30 days, June 2028".
func reading(kr, point map[string]any) string {
	value := number(point["value"])
	if value == "" {
		value = str(point["value"])
	}
	if value == "" {
		return ""
	}
	switch str(kr["kind"]) {
	case "percent":
		value += "%"
	default:
		if unit := str(kr["unit"]); unit != "" {
			value += " " + unit
		}
	}
	if when := month(str(point["date"])); when != "" {
		value += ", " + when
	}
	return value
}

// month prints "2028-06" as "June 2028", and anything else as written.
func month(s string) string {
	if t, err := time.Parse("2006-01", s); err == nil {
		return t.Format("January 2006")
	}
	return s
}
