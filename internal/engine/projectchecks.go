package engine

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

const (
	checkOK    = "ok"
	checkWarn  = "warn"
	checkBlock = "block"
)

const (
	phaseInitiation = "initiation"
	phaseClosing    = "closing"
	phaseLanding    = "landing"
)

// ProjectCheckFix names where in the definition journey a check's problem
// is fixed. It usually matches the check's own Phase/Section, except for a
// handful of checks (for example the operational owner binding, edited in
// the People section of Initiation even though the check itself belongs to
// the Landing phase) where the editable field lives elsewhere.
type ProjectCheckFix struct {
	Phase   string `json:"phase"`
	Section string `json:"section"`
}

// ProjectCheckItem is one row of GET /manifests/Project/{id}/checks. State
// block stops submission (draft to in review); warn never does.
type ProjectCheckItem struct {
	ID      string          `json:"id"`
	Section string          `json:"section"`
	Phase   string          `json:"phase"`
	State   string          `json:"state"`
	Message string          `json:"message"`
	Fix     ProjectCheckFix `json:"fix"`
}

// ProjectChecks is the full result: the derived end month (empty when the
// timeline cannot be computed), how many items block submission, and every
// check.
type ProjectChecks struct {
	DerivedEnd string             `json:"derivedEnd,omitempty"`
	Blocking   int                `json:"blocking"`
	Items      []ProjectCheckItem `json:"items"`
}

// checkAdder appends one check item, tallying blocking items as it goes.
// fixPhase/fixSection default to the item's own phase/section when both are
// empty.
type checkAdder struct {
	pc *ProjectChecks
}

func (a checkAdder) add(id, section, phase, state, message string) {
	a.addFix(id, section, phase, state, message, phase, section)
}

func (a checkAdder) addFix(id, section, phase, state, message, fixPhase, fixSection string) {
	a.pc.Items = append(a.pc.Items, ProjectCheckItem{
		ID: id, Section: section, Phase: phase, State: state, Message: message,
		Fix: ProjectCheckFix{Phase: fixPhase, Section: fixSection},
	})
	if state == checkBlock {
		a.pc.Blocking++
	}
}

// ProjectChecks computes every per-section check for a project, evaluating
// the current committed version. Checks never block a save; they only
// report, and drive the draft-to-in-review submission gate (Blocking == 0).
func (e *Engine) ProjectChecks(ctx context.Context, id string, draft bool) (ProjectChecks, error) {
	doc, err := e.loadProjectDoc(ctx, id)
	if err != nil {
		return ProjectChecks{}, err
	}
	return e.projectChecksOf(ctx, id, doc)
}

