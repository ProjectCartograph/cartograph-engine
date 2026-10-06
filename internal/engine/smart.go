package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/kinds/kit"
)

// Smart is how far one goal, objective or outcome meets the SMART criteria
// (Doran, "There's a S.M.A.R.T. way to write management's goals and
// objectives", Management Review, 1981; the current reading, Specific,
// Measurable, Attainable, Relevant, Time-bound). TAXONOMY.md D25, D26.
//
// Each letter is read from what the record already holds, never typed:
//   - Specific: a statement, with the numbers left to the measures.
//   - Measurable: at least one measure, a key result of its own or an
//     indicator (KPI) aligned to it, with a target.
//   - Attainable: the target can reasonably be reached: every measure has
//     today's figure (or an admitted unknown), dated before its target, and
//     the target moves the way the measure says it should. Cartograph cannot
//     judge ambition; it catches a target that cannot be reached as written.
//   - Relevant: it sits under the right level (a goal under the vault's
//     vision and mission) and says why it matters.
//   - Time-bound: every target has an end date, inside the aim's horizon
//     when it has one (its own, or its parent's).
type Smart struct {
	Specific   bool `json:"specific"`
	Measurable bool `json:"measurable"`
	Attainable bool `json:"attainable"`
	Relevant   bool `json:"relevant"`
	TimeBound  bool `json:"timeBound"`
}

// measure is one key result or aligned indicator, reduced to what SMART
// asks of it.
type measure struct {
	baseline, target, dated bool
	direction               string
	baseValue, targetValue  float64
	hasBase, hasTarget      bool
	baseDate, targetDate    string
}

func measureOf(m map[string]any) measure {
	var out measure
	out.direction, _ = m["direction"].(string)
	if b, ok := m["baseline"].(map[string]any); ok {
		_, v := b["value"]
		_, u := b["unknownReason"]
		out.baseline = v || u
		out.baseValue, out.hasBase = toFloat(b["value"])
		out.baseDate = monthOf(b["date"], false)
	}
	if t, ok := m["target"].(map[string]any); ok {
		_, out.target = t["value"]
		out.targetValue, out.hasTarget = toFloat(t["value"])
		out.targetDate = monthOf(t["date"], true)
		out.dated = out.targetDate != ""
	}
	return out
}

// wrongWay reports a target that moves against the measure's direction.
func (m measure) wrongWay() bool {
	if !m.hasBase || !m.hasTarget {
		return false
	}
	switch m.direction {
	case "increase":
		return m.targetValue <= m.baseValue
	case "decrease":
		return m.targetValue >= m.baseValue
	}
	return false
}

// outOfOrder reports a target dated on or before today's figure.
func (m measure) outOfOrder() bool {
	return m.baseDate != "" && m.targetDate != "" && m.targetDate <= m.baseDate
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}

// monthOf reads a date as "YYYY-MM". A bare year reads as its January, or
// its December when it closes a period (an end or a target).
func monthOf(v any, end bool) string {
	s := strings.TrimSpace(fmt.Sprint(v))
	if v == nil || s == "" {
		return ""
	}
	if len(s) >= 7 && s[4] == '-' {
		return s[:7]
	}
	if len(s) == 4 {
		if end {
			return s + "-12"
		}
		return s + "-01"
	}
	return ""
}

// Horizon is the period an aim covers, as "YYYY-MM" at each end.
type Horizon struct {
	Start string `json:"start"`
	End   string `json:"end"`
	// Inherited is true when the aim has none of its own and takes its
	// parent's.
	Inherited bool `json:"inherited,omitempty"`
}

func horizonOf(spec map[string]any) *Horizon {
	h, ok := spec["horizon"].(map[string]any)
	if !ok {
		return nil
	}
	s, e := monthOf(h["start"], false), monthOf(h["end"], true)
	if s == "" || e == "" {
		return nil
	}
	return &Horizon{Start: s, End: e}
}

// goalReader is where the goal code reads the goals around the one it is
// judging: from the store, one at a time, for a single goal; or from
// documents already read in bulk, for the whole tree.
type goalReader struct {
	// spec returns a goal's spec, or false when there is no such goal.
	spec func(id string) (map[string]any, bool)
	// settings returns the vault's settings, read at most once.
	settings func() (Settings, error)
}

