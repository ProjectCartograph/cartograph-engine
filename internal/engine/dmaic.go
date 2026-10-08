package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/document"
)

// Whether a project can be taken through DMAIC, Lean Six Sigma's Define,
// Measure, Analyze, Improve and Control (TAXONOMY.md D58): the
// deliverables each phase's tollgate asks for, read from what the project
// and the records it names already hold. Nothing here is asked of a
// project that is not being improved this way; it says what is there, and
// what a practitioner would still need.

// DMAIC phases, in order.
const (
	PhaseDefine  = "define"
	PhaseMeasure = "measure"
	PhaseAnalyze = "analyze"
	PhaseImprove = "improve"
	PhaseControl = "control"
)

// DMAICItem is one deliverable of a phase.
type DMAICItem struct {
	Phase string `json:"phase"`
	Key   string `json:"key"`
	Met   bool   `json:"met"`
	// What the deliverable is, and where in Cartograph it is held.
	Says  string `json:"says"`
	Field string `json:"field"`
	// Lacking names what is missing, when it is not met.
	Lacking string `json:"lacking,omitempty"`
}

// DMAICPhase is a phase's tollgate: its items and how many are met.
type DMAICPhase struct {
	Phase string      `json:"phase"`
	Met   int         `json:"met"`
	Of    int         `json:"of"`
	Items []DMAICItem `json:"items"`
}

// DMAIC is a project's readiness for each phase.
type DMAIC struct {
	Project string       `json:"project"`
	Phases  []DMAICPhase `json:"phases"`
}