// projectChecksOf computes the checks for a project's document, saved or
// not: a draft, or what an agent proposes.
func (e *Engine) projectChecksOf(ctx context.Context, id string, doc map[string]any) (ProjectChecks, error) {
	spec, _ := doc["spec"].(map[string]any)
	if spec == nil {
		spec = map[string]any{}
	}

	var pc ProjectChecks
	c := checkAdder{&pc}

	start, phases, haveTimeline := parseTimeline(spec)
	if haveTimeline {
		if end, err := derivedEnd(start, phases); err == nil {
			pc.DerivedEnd = end
		}
	}

	// goals
	alignment, _ := spec["alignment"].(map[string]any)
	goals, _ := alignment["goals"].([]any)
	isComponent := componentParent(spec) != ""
	if len(goals) > 0 {
		c.add("goals-aligned", "goals", phaseInitiation, checkOK, "Aligned to at least one goal.")
	} else if isComponent {
		// A component serves its parent's objective, and through it the
		// parent's goals (TAXONOMY.md D15): naming them again would be a
		// second place to change them.
		c.add("goals-aligned", "goals", phaseInitiation, checkOK, "Serves the goals of the project it is part of.")
	} else {
		c.add("goals-aligned", "goals", phaseInitiation, checkBlock, "Not linked to an outcome yet.")
	}
	mandate, _ := spec["mandate"].([]any)
	if len(mandate) > 0 {
		c.add("aim-mandate", "aim", phaseInitiation, checkOK, "A mandate is named.")
	} else if isComponent {
		c.add("aim-mandate", "aim", phaseInitiation, checkOK, "Works under the mandate of the project it is part of.")
	} else {
		c.add("aim-mandate", "aim", phaseInitiation, checkWarn, "No mandate named yet.")
	}
	// Projects align to outcomes only: the work aligned to an outcome is
	// what the strategy reads under it (TAXONOMY.md D24). This check blocks
	// if aligned to a goal or an objective (goals-aligned above already blocks on
	// zero goals at all).
	if len(goals) > 0 {
		hasFunctional, hasNonFunctional, err := e.alignedToOutcome(ctx, goals)
		if err != nil {
			return ProjectChecks{}, err
		}
		if hasNonFunctional {
			c.add("goals-functional-level", "goals", phaseInitiation, checkBlock,
				"Aligned to a goal or an objective; align to an outcome instead.")
		} else if hasFunctional {
			c.add("goals-functional-level", "goals", phaseInitiation, checkOK, "Aligned to an outcome.")
		} else {
			c.add("goals-functional-level", "goals", phaseInitiation, checkBlock,
				"All aligned goals are unresolved; align to an outcome.")
		}
	}

	// Belonging to a programme is a declaration, not a derivation: the
	// project names the programmes it is part of, and the programme side is
	// read back from that. Sharing a goal corroborates the claim; it does
	// not constitute it.
	//
	// This warned rather than blocked from 2026-09-28. It blocked until
	// then, on the reading that membership is "proven, not asserted", and
	// three things were wrong with that (see TAXONOMY.md):
	//
	//   - A programme coordinates projects and other work toward benefits,
	//     and the work includes enabling components that serve no
	//     programme-level goal of their own. Refusing those refuses a
	//     normal part of a programme.
	//   - Shared goals is the test for whether a grouping deserves to be a
	//     programme at all rather than a portfolio; using it as a
	//     membership gate applies the portfolio criterion one level down.
	//   - It broke a manifest from outside itself. Editing a programme's
	//     own goals retroactively invalidated every project that named it,
	//     so a definition that was valid yesterday blocked today because
	//     somebody edited a different file. Nothing else here does that.
	//
	// It is still worth saying out loud, because a project in a programme
	// it shares nothing with is usually a mistake — just not always, and
	// not one this tool can settle.
	programmes, _ := alignment["programmes"].([]any)
	if len(programmes) > 0 {
		unproven, err := e.programmesWithoutSharedGoal(ctx, programmes, goals)
		if err != nil {
			return ProjectChecks{}, err
		}
		switch {
		case len(unproven) == 0:
			c.add("goals-programme-membership", "goals", phaseInitiation, checkOK,
				fmt.Sprintf("Part of %d programme%s, each sharing a goal with it.",
					len(programmes), plural(len(programmes))))
		case len(unproven) == 1:
			c.add("goals-programme-membership", "goals", phaseInitiation, checkWarn,
				fmt.Sprintf("%s shares no goal with this project.", unproven[0]))
		default:
			c.add("goals-programme-membership", "goals", phaseInitiation, checkWarn,
				fmt.Sprintf("%d programmes share no goal with this project.", len(unproven)))
		}
	}

	if err := e.addComponentChecks(ctx, c, id, spec); err != nil {
		return ProjectChecks{}, err
	}
	e.addCaptureChecks(ctx, c, id, spec)

	// goals and measures: the objective and its key results sit with the
	// goals they serve, so the alignment and the evidence are checked in
	// the same place.
	addMeasureChecks(c, spec)

	// aim: the problems and what will be different about each (the first
	// step of the journey), plus the mandate that authorises the project.
	// A project may answer more than one problem, and each is only stated
	// once both halves of it are.
	summary, _ := spec["summary"].(map[string]any)
	problems, _ := summary["problems"].([]any)
	stated, half := 0, 0
	for _, p := range problems {
		pm, ok := p.(map[string]any)
		if !ok {
			continue
		}
		// Both are held in parts now; the first part of each is what
		// makes it stated at all.
		situation := partOf(pm["problem"], "situation")
		what := partOf(pm["change"], "what")
		switch {
		case situation != "" && what != "":
			stated++
		case situation != "" || what != "":
			half++
		}
	}
	switch {
	case stated == 0:
		c.add("aim-problem-change", "aim", phaseInitiation, checkBlock, "The problem and the change both need to be stated.")
	case half > 0:
		c.add("aim-problem-change", "aim", phaseInitiation, checkBlock,
			fmt.Sprintf("%d problem%s states only one of its two halves.", half, plural(half)))
	case stated == 1:
		c.add("aim-problem-change", "aim", phaseInitiation, checkOK, "The problem and the change are both stated.")
	default:
		c.add("aim-problem-change", "aim", phaseInitiation, checkOK,
			fmt.Sprintf("%d problems, each with the change that answers it.", stated))
	}
	// Whom each problem is felt by: a problem stated about nobody is a
	// problem nobody can be asked to confirm.
	if stated > 0 {
		ungrouped := 0
		for _, p := range problems {
			pm, ok := p.(map[string]any)
			if !ok {
				continue
			}
			if groups, _ := pm["groups"].([]any); len(groups) == 0 {
				ungrouped++
			}
		}
		if ungrouped == 0 {
			c.addFix("aim-problem-groups", "aim", phaseInitiation, checkOK,
				"Every problem names the groups that feel it.", phaseInitiation, "beneficiaries")
		} else {
			c.addFix("aim-problem-groups", "aim", phaseInitiation, checkWarn,
				fmt.Sprintf("%d problem%s names no beneficiary group.", ungrouped, plural(ungrouped)),
				phaseInitiation, "beneficiaries")
		}
	}

	// scope
	scopeIn, _ := summary["scopeIn"].([]any)
	if len(scopeIn) > 0 {
		c.add("scope-in", "scope", phaseInitiation, checkOK, "At least one in-scope line.")
	} else {
		c.add("scope-in", "scope", phaseInitiation, checkWarn, "No in-scope line yet.")
	}
	// deliverables
	deliverables, _ := spec["deliverables"].([]any)
	if len(deliverables) > 0 {
		c.add("deliverables-count", "deliverables", phaseInitiation, checkOK,
			fmt.Sprintf("%d deliverable%s.", len(deliverables), plural(len(deliverables))))
	} else {
		c.add("deliverables-count", "deliverables", phaseInitiation, checkBlock, "No deliverables yet.")
	}
	missingAcceptance := 0
	unverified := 0
	for _, d := range deliverables {
		dm, ok := d.(map[string]any)
		if !ok {
			missingAcceptance++
			continue
		}
		criteria := acceptanceCriteria(dm)
		if len(criteria) == 0 {
			missingAcceptance++
			continue
		}
		for _, cr := range criteria {
			if _, ok := parseRef(cr["by"]); !ok {
				unverified++
			}
		}
	}
	if len(deliverables) > 0 {
		if missingAcceptance == 0 {
			c.add("deliverables-acceptance", "deliverables", phaseInitiation, checkOK, "Every deliverable says what makes it accepted.")
		} else {
			c.add("deliverables-acceptance", "deliverables", phaseInitiation, checkWarn,
				fmt.Sprintf("%d of %d deliverables do not say what makes them accepted.", missingAcceptance, len(deliverables)))
		}
		// A test nobody owns is a test nobody applies: every acceptance
		// criterion names the role that verifies it.
		if unverified == 0 {
			c.add("deliverables-verifier", "deliverables", phaseInitiation, checkOK, "Every acceptance criterion names who verifies it.")
		} else if unverified == 1 {
			c.add("deliverables-verifier", "deliverables", phaseInitiation, checkWarn,
				"1 acceptance criterion does not name who verifies it.")
		} else {
			c.add("deliverables-verifier", "deliverables", phaseInitiation, checkWarn,
				fmt.Sprintf("%d acceptance criteria do not name who verifies them.", unverified))
		}
	}

	// beneficiaries: who the project is for. Qualitative by design:
	// the groups are identified here and
	// nothing is counted, so the only question is whether any are named.
	beneficiaries, _ := summary["beneficiaries"].([]any)
	if len(beneficiaries) > 0 {
		c.add("beneficiaries-named", "beneficiaries", phaseInitiation, checkOK,
			fmt.Sprintf("%d beneficiary group%s.", len(beneficiaries), plural(len(beneficiaries))))
	} else {
		c.add("beneficiaries-named", "beneficiaries", phaseInitiation, checkWarn, "No beneficiary group named yet.")
	}

	// timeline
	if haveTimeline && start != "" && len(phases) > 0 {
		c.add("timeline-start-phases", "timeline", phaseInitiation, checkOK, "A start month and at least one phase are set.")
	} else {
		c.add("timeline-start-phases", "timeline", phaseInitiation, checkBlock, "A start month and at least one phase are both needed.")
	}

	// resources: the roles and the funding the project needs, and the
	// stakeholders it must keep close.
	addKPIChecks(c, spec, isComponent)

	addResourceChecks(c, spec, isComponent)

	// data
	addDataChecks(c, spec)

	// risks
	addRiskChecks(c, spec)

	// The two dependency facts no single manifest carries. They need the
	// whole vault, so they cannot sit with the rest of the risk checks.
	if err := e.addDependencyGraphChecks(ctx, c, id); err != nil {
		return ProjectChecks{}, err
	}

	// closing: what a project is accepted against is the acceptance
	// criteria its deliverables already carry,
	// not a second list authored on the closing step. So
	// what is checked here is whether every deliverable can be accepted
	// at all, with the fix filed where those tests are written rather
	// than where they are read back.
	criteria, _ := spec["successCriteria"].([]any)
	untestable := 0
	for _, d := range deliverables {
		dm, ok := d.(map[string]any)
		if !ok || len(acceptanceCriteria(dm)) == 0 {
			untestable++
		}
	}
	switch {
	case len(deliverables) == 0:
		c.addFix("closing-criteria", "closing", phaseClosing, checkBlock,
			"Nothing to accept yet: no deliverable.", phaseInitiation, "deliverables")
	case untestable > 0:
		c.addFix("closing-criteria", "closing", phaseClosing, checkBlock,
			fmt.Sprintf("%d of %d deliverables have no test to accept them against.", untestable, len(deliverables)),
			phaseInitiation, "deliverables")
	default:
		c.addFix("closing-criteria", "closing", phaseClosing, checkOK,
			fmt.Sprintf("Every deliverable has a test to accept it against (%d).", len(deliverables)),
			phaseInitiation, "deliverables")
	}

	// The standard itself is written on one step now,
	// so it is checked in one place rather than once per
	// phase. Every criterion needs the five parts that make it settle:
	// the outcome, the metric and the standard it clears, where that is
	// read from and how often, and the roles that track and confirm it.
	// A compliance line is satisfied rather than measured, so it is
	// never asked for a standard, a source or a cycle.
	// A requirement met or not is a success criterion of compliance
	// (TAXONOMY.md D25); a separate list of them, as projects kept before
	// 2.6.0, is a second place for the same thing.
	if lines, _ := spec["compliance"].([]any); len(lines) > 0 {
		c.addFix("compliance-as-criteria", "success", phaseInitiation, checkWarn,
			fmt.Sprintf("%d compliance line%s kept apart from the success criteria: record each as a criterion of compliance.", len(lines), plural(len(lines))),
			phaseInitiation, "success")
	}
	if len(criteria) == 0 {
		c.addFix("success-criteria", "success", phaseInitiation, checkBlock,
			"No success criterion yet.", phaseInitiation, "success")
	} else {
		c.addFix("success-criteria", "success", phaseInitiation, checkOK,
			fmt.Sprintf("%d success criteri%s.", len(criteria), plural2(len(criteria), "on", "a")),
			phaseInitiation, "success")
		unmeasured, unowned := 0, 0
		for _, cr := range criteria {
			cm, ok := cr.(map[string]any)
			if !ok {
				continue
			}
			metric, _ := cm["metric"].(string)
			if metric != "compliance" {
				standard, _ := cm["standard"].(string)
				source, _ := cm["source"].(string)
				cycle, _ := cm["cycle"].(string)
				if strings.TrimSpace(standard) == "" || strings.TrimSpace(source) == "" || strings.TrimSpace(cycle) == "" {
					unmeasured++
				}
			}
			if _, ok := parseRef(cm["owner"]); !ok {
				unowned++
			}
		}
		if unmeasured == 0 {
			c.addFix("success-measured", "success", phaseInitiation, checkOK,
				"Every measured criterion says what it clears, where it is read and how often.",
				phaseInitiation, "success")
		} else {
			c.addFix("success-measured", "success", phaseInitiation, checkBlock,
				fmt.Sprintf("%d criteri%s cannot be measured as written.", unmeasured, plural2(unmeasured, "on", "a")),
				phaseInitiation, "success")
		}
		if unowned == 0 {
			c.addFix("success-owner", "success", phaseInitiation, checkOK,
				"Every criterion names the role that tracks it.", phaseInitiation, "success")
		} else {
			c.addFix("success-owner", "success", phaseInitiation, checkWarn,
				fmt.Sprintf("Nobody tracks %d criteri%s.", unowned, plural2(unowned, "on", "a")),
				phaseInitiation, "success")
		}
	}

	// landing
	operation, _ := spec["operation"].(string)
	if strings.TrimSpace(operation) != "" {
		c.add("landing-operation", "landing", phaseLanding, checkOK, "An operation is named (or 'new').")
	} else {
		c.add("landing-operation", "landing", phaseLanding, checkBlock, "No operation is named yet.")
	}
	if isComponent && !criterionExists(criteria, "atLanding") && !criterionExists(criteria, "postClosingCycle") {
		// A component is accepted into its parent and lands with it: the
		// parent's landing test is the one that is judged (TAXONOMY.md D15).
		c.addFix("landing-criteria", "landing", phaseLanding, checkOK,
			"Lands with the project it is part of.", phaseInitiation, "success")
	} else if criterionExists(criteria, "atLanding") || criterionExists(criteria, "postClosingCycle") {
		c.addFix("landing-criteria", "landing", phaseLanding, checkOK,
			"At least one success criterion falls due once it is in use.", phaseInitiation, "success")
	} else {
		c.addFix("landing-criteria", "landing", phaseLanding, checkBlock,
			"Nothing has to be true once it is in use.", phaseInitiation, "success")
	}
	if len(resourcesWithRole(spec, "serviceOwner")) > 0 {
		c.addFix("landing-owner", "landing", phaseLanding, checkOK, "A service owner is named to accept the handover.", phaseInitiation, "resources")
	} else {
		c.addFix("landing-owner", "landing", phaseLanding, checkWarn, "No service owner named to accept the handover.", phaseInitiation, "resources")
	}

	return pc, nil
}

