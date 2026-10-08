package engine

import (
	"context"
	"fmt"
	"strings"
)

// kpiChecksOf checks an indicator (TAXONOMY.md D25, D26): a measure is
// only usable with a baseline (or an admitted unknown), a dated target,
// somewhere it is read from and how often, and the aims it measures. The
// schema asks for its definition, unit, direction and source; these are
// what a person still has to settle, each in the flow step that asks it.
func (e *Engine) kpiChecksOf(_ context.Context, _ string, doc map[string]any) ([]ProgrammeCheck, error) {
	spec, _ := doc["spec"].(map[string]any)
	if spec == nil {
		spec = map[string]any{}
	}
	var out []ProgrammeCheck
	add := func(id, section string, met bool, ok, warn string) {
		state, message := programmeCheckOK, ok
		if !met {
			state, message = programmeCheckWarn, warn
		}
		out = append(out, ProgrammeCheck{ID: id, Section: section, State: state, Message: message})
	}
	text := func(m map[string]any, k string) bool {
		s, _ := m[k].(string)
		return strings.TrimSpace(s) != ""
	}
	has := func(m map[string]any, k string) bool {
		v, ok := m[k]
		return ok && v != nil
	}

	baseline, _ := spec["baseline"].(map[string]any)
	switch {
	case baseline != nil && has(baseline, "value") && text(baseline, "date"):
		add("kpi-baseline", "baseline-target", true, "Today's figure is recorded, with its date.", "")
	case baseline != nil && text(baseline, "unknownReason"):
		add("kpi-baseline", "baseline-target", true, "Today's figure is admitted unknown, with the reason.", "")
	default:
		add("kpi-baseline", "baseline-target", false, "", "No baseline yet: today's figure and its date, or why it is not known.")
	}
	switch t := readTarget(spec["target"]); {
	case t.Pending && t.Set && !t.Timing.Late:
		add("kpi-target", "baseline-target", true, "The target "+pendingWords(t)+".", "")
	case t.Pending && t.Timing.Late:
		add("kpi-target", "baseline-target", false, "", "The target "+pendingWords(t)+".")
	default:
		add("kpi-target", "baseline-target", t.Set && t.Dated,
			"A target is set, with when it is to be reached.", "No dated target yet: the figure to reach and by when, or the event that sets it.")
	}
	add("kpi-cycle", "verification", text(spec, "cycle"), "Read on a reporting cycle.", "No reporting cycle yet: how often it is read.")
	goals, _ := spec["goals"].([]any)
	add("kpi-aligned", "result", len(goals) > 0, "Measures at least one aim.", "Measures no aim yet: name the goal, objective or outcome it tells you about.")
	if mt, _ := spec["metric"].(map[string]any); mt != nil {
		problem := metricProblem(mt, stringsOf(spec["sources"]))
		add("kpi-metric", "metric", problem == "", "Computed as a dbt metric from its sources.", problem)
	}
	if pc, ok := pendingCheck(doc); ok {
		out = append(out, pc)
	}
	return out, nil
}

// metricProblem says what keeps a KPI's metric from being built in the
// semantic layer (TAXONOMY.md D57): the parts its type needs, and each
// measure over one of the KPI's own sources, so the number and where it
// is checked agree. Empty when nothing does.
func metricProblem(mt map[string]any, sources []string) string {
	own := map[string]bool{}
	for _, s := range sources {
		own[s] = true
	}
	need := map[string][]string{"simple": {"measure"}, "cumulative": {"measure"}, "ratio": {"numerator", "denominator"}}
	t, _ := mt["type"].(string)
	if t == "derived" {
		if expr, _ := mt["expr"].(string); strings.TrimSpace(expr) == "" || len(stringsOf(mt["uses"])) == 0 {
			return "A derived metric needs the KPIs it uses and the expression over them."
		}
		return ""
	}
	for _, part := range need[t] {
		ms, _ := mt[part].(map[string]any)
		if ms == nil {
			return fmt.Sprintf("A %s metric needs its %s: the source it is counted in and how.", t, part)
		}
		if src, _ := ms["source"].(string); !own[src] {
			return fmt.Sprintf("Its %s is counted in %q, which is not one of this KPI's sources: add it to the sources, or count it in one of them.", part, src)
		}
	}
	return ""
}
