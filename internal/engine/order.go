package engine

import (
	"context"
	"fmt"
	"strings"
)

// The order of work (TAXONOMY.md D28). The record is a directed acyclic
// graph: a manifest names only what comes before it, so whatever it names
// exists by the time it is written, and nothing finished has to be opened
// again to be linked to what came after. The order says what comes before
// what. A strategy is written from the top down (purpose, goals,
// objectives, outcomes, the indicators that measure them, the gaps they
// close), and the work that delivers it comes after (programmes,
// operations, projects). The registers (teams, roles, units, cycles,
// segments, data and funding sources, beneficiary groups) are the roots:
// anything may name them and they name nothing in the strategy, so they
// are added when a field first asks for one.
//
// Every interface and agent reads the order from here, so a person in
// the editor and an agent over MCP are led the same way, and the contract
// is held to it: a test fails a schema reference that points downstream,
// and a version that names something later in the order, or closes a
// loop, is refused.

// Stage is one step of the order: a kind, or for a Goal one level of it.
type Stage struct {
	Key   string `json:"key"`
	Kind  string `json:"kind"`
	Level string `json:"level,omitempty"`
	// After are the stages that need a record before this one can be
	// written well: what it names.
	After []string `json:"after,omitempty"`
	// Optional is a stage a workspace may leave empty: not every plan
	// has a programme, an operation, assumptions or a stakeholder map.
	Optional bool `json:"optional,omitempty"`
}

// registers are the root kinds, in the order they may name each other: a
// data source names its team, a funding source the role that holds it, a
// beneficiary group the source that counts it.
var registers = []string{
	"Team", "Resource", "Unit", "ReportingCycle", "Segment", "DataSource", "FundingSource", "BeneficiaryGroup",
}

// stages are the order of work.
var stages = []Stage{
	{Key: "purpose", Kind: "Purpose"},
	{Key: "goal", Kind: "Goal", Level: "goal", After: []string{"purpose"}},
	{Key: "objective", Kind: "Goal", Level: "objective", After: []string{"goal"}},
	{Key: "outcome", Kind: "Goal", Level: "outcome", After: []string{"objective"}},
	{Key: "kpi", Kind: "KPI", After: []string{"outcome"}},
	{Key: "gap", Kind: "Gap", After: []string{"outcome", "kpi"}},
	{Key: "assumption", Kind: "Assumption", After: []string{"kpi"}, Optional: true},
	{Key: "programme", Kind: "Programme", After: []string{"gap"}, Optional: true},
	// A service names nothing in the strategy it must wait for: one already
	// running is recorded as it stands, and a planned one before the
	// project that sets it up (TAXONOMY.md D30).
	{Key: "operation", Kind: "Operation", Optional: true},
	{Key: "project", Kind: "Project", After: []string{"outcome"}},
	{Key: "stakeholders", Kind: "StakeholderMap", After: []string{"project"}, Optional: true},
}

// afterStages are kinds that hold a record's data rather than a step of
// the work: a KPI's readings come after the KPI.
var afterStages = []string{"KPIReadings"}

// goalLevels are a Goal's levels, in order.
var goalLevels = []string{"goal", "objective", "outcome"}

// Stages returns the order of work.
func Stages() []Stage {
	out := make([]Stage, len(stages))
	copy(out, stages)
	return out
}

// Registers returns the root kinds, in order.
func Registers() []string { return append([]string(nil), registers...) }

// rank is a kind's place in the order, for a Goal at a level: registers
// first, then the stages, then what holds a stage's data. A Goal with no
// level known ranks as a goal. Every kind not named ranks last, so a new
// kind must be placed before anything may name it.
func rank(kind, level string) int {
	for i, k := range registers {
		if k == kind {
			return i
		}
	}
	if kind == "Goal" && level == "" {
		level = "goal"
	}
	for i, s := range stages {
		if s.Kind == kind && (s.Level == "" || s.Level == level) {
			return len(registers) + i
		}
	}
	for i, k := range afterStages {
		if k == kind {
			return len(registers) + len(stages) + i
		}
	}
	return len(registers) + len(stages) + len(afterStages)
}

// orderedKinds is every kind in the order, each once, a Goal at its first
// level.
func orderedKinds() []string {
	out := append([]string(nil), registers...)
	seen := map[string]bool{}
	for _, k := range out {
		seen[k] = true
	}
	for _, s := range stages {
		if !seen[s.Kind] {
			seen[s.Kind] = true
			out = append(out, s.Kind)
		}
	}
	return append(out, afterStages...)
}

// stageOf is the stage a kind at a level is written in, if it has one.
func stageOf(kind, level string) (Stage, bool) {
	if kind == "Goal" && level == "" {
		level = "goal"
	}
	for _, s := range stages {
		if s.Kind == kind && (s.Level == "" || s.Level == level) {
			return s, true
		}
	}
	return Stage{}, false
}

// names reports whether a manifest of kind may name one of target: the
// target comes before it in the order, or is of its own kind, which a
// tree allows (a team's parent, a goal's parent at the level above, a
// project's parent) and the loop check holds to.
func names(kind, target string) bool {
	return kind == target || rank(target, "") < rank(kind, "")
}