// alignedToOutcome reports whether at least one of the given Goal ids
// (Project.spec.alignment.goals, already parsed as []any) resolves to an
// outcome, and whether any is at another level. The check keeps its id,
// goals-functional-level, from when outcomes were called functional goals
// (D25): an id is part of the contract. A dangling reference (should not happen: the generic reference
// check already refuses those at Commit) is silently skipped rather than
// erroring, matching every other check's leniency toward a draft's own
// possibly-inconsistent state.
func (e *Engine) alignedToOutcome(ctx context.Context, goals []any) (hasFunctional, hasNonFunctional bool, err error) {
	for _, g := range goals {
		id, ok := g.(string)
		if !ok || id == "" {
			continue
		}
		v, found, err := e.manifests.GetCurrent(ctx, "Goal", id)
		if err != nil {
			return false, false, err
		}
		if !found {
			continue
		}
		var doc goalDoc
		if e.codec.DecodeInto(v.YAML, &doc) == nil {
			if doc.Spec.Level == "outcome" {
				hasFunctional = true
			} else {
				hasNonFunctional = true
			}
		}
	}
	return
}

// addMeasureChecks reports on the objective and its key results. Each part
// of a key result is checked and named on its own: a check that lumps the
// baseline, the target and the source together reports a missing baseline
// at a key result that has one, which is worse than no check at all.
func addMeasureChecks(c checkAdder, spec map[string]any) {
	// The measures step is its own step now (Align picks the goals, Refine
	// writes the objective and its key results), so its checks carry its own
	// section and a fix link lands on the screen that owns the problem.
	const section = "measures"
	objectives, _ := spec["objectives"].([]any)
	if len(objectives) > 0 {
		c.add("goals-objective", section, phaseInitiation, checkOK,
			fmt.Sprintf("%d objective%s.", len(objectives), plural(len(objectives))))
	} else {
		c.add("goals-objective", section, phaseInitiation, checkBlock, "No objective yet.")
	}

	badCount := 0
	noBaseline := 0
	noTarget := 0
	noSource := 0
	totalKR := 0
	for _, o := range objectives {
		om, ok := o.(map[string]any)
		if !ok {
			continue
		}
		krs, _ := om["keyResults"].([]any)
		if len(krs) < 1 || len(krs) > 3 {
			badCount++
		}
		for _, kr := range krs {
			totalKR++
			m, ok := kr.(map[string]any)
			if !ok {
				noBaseline++
				noTarget++
				noSource++
				continue
			}
			if !keyResultHasBaseline(m) {
				noBaseline++
			}
			if !keyResultHasTarget(m) {
				noTarget++
			}
			if source, _ := m["source"].(string); strings.TrimSpace(source) == "" {
				noSource++
			}
		}
	}
	if len(objectives) == 0 {
		return
	}
	if badCount == 0 {
		c.add("goals-key-results-count", section, phaseInitiation, checkOK, "Every objective has one to three key results.")
	} else {
		c.add("goals-key-results-count", section, phaseInitiation, checkBlock,
			fmt.Sprintf("%d objective%s does not have one to three key results.", badCount, plural(badCount)))
	}
	if totalKR == 0 {
		return
	}
	if noBaseline == 0 {
		c.add("goals-key-results-baseline", section, phaseInitiation, checkOK,
			"Every key result has a baseline, or says why it is not known yet.")
	} else {
		c.add("goals-key-results-baseline", section, phaseInitiation, checkBlock,
			fmt.Sprintf("%d of %d key results has no baseline and no reason it is not known yet.", noBaseline, totalKR))
	}
	if noTarget == 0 {
		c.add("goals-key-results-target", section, phaseInitiation, checkOK, "Every key result has a target.")
	} else {
		c.add("goals-key-results-target", section, phaseInitiation, checkBlock,
			fmt.Sprintf("%d of %d key results has no target.", noTarget, totalKR))
	}
	if noSource == 0 {
		c.add("goals-key-results-source", section, phaseInitiation, checkOK, "Every key result names what measures it.")
	} else {
		c.add("goals-key-results-source", section, phaseInitiation, checkBlock,
			fmt.Sprintf("%d of %d key results does not name a data source.", noSource, totalKR))
	}
}

