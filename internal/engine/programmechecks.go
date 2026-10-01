package engine

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
)

// What a programme is checked on, and why there is no block state here.
//
// A programme is a temporary structure coordinating projects and
// business-as-usual to deliver beneficial change (TAXONOMY.md). So the
// questions worth asking are whether it can be judged on that: does it
// coordinate anything, is there a change it is aiming at, and can the
// benefit be measured. Its required fields are not checked — the schema
// already refuses a programme without them, and repeating a refusal as an
// advisory line says nothing.
//
// Every check below reads manifests other than this one: the work that
// names this programme, the goals it aligns to, the dependency graph it
// sits in. That is exactly the case where a blocking check misfires,
// because a definition that saved yesterday would be refused today
// because somebody edited a different file (the reason programme
// membership was demoted on 2026-09-28). So none of these blocks, and the
// contract has no block state to hold one.

const (
	programmeCheckOK   = "ok"
	programmeCheckWarn = "warn"
)

// ProgrammeCheck is one advisory line about a programme, and the step of
// the walk a reader would go to about it.
type ProgrammeCheck struct {
	ID      string `json:"id"`
	Section string `json:"section"`
	State   string `json:"state"`
	Message string `json:"message"`
}

// ProgrammeChecks reads one programme and everything that points at it.
func (e *Engine) ProgrammeChecks(ctx context.Context, id string) ([]ProgrammeCheck, error) {
	v, found, err := e.manifests.GetCurrent(ctx, "Programme", id)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("%w: Programme/%s", ErrNotFound, id)
	}
	var doc map[string]any
	if err := e.codec.DecodeInto(v.YAML, &doc); err != nil {
		return nil, fmt.Errorf("parse Programme/%s: %w", id, err)
	}
	spec, _ := doc["spec"].(map[string]any)
	if spec == nil {
		spec = map[string]any{}
	}

	var out []ProgrammeCheck
	add := func(checkID, section, state, message string) {
		out = append(out, ProgrammeCheck{ID: checkID, Section: section, State: state, Message: message})
	}

	// Coordinating something is what makes it a programme rather than a
	// heading. What sits inside one is a component, which is PMI's own
	// word for it; the count is derived, because the work declares which
	// programme it belongs to and this reads that back, so a programme is
	// never edited to gain one.
	projects, operations, err := e.programmeMembers(ctx, id)
	if err != nil {
		return nil, err
	}
	switch {
	case len(projects) == 0 && len(operations) == 0:
		add("components-present", "components", programmeCheckWarn,
			"No project or operation has named this programme yet, so it coordinates nothing.")
	case len(operations) == 0:
		add("components-present", "components", programmeCheckOK,
			fmt.Sprintf("%d project%s named.", len(projects), plural(len(projects))))
	case len(projects) == 0:
		add("components-present", "components", programmeCheckOK,
			fmt.Sprintf("%d operation%s named.", len(operations), plural(len(operations))))
	default:
		add("components-present", "components", programmeCheckOK,
			fmt.Sprintf("%d project%s and %d operation%s named.",
				len(projects), plural(len(projects)), len(operations), plural(len(operations))))
	}

	// A programme exists to answer something wrong, and a problem that
	// names nobody is a statement about the system rather than about a
	// group of people — which is what a Gap is for.
	problems, _ := spec["problems"].([]any)
	if len(problems) == 0 {
		add("problems-stated", "problems", programmeCheckWarn, "No problem stated yet.")
	} else {
		add("problems-stated", "problems", programmeCheckOK,
			fmt.Sprintf("%d problem%s stated.", len(problems), plural(len(problems))))
		groupless, ungrounded := 0, 0
		for _, p := range problems {
			pm, ok := p.(map[string]any)
			if !ok {
				continue
			}
			if groups, _ := pm["groups"].([]any); len(groups) == 0 {
				groupless++
			}
			if gaps, _ := pm["gaps"].([]any); len(gaps) == 0 {
				ungrounded++
			}
		}
		if groupless == 0 {
			add("problems-groups", "problems", programmeCheckOK, "Every problem names who it affects.")
		} else if groupless == 1 {
			add("problems-groups", "problems", programmeCheckWarn, "1 problem does not say who it affects.")
		} else {
			add("problems-groups", "problems", programmeCheckWarn,
				fmt.Sprintf("%d problems do not say who they affect.", groupless))
		}
		if ungrounded == 0 {
			add("problems-gaps", "problems", programmeCheckOK, "Every problem points at the evidence for it.")
		} else if ungrounded == 1 {
			add("problems-gaps", "problems", programmeCheckWarn, "1 problem points at no gap as evidence.")
		} else {
			add("problems-gaps", "problems", programmeCheckWarn,
				fmt.Sprintf("%d problems point at no gap as evidence.", ungrounded))
		}
	}

	// Judged on benefits realised: a goal says what improvement, a KPI
	// says how it is watched. Without either there is nothing to judge.
	goals, _ := spec["goals"].([]any)
	if len(goals) == 0 {
		add("alignment-goals", "alignment", programmeCheckWarn, "Not linked to an outcome yet.")
	} else {
		add("alignment-goals", "alignment", programmeCheckOK,
			fmt.Sprintf("Aligned to %d goal%s.", len(goals), plural(len(goals))))
	}
	kpis, _ := spec["kpis"].([]any)
	if len(kpis) == 0 {
		add("alignment-kpis", "alignment", programmeCheckWarn, "No indicator named yet, so there is nothing to judge it by.")
	} else {
		add("alignment-kpis", "alignment", programmeCheckOK,
			fmt.Sprintf("%d measure%s named.", len(kpis), plural(len(kpis))))
	}

	out = append(out, programmeRiskChecks(spec)...)

	// A loop between programmes is the fact the Strategic Plan's own
	// linkages make likely and no single definition shows.
	loop, err := e.programmeCycleCheck(ctx, id)
	if err != nil {
		return nil, err
	}
	if loop != nil {
		out = append(out, *loop)
	}

	return out, nil
}

