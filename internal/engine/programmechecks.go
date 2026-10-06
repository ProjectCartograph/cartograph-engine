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
	return e.programmeChecksOf(ctx, id, doc)
}

// programmeChecksOf checks a programme's document, saved or not: a draft, or
// what an agent proposes.
func (e *Engine) programmeChecksOf(ctx context.Context, id string, doc map[string]any) ([]ProgrammeCheck, error) {
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
	// Sub-programmes are components too (TAXONOMY.md D32).
	subs, err := e.namedBy(ctx, "Programme", func(spec map[string]any) []any {
		out, _ := spec["programmes"].([]any)
		return out
	}, id)
	if err != nil {
		return nil, err
	}
	if counted := countedList([]counted{{len(projects), "project"}, {len(subs), "sub-programme"}, {len(operations), "operation"}}); counted == "" {
		add("components-present", "components", programmeCheckWarn,
			"No project, sub-programme or operation has named this programme yet, so it coordinates nothing.")
	} else {
		add("components-present", "components", programmeCheckOK, counted+" named.")
	}

	// A member project serves the programme by moving one of its aims,
	// or an aim beneath one (D1: shared goals are the test of the
	// grouping); one that serves none says it belongs while serving
	// something else. Operations name no goals, so only projects are read.
	if goals, _ := spec["goals"].([]any); len(goals) > 0 && len(projects) > 0 {
		var unserved []string
		l := &lookup{ctx: ctx, store: e.manifests, codec: e.codec}
		docs, err := l.Documents("Project")
		if err != nil {
			return nil, err
		}
		read := e.storedGoals(ctx)
		aims := strs(goals)
		for _, pid := range projects {
			pspec, _ := docs[pid]["spec"].(map[string]any)
			alignment, _ := pspec["alignment"].(map[string]any)
			theirs, _ := alignment["goals"].([]any)
			// It serves the programme when one of its goals is one of the
			// programme's aims or beneath it.
			if !read.servesAny(strs(theirs), aims) {
				unserved = append(unserved, docName(docs[pid], pid))
			}
		}
		sort.Strings(unserved)
		if len(unserved) == 0 {
			add("components-serve-outcomes", "components", programmeCheckOK, "Every project in it serves one of its aims.")
		} else {
			add("components-serve-outcomes", "components", programmeCheckWarn,
				fmt.Sprintf("%s name%s this programme but serve%s none of its aims.", englishList(unserved),
					map[bool]string{true: "s", false: ""}[len(unserved) == 1], map[bool]string{true: "s", false: ""}[len(unserved) == 1]))
		}
	}

	// The change it exists for, the pathway it believes leads there, and
	// the team that leads it: without them a programme is a heading.
	aim, _ := spec["aim"].(map[string]any)
	if change, _ := aim["change"].(string); strings.TrimSpace(change) != "" {
		add("aim-change", "aim", programmeCheckOK, "The change it exists for is stated.")
	} else {
		add("aim-change", "aim", programmeCheckWarn, "No change stated yet.")
	}
	if steps, _ := spec["pathway"].([]any); len(steps) > 0 {
		add("pathway-steps", "pathway", programmeCheckOK, fmt.Sprintf("%d pathway step%s.", len(steps), plural(len(steps))))
	} else {
		// The theory of change is what makes it a programme rather than a
		// portfolio (TAXONOMY.md D32), so its absence says which it may be.
		add("pathway-steps", "pathway", programmeCheckWarn,
			"No theory of change yet: what must hold before the change happens. Without one this may be a portfolio, work grouped to fund and prioritise rather than to bring about one change.")
	}
	if lead, _ := spec["leadTeam"].(string); lead != "" {
		add("governance-lead", "governance", programmeCheckOK, "A lead team is named.")
	} else {
		add("governance-lead", "governance", programmeCheckWarn, "No lead team named yet.")
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

	// A loop between programmes is the fact a plan's many linkages make
	// likely and no single definition shows.
	loop, err := e.programmeCycleCheck(ctx, id)
	if err != nil {
		return nil, err
	}
	if loop != nil {
		out = append(out, *loop)
	}

	if pc, ok := pendingCheck(doc); ok {
		out = append(out, pc)
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
		if m, _ := rm["mitigation"].(string); strings.TrimSpace(m) == "" && rm["type"] != "constraint" {
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
			Message: fmt.Sprintf("%d of %d risks, issues and dependencies have no mitigation.", missingMitigation, mitigable(risks))})
	}
	if unowned := unownedHighRisks(risks); unowned == 0 {
		out = append(out, ProgrammeCheck{ID: "risks-owned", Section: "risks",
			State: programmeCheckOK, Message: "Every high-impact risk names the role that owns it."})
	} else {
		out = append(out, ProgrammeCheck{ID: "risks-owned", Section: "risks",
			State:   programmeCheckWarn,
			Message: unownedMessage(unowned)})
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

// strs keeps the strings of a list read from a manifest.
func strs(list []any) []string {
	var out []string
	for _, v := range list {
		if s, ok := v.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

// namedBy lists, sorted, the manifests of kind whose list (read from their
// spec) names id: what a programme or a portfolio holds, read back from
// what declares it (TAXONOMY.md D1, D32).
func (e *Engine) namedBy(ctx context.Context, kind string, read func(spec map[string]any) []any, id string) ([]string, error) {
	l := &lookup{ctx: ctx, store: e.manifests, codec: e.codec}
	docs, err := l.Documents(kind)
	if err != nil {
		return nil, err
	}
	var found []string
	for docID, doc := range docs {
		spec, _ := doc["spec"].(map[string]any)
		if spec == nil {
			continue
		}
		for _, p := range read(spec) {
			if named, _ := p.(string); strings.TrimSpace(named) == id {
				found = append(found, docID)
				break
			}
		}
	}
	sort.Strings(found)
	return found, nil
}

// counted is a number of things and the word for one of them.
type counted struct {
	n    int
	noun string
}

// countedList says the non-zero counts as a sentence's subject:
// "2 projects and 1 operation". Empty when every count is zero.
func countedList(cs []counted) string {
	var parts []string
	for _, c := range cs {
		if c.n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s%s", c.n, c.noun, plural(c.n)))
		}
	}
	return joinAnd(parts)
}