// addKPIChecks reads Project.spec.kpis. A KPI is named by the project as a
// whole, with the reason it is named, and is no longer a property of one
// key result: one KPI may be moved by several
// projects, so tying it to a single key result said something untrue.
func addKPIChecks(c checkAdder, spec map[string]any, isComponent bool) {
	kpis, _ := spec["kpis"].([]any)
	if len(kpis) == 0 && isComponent {
		c.add("measures-kpis", "measures", phaseInitiation, checkOK, "Moves the indicators of the project it is part of.")
		return
	}
	if len(kpis) == 0 {
		c.add("measures-kpis", "measures", phaseInitiation, checkWarn, "No indicator named yet.")
		return
	}
	c.add("measures-kpis", "measures", phaseInitiation, checkOK,
		fmt.Sprintf("%d KPI%s named.", len(kpis), plural(len(kpis))))
	// One indicator named twice says the same thing twice, with two
	// reasons that can drift apart.
	seen := map[string]bool{}
	var twice []string
	for _, k := range kpis {
		km, _ := k.(map[string]any)
		id, _ := km["kpi"].(string)
		if id != "" && seen[id] && !slices.Contains(twice, id) {
			twice = append(twice, id)
		}
		seen[id] = true
	}
	if len(twice) > 0 {
		c.add("measures-kpis-once", "measures", phaseInitiation, checkWarn, "Named more than once: "+strings.Join(twice, ", ")+".")
	} else {
		c.add("measures-kpis-once", "measures", phaseInitiation, checkOK, "Each KPI is named once.")
	}
}