// programmeRiskChecks are the two risk questions that do not need a
// timeline, which is every risk question a programme can answer: it
// schedules nothing, so nothing here is about dates.
func programmeRiskChecks(spec map[string]any) []ProgrammeCheck {
	risks, _ := spec["risks"].([]any)
	if len(risks) == 0 {
		return nil
	}
	missingMitigation, deps, edges := 0, 0, 0
	for _, r := range risks {
		rm, ok := r.(map[string]any)
		if !ok {
			missingMitigation++
			continue
		}
		if m, _ := rm["mitigation"].(string); strings.TrimSpace(m) == "" {
			missingMitigation++
		}
		if t, _ := rm["type"].(string); t != "dependency" {
			continue
		}
		deps++
		dep, ok := rm["depends"].(map[string]any)
		if !ok {
			continue
		}
		if _, ok := parseRef(dep["on"]); ok {
			edges++
		}
	}
	var out []ProgrammeCheck
	if missingMitigation == 0 {
		out = append(out, ProgrammeCheck{ID: "risks-mitigation", Section: "risks",
			State: programmeCheckOK, Message: "Every risk has a mitigation."})
	} else {
		out = append(out, ProgrammeCheck{ID: "risks-mitigation", Section: "risks",
			State:   programmeCheckWarn,
			Message: fmt.Sprintf("%d of %d risks have no mitigation.", missingMitigation, len(risks))})
	}
	if deps > 0 {
		if edges == deps {
			out = append(out, ProgrammeCheck{ID: "risks-dependency-edges", Section: "risks",
				State: programmeCheckOK, Message: "Every dependency says what it waits on."})
		} else {
			out = append(out, ProgrammeCheck{ID: "risks-dependency-edges", Section: "risks",
				State:   programmeCheckWarn,
				Message: fmt.Sprintf("%d of %d dependencies do not say what they wait on.", deps-edges, deps)})
		}
	}
	return out
}

// programmeCycleCheck reports a loop through this programme, read from
// this programme so the sentence starts where the reader is. Nothing is
// reported for a programme holding no edge: an empty reassurance is a
// string that earns nothing.
func (e *Engine) programmeCycleCheck(ctx context.Context, id string) (*ProgrammeCheck, error) {
	edges, err := e.DependencyGraph(ctx)
	if err != nil {
		return nil, err
	}
	node := "Programme/" + id
	involved := false
	names := map[string]string{}
	for _, edge := range edges {
		names[edge.FromKind+"/"+edge.FromID] = edge.FromName
		names[edge.ToKind+"/"+edge.ToID] = edge.ToName
		if edge.FromKind == "Programme" && edge.FromID == id {
			involved = true
		}
	}
	if !involved {
		return nil, nil
	}
	nameOf := func(n string) string {
		if name := names[n]; name != "" {
			return name
		}
		return n[strings.Index(n, "/")+1:]
	}

	cycles, err := e.DependencyCycles(ctx)
	if err != nil {
		return nil, err
	}
	var loops []string
	for _, cycle := range cycles {
		at := slices.Index(cycle, node)
		if at < 0 {
			continue
		}
		path := make([]string, 0, len(cycle)+1)
		for i := range cycle {
			path = append(path, nameOf(cycle[(at+i)%len(cycle)]))
		}
		loops = append(loops, strings.Join(append(path, path[0]), " → "))
	}
	if len(loops) == 0 {
		return &ProgrammeCheck{ID: "risks-dependency-cycle", Section: "risks",
			State: programmeCheckOK, Message: "Nothing this waits on is waiting on it in return."}, nil
	}
	return &ProgrammeCheck{ID: "risks-dependency-cycle", Section: "risks",
		State: programmeCheckWarn, Message: fmt.Sprintf("A loop: %s.", strings.Join(loops, "; "))}, nil
}

// programmeMembers reads back what names this programme. Membership is
// declared by the work and derived here (TAXONOMY D1, D2), so both sides
// are read rather than a list kept on the programme.
func (e *Engine) programmeMembers(ctx context.Context, id string) (projects, operations []string, err error) {
	l := &lookup{ctx: ctx, store: e.manifests, codec: e.codec}
	// A project declares membership under alignment; an operation
	// declares it at the top of its spec, having no alignment block.
	for _, source := range []struct {
		kind string
		read func(spec map[string]any) []any
	}{
		{"Project", func(spec map[string]any) []any {
			alignment, _ := spec["alignment"].(map[string]any)
			out, _ := alignment["programmes"].([]any)
			return out
		}},
		{"Operation", func(spec map[string]any) []any {
			out, _ := spec["programmes"].([]any)
			return out
		}},
	} {
		docs, err := l.Documents(source.kind)
		if err != nil {
			return nil, nil, err
		}
		var found []string
		for docID, doc := range docs {
			spec, _ := doc["spec"].(map[string]any)
			if spec == nil {
				continue
			}
			// Both lists are plain slugs, not the Ref shape: the link
			// carries nothing of its own, so there is nothing to wrap.
			for _, p := range source.read(spec) {
				if named, _ := p.(string); strings.TrimSpace(named) == id {
					found = append(found, docID)
					break
				}
			}
		}
		sort.Strings(found)
		if source.kind == "Project" {
			projects = found
		} else {
			operations = found
		}
	}
	return projects, operations, nil
}