// orderProblems refuses a reference that points later in the order, and
// one that closes a loop through manifests of its own kind. Both are
// read from the references the schema declares, so no kind is written
// out here. Only a reference being added is refused: one the manifest's
// current version already holds was valid when it was saved, and a
// deployed record is not broken by a later minor release (VERSIONING.md).
func (e *Engine) orderProblems(kind, id string, doc map[string]any, l *lookup) []Problem {
	had := map[string]bool{}
	if v, ok, err := l.store.GetCurrent(l.ctx, kind, id); err == nil && ok {
		var d map[string]any
		if l.codec.DecodeInto(v.YAML, &d) == nil {
			for _, f := range extractRefs(d, e.refRules[kind]) {
				had[f.kind+"/"+f.id] = true
			}
		}
	}
	var out []Problem
	same := false
	for _, f := range extractRefs(doc, e.refRules[kind]) {
		if had[f.kind+"/"+f.id] {
			continue
		}
		if f.kind == kind {
			same = true
			if f.id == id {
				out = append(out, Problem{Path: f.path, Message: fmt.Sprintf("a %s cannot name itself", kind)})
			}
			continue
		}
		if !names(kind, f.kind) {
			out = append(out, Problem{Path: f.path, Message: fmt.Sprintf(
				"a %s names only what comes before it in the order of work, and %s comes after it: name this %s from the %s instead",
				kind, f.kind, kind, f.kind)})
		}
	}
	if !same || id == "" {
		return out
	}
	// A loop through its own kind: walk what the others of this kind name
	// of it, from what this one names, and see whether it comes back.
	others, err := l.Documents(kind)
	if err != nil {
		return out
	}
	next := func(at string) []string {
		var ids []string
		d := others[at]
		if at == id {
			d = doc
		}
		for _, f := range extractRefs(d, e.refRules[kind]) {
			if f.kind == kind {
				ids = append(ids, f.id)
			}
		}
		return ids
	}
	for _, f := range extractRefs(doc, e.refRules[kind]) {
		if f.kind != kind || f.id == id || had[f.kind+"/"+f.id] {
			continue
		}
		seen := map[string]bool{}
		path := []string{id, f.id}
		var reaches func(at string) bool
		reaches = func(at string) bool {
			if at == id {
				return true
			}
			if seen[at] {
				return false
			}
			seen[at] = true
			for _, n := range next(at) {
				path = append(path, n)
				if reaches(n) {
					return true
				}
				path = path[:len(path)-1]
			}
			return false
		}
		if reaches(f.id) {
			out = append(out, Problem{Path: f.path, Message: fmt.Sprintf("this would make a loop: %s", strings.Join(path, " → "))})
		}
	}
	return out
}

// StageState is one stage of the order as a workspace stands.
type StageState struct {
	Stage
	// Count is how many records it has.
	Count int `json:"count"`
	// State is done (it has a record), next (the one to write now), ready
	// (what it names is there) or waiting (what it names is not).
	State string `json:"state"`
	// Waiting are the stages it waits on, while it waits.
	Waiting []string `json:"waiting,omitempty"`
}

// Order is the order of work as a workspace stands: each stage, how far
// it has got, and which to write next.
type Order struct {
	Stages []StageState `json:"stages"`
	// Next is the key of the stage to write now; empty once every stage a
	// plan needs has a record.
	Next string `json:"next,omitempty"`
	// Registers are the root kinds, each with its count.
	Registers []KindInfo `json:"registers"`
}

// WorkspaceOrder says, for the workspace as it stands, how far each stage
// has got and which comes next: the first stage a plan needs that has no
// record and is not waiting on one that has none. An empty workspace
// starts at its purpose.
func (e *Engine) WorkspaceOrder(ctx context.Context) (Order, error) {
	counts, err := e.manifests.Counts(ctx)
	if err != nil {
		return Order{}, err
	}
	levels := map[string]int{}
	if counts["Goal"] > 0 {
		tree, err := e.GoalTree(ctx)
		if err != nil {
			return Order{}, err
		}
		var walk func([]*GoalNode)
		walk = func(ns []*GoalNode) {
			for _, n := range ns {
				levels[n.Level]++
				walk(n.Children)
			}
		}
		walk(tree.Nodes)
	}
	// The purpose may still live in the settings (ADR 0020).
	if counts["Purpose"] == 0 {
		if s, err := e.GetSettings(ctx); err == nil && s.Purpose != nil && (strings.TrimSpace(s.Purpose.Vision) != "" || strings.TrimSpace(s.Purpose.Mission) != "") {
			counts["Purpose"] = 1
		}
	}
	out := Order{Stages: make([]StageState, len(stages)), Registers: []KindInfo{}}
	have := map[string]bool{}
	for i, s := range stages {
		n := counts[s.Kind]
		if s.Level != "" {
			n = levels[s.Level]
		}
		out.Stages[i] = StageState{Stage: s, Count: n}
		have[s.Key] = n > 0
	}
	for i := range out.Stages {
		st := &out.Stages[i]
		for _, a := range st.After {
			if !have[a] {
				st.Waiting = append(st.Waiting, a)
			}
		}
		switch {
		case st.Count > 0:
			st.State = "done"
		case len(st.Waiting) > 0:
			st.State = "waiting"
		case out.Next == "" && !st.Optional:
			st.State = "next"
			out.Next = st.Key
		default:
			st.State = "ready"
		}
	}
	for _, k := range registers {
		out.Registers = append(out.Registers, KindInfo{Kind: k, Count: counts[k]})
	}
	return out, nil
}