// addResourceChecks reads Project.spec.resources and spec.funding: one
// step for what the project needs around it, roles and money and the
// people who must be kept close. Roles, never persons.
func addResourceChecks(c checkAdder, spec map[string]any, isComponent bool) {
	resources, _ := spec["resources"].([]any)

	// The approved envelope sits with what the project needs to run, not
	// with where it stops: the charter template puts money and resources
	// in one section, and scope is the boundary, not the budget.
	funding, _ := spec["funding"].([]any)
	if len(funding) > 0 {
		c.add("resources-funding", "resources", phaseInitiation, checkOK, "Funding is named.")
	} else if isComponent {
		c.add("resources-funding", "resources", phaseInitiation, checkOK, "Funded through the project it is part of.")
	} else {
		c.add("resources-funding", "resources", phaseInitiation, checkWarn, "No funding line yet.")
	}

	hasSponsor := len(resourcesWithRole(spec, "sponsor")) > 0
	hasLead := len(resourcesWithRole(spec, "manager")) > 0
	if hasSponsor && hasLead {
		c.add("resources-sponsor-lead", "resources", phaseInitiation, checkOK, "A sponsor and a project manager are named.")
	} else if isComponent && hasLead {
		// One sponsor for the whole project: a component answers to its
		// parent's.
		c.add("resources-sponsor-lead", "resources", phaseInitiation, checkOK, "A project manager is named; the sponsor is the parent project's.")
	} else if isComponent {
		c.add("resources-sponsor-lead", "resources", phaseInitiation, checkBlock, "A project manager is needed.")
	} else {
		c.add("resources-sponsor-lead", "resources", phaseInitiation, checkBlock, "A sponsor and a project manager are both needed.")
	}

	// Stakeholders are named beside the roles and the funding: one step
	// for everything the project needs around it (the organisation's own
	// charter groups them the same way, under "Stakeholders and
	// Resources").
	// Whether the work has named its stakeholders, and placed them, is not
	// a fact this document holds. A stakeholder is not a position a project
	// staffs, so it is not in the resource list; who has a stake, and their
	// place on the grid, is on the StakeholderMap scoped to the work, whose
	// own checks ask (stakeholderchecks.go). Asking here would only produce
	// a warning nothing on this screen could clear.

	// Informational, never warn: how many of the project's own roles are
	// still named in free text rather than picked from the Resource
	// catalogue. It used to count every role, mapped or not, and to say
	// they were unmapped "to a person", which no part of Cartograph holds: a
	// definition names roles, and who fills one lives in the delivery tool
	// (the Person kind was removed).
	unnamed := 0
	for _, r := range resources {
		rm, ok := r.(map[string]any)
		if !ok {
			continue
		}
		if res, _ := rm["resource"].(string); res == "" {
			unnamed++
		}
	}
	c.add("resources-mapped", "resources", phaseInitiation, checkOK,
		fmt.Sprintf("%d of %d role%s named from the resource catalogue.",
			len(resources)-unnamed, len(resources), plural(len(resources))))
}