// storedGoals reads goals from the store as they are asked for.
func (e *Engine) storedGoals(ctx context.Context) goalReader {
	return goalReader{
		spec: func(id string) (map[string]any, bool) {
			doc, found, err := e.currentDoc(ctx, "Goal", id)
			if err != nil || !found {
				return nil, false
			}
			sp, _ := doc["spec"].(map[string]any)
			return sp, sp != nil
		},
		settings: e.onceSettings(ctx),
	}
}

// loadedGoals answers from goals already read, by id.
func (e *Engine) loadedGoals(ctx context.Context, goals map[string]map[string]any) goalReader {
	return goalReader{
		spec: func(id string) (map[string]any, bool) {
			sp, _ := goals[id]["spec"].(map[string]any)
			return sp, sp != nil
		},
		settings: e.onceSettings(ctx),
	}
}

// onceSettings reads the settings on first use and keeps the answer.
func (e *Engine) onceSettings(ctx context.Context) func() (Settings, error) {
	var (
		read bool
		s    Settings
		err  error
	)
	return func() (Settings, error) {
		if !read {
			s, err = e.GetSettings(ctx)
			read = true
		}
		return s, err
	}
}

// effectiveHorizon is the aim's own horizon, or the nearest ancestor's.
func (e *Engine) effectiveHorizon(read goalReader, spec map[string]any) *Horizon {
	if h := horizonOf(spec); h != nil {
		return h
	}
	parent, _ := spec["parent"].(string)
	for i := 0; parent != "" && i < 4; i++ {
		ps, found := read.spec(parent)
		if !found {
			return nil
		}
		if h := horizonOf(ps); h != nil {
			h.Inherited = true
			return h
		}
		parent, _ = ps["parent"].(string)
	}
	return nil
}

// span prints a horizon the way people say it: 2025 to 2030, or with
// months when either end has one.
func span(h *Horizon) string {
	if h == nil {
		return ""
	}
	if strings.HasSuffix(h.Start, "-01") && strings.HasSuffix(h.End, "-12") {
		return h.Start[:4] + " to " + h.End[:4]
	}
	return h.Start + " to " + h.End
}