// DMAICOf reads a project's readiness for DMAIC, as ctx reads the
// workspace.
func (e *Engine) DMAICOf(ctx context.Context, id string) (DMAIC, error) {
	docs := map[string]map[string]map[string]any{}
	for _, k := range []string{"Project", "KPI", "KPIReadings", "Operation", "DataSource"} {
		in, err := e.allInPlay(ctx, k)
		if err != nil {
			return DMAIC{}, err
		}
		docs[k] = map[string]map[string]any{}
		for _, d := range in {
			docs[k][d.id] = d.doc
		}
	}
	doc, ok := docs["Project"][id]
	if !ok {
		return DMAIC{}, fmt.Errorf("%w: Project/%s", ErrNotFound, id)
	}
	sp := specOf(doc)
	summary, _ := sp["summary"].(map[string]any)
	problems := listOf(summary["problems"])
	objectives := listOf(sp["objectives"])

	// The measures the project moves: the KPIs it names.
	var kpis []string
	for _, k := range listOf(sp["kpis"]) {
		if id, _ := k["kpi"].(string); id != "" {
			kpis = append(kpis, id)
		}
	}
	readings := map[string]int{}
	for _, d := range docs["KPIReadings"] {
		rs := specOf(d)
		k, _ := rs["kpi"].(string)
		readings[k] += len(listOf(rs["readings"]))
	}
	everyKPI := func(test func(spec map[string]any) bool) (bool, []string) {
		var lack []string
		for _, k := range kpis {
			kd, ok := docs["KPI"][k]
			if !ok || !test(specOf(kd)) {
				lack = append(lack, k)
			}
		}
		return len(kpis) > 0 && len(lack) == 0, lack
	}
	var out DMAIC
	out.Project = id
	add := func(phase, key string, met bool, says, field, lacking string) {
		i := map[string]int{PhaseDefine: 0, PhaseMeasure: 1, PhaseAnalyze: 2, PhaseImprove: 3, PhaseControl: 4}[phase]
		for len(out.Phases) <= i {
			out.Phases = append(out.Phases, DMAICPhase{Phase: []string{PhaseDefine, PhaseMeasure, PhaseAnalyze, PhaseImprove, PhaseControl}[len(out.Phases)]})
		}
		item := DMAICItem{Phase: phase, Key: key, Met: met, Says: says, Field: field}
		if !met {
			item.Lacking = lacking
		}
		p := &out.Phases[i]
		p.Items = append(p.Items, item)
		p.Of++
		if met {
			p.Met++
		}
	}
	text := func(m map[string]any, keys ...string) bool {
		for _, k := range keys {
			next, ok := m[k].(map[string]any)
			if ok {
				m = next
				continue
			}
			s, _ := m[k].(string)
			return strings.TrimSpace(s) != ""
		}
		return false
	}
	roles := map[string]bool{}
	for _, r := range listOf(sp["resources"]) {
		if role, _ := r["role"].(string); role != "" {
			roles[role] = true
		}
	}
	all := func(items []map[string]any, test func(map[string]any) bool) bool {
		if len(items) == 0 {
			return false
		}
		for _, it := range items {
			if !test(it) {
				return false
			}
		}
		return true
	}

	// Define: the charter.
	add(PhaseDefine, "problem", all(problems, func(p map[string]any) bool { return text(p, "problem", "situation") }),
		"A problem statement: what is going wrong, for whom.", "/spec/summary/problems", "No problem stated.")
	add(PhaseDefine, "goal", len(objectives) == 1 && text(objectives[0], "objective"),
		"One goal statement: the objective.", "/spec/objectives", "No objective.")
	add(PhaseDefine, "customers", len(listOf(summary["beneficiaries"])) > 0 || len(stringsOf(summary["beneficiaries"])) > 0,
		"The customers whose requirements set the measures: the groups it serves.", "/spec/summary/beneficiaries", "No beneficiary group.")
	add(PhaseDefine, "scope", len(stringsOf(summary["scopeIn"])) > 0 && len(stringsOf(summary["scopeOut"])) > 0,
		"What is in scope and what is out.", "/spec/summary/scopeIn", "Scope in or out is not written.")
	add(PhaseDefine, "team", roles["sponsor"] && (roles["manager"] || roles["technicalLead"]),
		"A sponsor and someone leading the work.", "/spec/resources", "No sponsor, or no one leading.")
	add(PhaseDefine, "business-case", len(listOf(sp["mandate"])) > 0 || len(stringsOf(mapOf(sp["alignment"])["goals"])) > 0,
		"Why it matters: its mandate, or the goals it serves.", "/spec/mandate", "No mandate and no goal it serves.")
	add(PhaseDefine, "schedule", len(listOf(sp["milestones"])) > 0,
		"Its milestones.", "/spec/milestones", "No milestones.")

	// Measure: what is measured, exactly, and where it stands now.
	add(PhaseMeasure, "measures", len(kpis) > 0,
		"The measures it moves: its KPIs.", "/spec/kpis", "It names no KPI.")
	ok1, lack := everyKPI(func(k map[string]any) bool { return text(k, "definition") && mapOf(k["metric"])["type"] != nil })
	add(PhaseMeasure, "operational-definition", ok1,
		"An operational definition of each: the words, and how it is computed (its metric).", "KPI /spec/metric", "Without a metric: "+strings.Join(lack, ", "))
	ok2, lack := everyKPI(func(k map[string]any) bool {
		return len(stringsOf(k["sources"])) > 0 && text(k, "cycle") && k["owner"] != nil
	})
	add(PhaseMeasure, "data-collection", ok2,
		"A data collection plan for each: its sources, its cycle and its owner.", "KPI /spec/sources", "Missing a source, cycle or owner: "+strings.Join(lack, ", "))
	ok3, lack := everyKPI(func(k map[string]any) bool { _, has := document.Number(mapOf(k["baseline"])["value"]); return has })
	add(PhaseMeasure, "baseline", ok3,
		"A baseline for each: today's figure, dated.", "KPI /spec/baseline", "No baseline figure: "+strings.Join(lack, ", "))
	ok4, lack := everyKPI(func(k map[string]any) bool { return k["specLimits"] != nil })
	add(PhaseMeasure, "ctq-limits", ok4,
		"The requirement each must meet: its specification limits, critical to quality.", "KPI /spec/specLimits", "No specification limits: "+strings.Join(lack, ", "))
	var unchecked []string
	for _, k := range kpis {
		for _, s := range stringsOf(specOf(docs["KPI"][k])["sources"]) {
			ds, ok := docs["DataSource"][s]
			if !ok || !(text(specOf(ds), "capture") || text(specOf(ds), "qualityIssues")) {
				unchecked = append(unchecked, s)
			}
		}
	}
	add(PhaseMeasure, "measurement-system", len(kpis) > 0 && len(unchecked) == 0,
		"How each source's figures are captured, and what is known to be wrong with them: the measurement system.", "DataSource /spec/capture", "Capture and known issues not described: "+strings.Join(unchecked, ", "))

	// Analyze: the causes, with evidence.
	causes := func(p map[string]any) []map[string]any { return listOf(mapOf(p["problem"])["causes"]) }
	add(PhaseAnalyze, "causes", all(problems, func(p map[string]any) bool { return len(causes(p)) > 0 }),
		"The causes found behind each problem.", "/spec/summary/problems/-/problem/causes", "A problem with no cause found.")
	add(PhaseAnalyze, "evidence", all(problems, func(p map[string]any) bool {
		return all(causes(p), func(c map[string]any) bool { return text(c, "evidence") })
	}), "The evidence for each cause.", "/spec/summary/problems/-/problem/causes/-/evidence", "A cause without evidence.")
	add(PhaseAnalyze, "root-cause", all(problems, func(p map[string]any) bool {
		for _, c := range causes(p) {
			if v, _ := c["verified"].(bool); v {
				return true
			}
		}
		return false
	}), "A root cause verified for each problem.", "/spec/summary/problems/-/problem/causes/-/verified", "A problem with no verified root cause.")
	add(PhaseAnalyze, "gap", all(problems, func(p map[string]any) bool { return len(listOf(p["gaps"])) > 0 || len(stringsOf(p["gaps"])) > 0 }),
		"The gap each problem is, measured: where things are and where they should be.", "/spec/summary/problems/-/gaps", "A problem tied to no gap.")

	// Improve: the change, and what could go wrong with it.
	add(PhaseImprove, "solution", all(problems, func(p map[string]any) bool { return text(p, "change", "what") }),
		"The change each problem needs.", "/spec/summary/problems/-/change/what", "A problem with no change stated.")
	add(PhaseImprove, "deliverables", len(listOf(sp["deliverables"])) > 0,
		"What the work hands over.", "/spec/deliverables", "No deliverables.")
	risks := listOf(sp["risks"])
	add(PhaseImprove, "fmea", all(risks, func(r map[string]any) bool {
		return text(r, "impact") && text(r, "likelihood") && text(r, "detection")
	}), "Each risk rated for impact, likelihood and detection: a failure mode and effects analysis.", "/spec/risks", "Risks not rated for impact, likelihood and detection.")

	// Control: holding the gain.
	landing, _ := sp["operation"].(string)
	op, hasOp := docs["Operation"][landing]
	add(PhaseControl, "process-owner", hasOp && specOf(op)["serviceOwner"] != nil,
		"The process owner: the service the result lands in, with its owner.", "/spec/operation", "No landing operation with a service owner.")
	ok5, lack := everyKPI(func(k map[string]any) bool { return k["response"] != nil })
	add(PhaseControl, "response-plan", ok5,
		"A response plan for each measure: what is done when a reading signals trouble, and by whom.", "KPI /spec/response", "No response plan: "+strings.Join(lack, ", "))
	var thin []string
	for _, k := range kpis {
		if readings[k] < 2 {
			thin = append(thin, k)
		}
	}
	add(PhaseControl, "control-chart", len(kpis) > 0 && len(thin) == 0,
		"Readings enough to chart each measure against its control limits.", "KPIReadings", "Fewer than two readings: "+strings.Join(thin, ", "))
	add(PhaseControl, "success-confirmed", all(listOf(sp["successCriteria"]), func(c map[string]any) bool { return c["confirmedBy"] != nil }),
		"Success criteria, each with who confirms it.", "/spec/successCriteria", "Success criteria not stated, or not confirmed by anyone.")
	return out, nil
}

func mapOf(v any) map[string]any {
	m, _ := v.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	return m
}