// programmesWithoutSharedGoal names the programmes a project claims that
// share none of its goals. A programme that cannot be resolved is skipped
// rather than reported: the generic reference check already refuses a
// dangling one, and reporting it twice would say the same thing twice.
func (e *Engine) programmesWithoutSharedGoal(ctx context.Context, programmes []any, goals []any) ([]string, error) {
	var projectGoals []string
	for _, g := range goals {
		if id, ok := g.(string); ok && id != "" {
			projectGoals = append(projectGoals, id)
		}
	}
	read := e.storedGoals(ctx)
	var unproven []string
	for _, p := range programmes {
		id, ok := p.(string)
		if !ok || id == "" {
			continue
		}
		v, found, err := e.manifests.GetCurrent(ctx, "Programme", id)
		if err != nil {
			return nil, err
		}
		if !found {
			continue
		}
		var doc struct {
			Metadata struct {
				Name string `yaml:"name"`
			} `yaml:"metadata"`
			Spec struct {
				Goals []string `yaml:"goals"`
			} `yaml:"spec"`
		}
		if e.codec.DecodeInto(v.YAML, &doc) != nil {
			continue
		}
		// Shared when one of the project's goals is one of the programme's
		// or beneath it: the programme may be judged at any level.
		if !read.servesAny(projectGoals, doc.Spec.Goals) {
			name := doc.Metadata.Name
			if name == "" {
				name = id
			}
			unproven = append(unproven, name)
		}
	}
	return unproven, nil
}