// goalSmart reads the five letters for one record and the check line for
// each. kpis are the specs of the indicators aligned to it.
func (e *Engine) goalSmart(read goalReader, spec map[string]any, kpis []map[string]any, horizon *Horizon) (Smart, []GoalCheck) {
	var s Smart
	var checks []GoalCheck
	add := func(id string, ok bool, section, okMsg, warnMsg string) {
		state, msg := goalCheckOK, okMsg
		if !ok {
			state, msg = goalCheckWarn, warnMsg
		}
		checks = append(checks, GoalCheck{ID: id, State: state, Message: msg, Fix: &GoalCheckFix{Section: section}})
	}

	statement, _ := spec["objective"].(string)
	specificMsg := "Specific: say what will change."
	if strings.TrimSpace(statement) != "" {
		if p := kit.ObjectiveDigitProblem(statement, "/spec/objective"); p != nil {
			specificMsg = "Specific: " + p.Message
		} else {
			s.Specific = true
		}
	}
	add("smart-specific", s.Specific, "objective", "Specific: says what will change.", specificMsg)

	var ms []measure
	krs, _ := spec["keyResults"].([]any)
	for _, kr := range krs {
		if m, ok := kr.(map[string]any); ok {
			ms = append(ms, measureOf(m))
		}
	}
	for _, k := range kpis {
		ms = append(ms, measureOf(k))
	}
	targeted, based, dated, wrong, order, outside := 0, 0, 0, 0, 0, 0
	for _, m := range ms {
		if m.target {
			targeted++
		}
		if m.baseline {
			based++
		}
		if m.dated {
			dated++
			if horizon != nil && (m.targetDate > horizon.End || m.targetDate < horizon.Start) {
				outside++
			}
		}
		if m.wrongWay() {
			wrong++
		}
		if m.outOfOrder() {
			order++
		}
	}
	s.Measurable = targeted > 0
	s.Attainable = len(ms) > 0 && based == len(ms) && wrong == 0 && order == 0
	s.TimeBound = targeted > 0 && dated == len(ms) && outside == 0
	add("smart-measurable", s.Measurable, "keyResults",
		fmt.Sprintf("Measurable: %d measure%s with a target.", targeted, plural(targeted)),
		"Measurable: add a measure with a target to reach.")
	attainWarn := "Attainable: record today's figure, so you can tell whether the target can be reached."
	switch {
	case len(ms) == 0:
	case based < len(ms):
		attainWarn = fmt.Sprintf("Attainable: today's figure is missing for %d of %d measures.", len(ms)-based, len(ms))
	case wrong > 0:
		attainWarn = fmt.Sprintf("Attainable: %d target%s move%s against the measure's direction.", wrong, plural(wrong), map[bool]string{true: "s", false: ""}[wrong == 1])
	case order > 0:
		attainWarn = fmt.Sprintf("Attainable: %d target%s %s dated before today's figure.", order, plural(order), map[bool]string{true: "is", false: "are"}[order == 1])
	}
	add("smart-attainable", s.Attainable, "keyResults",
		"Attainable: each target starts from today's figure and moves the right way, in time.",
		attainWarn)
	timeWarn := "Time-bound: give the target an end date to be reached by."
	switch {
	case targeted == 0:
	case dated < len(ms):
		timeWarn = fmt.Sprintf("Time-bound: %d of %d targets have no end date.", len(ms)-dated, len(ms))
	case outside > 0:
		timeWarn = fmt.Sprintf("Time-bound: %d target%s fall%s outside the horizon, %s.", outside, plural(outside), map[bool]string{true: "s", false: ""}[outside == 1], span(horizon))
	}
	timeOK := "Time-bound: every target has an end date."
	if horizon != nil {
		timeOK = "Time-bound: every target has an end date inside the horizon, " + span(horizon) + "."
	}
	add("smart-time-bound", s.TimeBound, "keyResults", timeOK, timeWarn)

	level, _ := spec["level"].(string)
	parentID, _ := spec["parent"].(string)
	why, _ := spec["whyItMatters"].(string)
	var placed bool
	var relevantWarn string
	switch level {
	case "goal":
		settings, err := read.settings()
		placed = err == nil && settings.Purpose != nil &&
			(strings.TrimSpace(settings.Purpose.Vision) != "" || strings.TrimSpace(settings.Purpose.Mission) != "")
		relevantWarn = "Relevant: no vision or mission to judge it against."
	default:
		want := map[string]string{"objective": "goal", "outcome": "objective"}[level]
		if parentID != "" {
			if ps, found := read.spec(parentID); found {
				level, _ := ps["level"].(string)
				placed = level == want
			}
		}
		relevantWarn = fmt.Sprintf("Relevant: no %s above it yet.", want)
	}
	s.Relevant = placed && strings.TrimSpace(why) != ""
	if placed && !s.Relevant {
		relevantWarn = "Relevant: say why it matters."
	}
	section := "parent"
	if placed {
		section = "whyItMatters"
	}
	add("smart-relevant", s.Relevant, section, "Relevant: serves the aim above it, and says why.", relevantWarn)
	return s, checks
}

// alignedKPISpecs returns the spec of every indicator aligned to a goal.
func (e *Engine) alignedKPISpecs(ctx context.Context, id string) ([]map[string]any, error) {
	refs, err := e.referencing(ctx, "Goal", id)
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	for _, r := range refs {
		if r.Kind != "KPI" {
			continue
		}
		doc, found, err := e.currentDoc(ctx, "KPI", r.ID)
		if err != nil || !found {
			continue
		}
		if sp, ok := doc["spec"].(map[string]any); ok {
			out = append(out, sp)
		}
	}
	return out, nil
}

// serves reports whether a goal is aim or sits beneath it in the tree: a
// project aligned to an outcome serves a programme judged on the
// objective above it (TAXONOMY.md D24, D25: aims at every level).
func (r goalReader) serves(goal, aim string) bool {
	seen := map[string]bool{}
	for id := goal; id != "" && !seen[id]; {
		if id == aim {
			return true
		}
		seen[id] = true
		spec, ok := r.spec(id)
		if !ok {
			return false
		}
		id, _ = spec["parent"].(string)
	}
	return false
}

// servesAny reports whether any of goals serves any of aims.
func (r goalReader) servesAny(goals []string, aims []string) bool {
	for _, g := range goals {
		for _, a := range aims {
			if r.serves(g, a) {
				return true
			}
		}
	}
	return false
}