// resourcesWithRole returns every Project.spec.resources[] entry with the
// given role.
// plural2 picks between two whole endings, for words a bare "s" does not
// pluralise: "criterion" and "criteria".
func plural2(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func resourcesWithRole(spec map[string]any, role string) []map[string]any {
	resources, _ := spec["resources"].([]any)
	var out []map[string]any
	for _, r := range resources {
		rm, ok := r.(map[string]any)
		if !ok {
			continue
		}
		if rv, _ := rm["role"].(string); rv == role {
			out = append(out, rm)
		}
	}
	return out
}

func addDataChecks(c checkAdder, spec map[string]any) {
	data, _ := spec["data"].(map[string]any)
	produces, _ := data["produces"].([]any)
	consumes, _ := data["consumes"].([]any)

	if len(produces) > 0 {
		missing := 0
		// Data that lands nowhere is data nobody can find again: every
		// output names the register, system or store that holds it
		// afterwards.
		noSink := 0
		for _, p := range produces {
			pm, ok := p.(map[string]any)
			if !ok {
				missing++
				noSink++
				continue
			}
			if pd, _ := pm["personalData"].(string); pd == "" {
				missing++
			}
			if sink, _ := pm["sink"].(string); strings.TrimSpace(sink) == "" {
				noSink++
			}
		}
		if missing == 0 {
			c.add("data-personal-data", "data", phaseInitiation, checkOK, "Everything produced has personal data set.")
		} else {
			c.add("data-personal-data", "data", phaseInitiation, checkBlock,
				fmt.Sprintf("%d of %d produced outputs have no personal data setting.", missing, len(produces)))
		}
		if noSink == 0 {
			c.add("data-sink", "data", phaseInitiation, checkOK, "Everything produced lands somewhere named.")
		} else {
			c.add("data-sink", "data", phaseInitiation, checkBlock,
				fmt.Sprintf("%d of %d produced outputs do not say where they land.", noSink, len(produces)))
		}
	}

	manualReentry := 0
	for _, cs := range consumes {
		cm, ok := cs.(map[string]any)
		if !ok {
			continue
		}
		if h, _ := cm["handoff"].(string); h == "manualReentry" {
			manualReentry++
		}
	}
	if manualReentry > 0 {
		c.add("data-handoff", "data", phaseInitiation, checkWarn,
			fmt.Sprintf("%d consumed source%s handed off by manual re-entry.", manualReentry, plural(manualReentry)))
	}
}

func addRiskChecks(c checkAdder, spec map[string]any) {
	risks, _ := spec["risks"].([]any)
	if len(risks) == 0 {
		return
	}
	missingMitigation := 0
	escalated := 0
	unplaced, scored := 0, 0
	for _, r := range risks {
		rm, ok := r.(map[string]any)
		if !ok {
			missingMitigation++
			continue
		}
		// A risk is weighed by its impact and its likelihood; one with
		// either missing cannot be placed on the matrix or compared.
		if t, _ := rm["type"].(string); t == "risk" {
			scored++
			if imp, _ := rm["impact"].(string); imp == "" {
				unplaced++
			} else if lik, _ := rm["likelihood"].(string); lik == "" {
				unplaced++
			}
		}
		if m, _ := rm["mitigation"].(string); strings.TrimSpace(m) == "" {
			missingMitigation++
		}
		if esc, ok := rm["escalate"].(map[string]any); ok {
			if flag, _ := esc["flag"].(bool); flag {
				escalated++
			}
		}
	}
	if missingMitigation == 0 {
		c.add("risks-mitigation", "risks", phaseInitiation, checkOK, "Every risk has a mitigation.")
	} else {
		c.add("risks-mitigation", "risks", phaseInitiation, checkWarn,
			fmt.Sprintf("%d of %d risks have no mitigation.", missingMitigation, len(risks)))
	}
	if scored > 0 {
		if unplaced == 0 {
			c.add("risks-placed", "risks", phaseInitiation, checkOK, "Every risk has an impact and a likelihood.")
		} else {
			c.add("risks-placed", "risks", phaseInitiation, checkWarn,
				fmt.Sprintf("%d of %d risks have no impact or likelihood yet.", unplaced, scored))
		}
	}
	if escalated > 0 {
		c.add("risks-escalated", "risks", phaseInitiation, checkOK,
			fmt.Sprintf("%d risk%s escalated.", escalated, plural(escalated)))
	} else {
		c.add("risks-escalated", "risks", phaseInitiation, checkOK, "No risks escalated.")
	}
	addDependencyChecks(c, spec, risks)
}

// addDependencyChecks reports on the dependency rows specifically. A
// dependency is the one risk type that carries an edge, and an edge is the
// whole point of the type: without one the row is a sentence filed under
// the wrong word, which is what most rows typed dependency were before
// the edge existed. Saying so is what keeps the category honest.
//
// Edges that leave Cartograph are counted, not faulted. A project really does
// wait on the board, and the alternative to recording that is inventing a
// manifest for the board.
func addDependencyChecks(c checkAdder, spec map[string]any, risks []any) {
	deps, edges, outside, undirected := 0, 0, 0, 0
	for _, r := range risks {
		rm, ok := r.(map[string]any)
		if !ok {
			continue
		}
		if t, _ := rm["type"].(string); t != "dependency" {
			continue
		}
		deps++
		dep, ok := rm["depends"].(map[string]any)
		if !ok {
			continue
		}
		on, ok := parseRef(dep["on"])
		if !ok {
			continue
		}
		edges++
		if on.outsideCartograph() {
			outside++
		}
		if d, _ := dep["direction"].(string); d == "" {
			undirected++
		}
	}
	if deps == 0 {
		return
	}
	switch {
	case edges == deps && undirected == 0:
		c.add("risks-dependency-edges", "risks", phaseInitiation, checkOK,
			fmt.Sprintf("Every dependency says what it waits on%s.",
				outsideNote(outside)))
	default:
		c.add("risks-dependency-edges", "risks", phaseInitiation, checkWarn,
			fmt.Sprintf("%d of %d dependencies do not say what they wait on.", deps-edges+undirected, deps))
	}
}

// outsideNote adds the count of edges that leave Cartograph to a passing
// message, so the number is visible without being a fault.
func outsideNote(outside int) string {
	if outside == 0 {
		return ""
	}
	return fmt.Sprintf(" (%d outside Cartograph)", outside)
}

// acceptanceCriteria returns a deliverable's acceptance criteria as parsed
// maps, skipping anything that is not one.
func acceptanceCriteria(deliverable map[string]any) []map[string]any {
	raw, _ := deliverable["acceptance"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if m, ok := item.(map[string]any); ok {
			if outcome, _ := m["outcome"].(string); strings.TrimSpace(outcome) != "" {
				out = append(out, m)
			}
		}
	}
	return out
}

// keyResultHasBaseline reports whether a key result (as a raw parsed map)
// has a usable baseline: a known value, or an admitted unknown with a
// reason. A baseline of zero is a baseline; only an absent one is missing.
func keyResultHasBaseline(kr map[string]any) bool {
	baseline, ok := kr["baseline"].(map[string]any)
	if !ok {
		return false
	}
	if _, hasValue := baseline["value"]; hasValue {
		return true
	}
	reason, _ := baseline["unknownReason"].(string)
	return strings.TrimSpace(reason) != ""
}

// keyResultHasTarget reports whether a key result carries a target value.
func keyResultHasTarget(kr map[string]any) bool {
	target, ok := kr["target"].(map[string]any)
	if !ok {
		return false
	}
	_, hasValue := target["value"]
	return hasValue
}

func criterionExists(criteria []any, when string) bool {
	for _, item := range criteria {
		cm, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if w, _ := cm["when"].(string); w == when {
			return true
		}
	}
	return false
}

// loadProjectDoc returns the parsed manifest document for the current
// committed version.
func (e *Engine) loadProjectDoc(ctx context.Context, id string) (map[string]any, error) {
	v, found, err := e.manifests.GetCurrent(ctx, "Project", id)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("%w: Project/%s", ErrNotFound, id)
	}
	var doc map[string]any
	if err := e.codec.DecodeInto(v.YAML, &doc); err != nil {
		return nil, fmt.Errorf("parse Project/%s: %w", id, err)
	}
	// Same reason as Engine.Get: the file may predate the current shape.
	rewriteLegacyFields("Project", doc)
	return doc, nil
}

// parseTimeline reads spec.timeline.start and spec.timeline.phases[].months
// leniently (a draft may carry a partial or malformed shape). ok is false
// when there is no usable timeline object at all.
func parseTimeline(spec map[string]any) (start string, phases []int, ok bool) {
	timeline, isMap := spec["timeline"].(map[string]any)
	if !isMap {
		return "", nil, false
	}
	start, _ = timeline["start"].(string)
	rawPhases, _ := timeline["phases"].([]any)
	for _, p := range rawPhases {
		pm, ok := p.(map[string]any)
		if !ok {
			continue
		}
		switch months := pm["months"].(type) {
		case int:
			phases = append(phases, months)
		case float64:
			phases = append(phases, int(months))
		}
	}
	return start, phases, true
}

// derivedEnd advances start (yyyy-mm) by the sum of every phase's months.
func derivedEnd(start string, phaseMonths []int) (string, error) {
	total := 0
	for _, m := range phaseMonths {
		total += m
	}
	return addMonths(start, total)
}

// addMonths advances a yyyy-mm string by n months (n may be 0).
func addMonths(yearMonth string, n int) (string, error) {
	y, m, ok := splitYearMonth(yearMonth)
	if !ok {
		return "", fmt.Errorf("not a year-month: %q", yearMonth)
	}
	total := y*12 + (m - 1) + n
	ny := total / 12
	nm := total%12 + 1
	return fmt.Sprintf("%04d-%02d", ny, nm), nil
}

func splitYearMonth(s string) (year, month int, ok bool) {
	parts := strings.SplitN(s, "-", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	y, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return y, m, true
}

// addDependencyGraphChecks reports the two things an edge makes visible
// that reading one definition cannot: a loop, and a date that cannot be
// met. Both are computed across every manifest holding an edge.
//
// Both warn rather than block, for the reason programme membership was
// demoted on 2026-09-28: either one can appear because somebody edited a
// different file. A definition that was valid yesterday must not refuse to
// save today because another project moved its start month.
func (e *Engine) addDependencyGraphChecks(ctx context.Context, c checkAdder, id string) error {
	edges, err := e.DependencyGraph(ctx)
	if err != nil {
		return err
	}
	node := "Project/" + id
	// Only projects holding an edge get either check: a project waiting on
	// nothing has nothing to report, and an empty "no loops here" line is a
	// string earning nothing.
	involved := false
	for _, edge := range edges {
		if edge.FromKind == "Project" && edge.FromID == id {
			involved = true
			break
		}
	}
	if !involved {
		return nil
	}

	names := map[string]string{}
	for _, edge := range edges {
		names[edge.FromKind+"/"+edge.FromID] = edge.FromName
		names[edge.ToKind+"/"+edge.ToID] = edge.ToName
	}
	nameOf := func(n string) string {
		if name := names[n]; name != "" {
			return name
		}
		return strings.TrimPrefix(n, "Project/")
	}

	cycles, err := e.DependencyCycles(ctx)
	if err != nil {
		return err
	}
	var loops []string
	for _, cycle := range cycles {
		if !slices.Contains(cycle, node) {
			continue
		}
		// Read the loop from this project, so the sentence starts where
		// the reader is rather than wherever the walk entered it.
		at := slices.Index(cycle, node)
		path := make([]string, 0, len(cycle)+1)
		for i := range cycle {
			path = append(path, nameOf(cycle[(at+i)%len(cycle)]))
		}
		loops = append(loops, strings.Join(append(path, path[0]), " → "))
	}
	if len(loops) > 0 {
		c.add("risks-dependency-cycle", "risks", phaseInitiation, checkWarn,
			fmt.Sprintf("A loop: %s.", strings.Join(loops, "; ")))
	} else {
		c.add("risks-dependency-cycle", "risks", phaseInitiation, checkOK,
			"Nothing this waits on is waiting on it in return.")
	}

	conflicts, err := e.DependencyScheduleConflicts(ctx)
	if err != nil {
		return err
	}
	var late []string
	for _, conflict := range conflicts {
		if conflict.Edge.FromKind != "Project" || conflict.Edge.FromID != id {
			continue
		}
		late = append(late, fmt.Sprintf("%s is needed by %s but does not finish until %s",
			conflict.Edge.ToName, conflict.NeedByOn, conflict.ReadyOn))
	}
	if len(late) > 0 {
		c.add("risks-dependency-schedule", "risks", phaseInitiation, checkWarn,
			capitalise(strings.Join(late, "; "))+".")
	} else {
		c.add("risks-dependency-schedule", "risks", phaseInitiation, checkOK,
			"Every dependency lands before it is needed.")
	}
	return nil
}

// capitalise raises the first letter of a message assembled from parts, so
// a sentence built out of names still reads as one.
func capitalise(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// partOf reads one field of a statement held in parts.
func partOf(holder any, field string) string {
	m, ok := holder.(map[string]any)
	if !ok {
		return ""
	}
	s, _ := m[field].(string)
	return strings.TrimSpace(s)
}
