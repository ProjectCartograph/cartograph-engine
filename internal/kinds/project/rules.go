// Package project implements the Project kind's rules beyond its JSON
// Schema: unique key result ids within each objective, the key result
// unit-by-kind rule, the objective must-be-qualitative rule, unique
// deliverable ids, unique success criteria ids, a success criterion's
// derivedFrom must name exactly one key result or deliverable that
// actually exists in this project, at most one funding line per currency,
// and a risk's escalate.reason is required when escalate.flag is true;
// every local reference resolves to an item this project
// actually has; and a dependency edge belongs to the dependency type and
// lands by a phase the timeline actually has.
package project

import (
	"fmt"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/kinds/kit"
)

func Rules(doc map[string]any, ctx kit.RuleContext) []kit.Problem {
	var problems []kit.Problem

	spec, _ := doc["spec"].(map[string]any)
	if spec == nil {
		return problems
	}

	// Citing part of a gap is only honest if the part exists.
	if summary, ok := spec["summary"].(map[string]any); ok {
		if list, ok := summary["problems"].([]any); ok {
			problems = append(problems,
				kit.GapCitationProblems(list, "/spec/summary/problems", ctx.Lookup)...)
		}
	}

	keyResultIDs := map[string]bool{}

	objectives, _ := spec["objectives"].([]any)
	for oi, o := range objectives {
		om, ok := o.(map[string]any)
		if !ok {
			continue
		}
		krs, _ := om["keyResults"].([]any)
		seen := map[string]int{}
		for ki, kr := range krs {
			m, ok := kr.(map[string]any)
			if !ok {
				continue
			}
			id, _ := m["id"].(string)
			if id == "" {
				continue
			}
			keyResultIDs[id] = true
			if first, dup := seen[id]; dup {
				problems = append(problems, kit.Problem{
					Path: fmt.Sprintf("/spec/objectives/%d/keyResults/%d/id", oi, ki),
					Message: fmt.Sprintf(
						"key result id %q duplicates the one at index %d in this objective", id, first),
				})
			} else {
				seen[id] = ki
			}
			if p := kit.KeyResultUnitProblem(m, fmt.Sprintf("/spec/objectives/%d/keyResults/%d", oi, ki)); p != nil {
				problems = append(problems, *p)
			}
		}
	}

	deliverableIDs := map[string]bool{}
	if deliverables, ok := spec["deliverables"].([]any); ok {
		seen := map[string]int{}
		for di, d := range deliverables {
			dm, ok := d.(map[string]any)
			if !ok {
				continue
			}
			id, _ := dm["id"].(string)
			if id == "" {
				continue
			}
			deliverableIDs[id] = true
			if first, dup := seen[id]; dup {
				problems = append(problems, kit.Problem{
					Path: fmt.Sprintf("/spec/deliverables/%d/id", di),
					Message: fmt.Sprintf(
						"deliverable id %q duplicates the one at index %d", id, first),
				})
			} else {
				seen[id] = di
			}
		}
	}

	if criteria, ok := spec["successCriteria"].([]any); ok {
		seen := map[string]int{}
		for ci, c := range criteria {
			cm, ok := c.(map[string]any)
			if !ok {
				continue
			}
			path := fmt.Sprintf("/spec/successCriteria/%d", ci)

			id, _ := cm["id"].(string)
			if id != "" {
				if first, dup := seen[id]; dup {
					problems = append(problems, kit.Problem{
						Path: path + "/id",
						Message: fmt.Sprintf(
							"success criterion id %q duplicates the one at index %d", id, first),
					})
				} else {
					seen[id] = ci
				}
			}
		}
	}

	problems = append(problems, fundingCurrencyProblems(spec)...)
	problems = append(problems, referenceProblems(spec)...)
	problems = append(problems, kit.MandateProblems(spec)...)
	// A project schedules, so a dependency may land by one of its phases.
	problems = append(problems, kit.RiskProblems(spec, true)...)
	problems = append(problems, affectsProblems(spec)...)
	problems = append(problems, singleRoleProblems(spec)...)

	return problems
}

// singleRoles are the roles a project holds once (TAXONOMY.md D61): one
// sponsor answers for it, one project manager runs it.
var singleRoles = map[string]string{"sponsor": "sponsor", "manager": "project manager"}

// singleRoleProblems refuses a second sponsor or project manager.
func singleRoleProblems(spec map[string]any) []kit.Problem {
	var out []kit.Problem
	resources, _ := spec["resources"].([]any)
	first := map[string]int{}
	for i, r := range resources {
		m, _ := r.(map[string]any)
		role, _ := m["role"].(string)
		name, single := singleRoles[role]
		if !single {
			continue
		}
		if at, dup := first[role]; dup {
			out = append(out, kit.Problem{
				Path:    fmt.Sprintf("/spec/resources/%d/role", i),
				Message: fmt.Sprintf("a project has one %s, named at resources %d: give this one another role, or replace that one", name, at),
			})
			continue
		}
		first[role] = i
	}
	return out
}

// fundingCurrencyProblems enforces at most one funding line per currency.
func fundingCurrencyProblems(spec map[string]any) []kit.Problem {
	var problems []kit.Problem
	funding, ok := spec["funding"].([]any)
	if !ok {
		return problems
	}
	seen := map[string]int{}
	for i, f := range funding {
		fm, ok := f.(map[string]any)
		if !ok {
			continue
		}
		currency, _ := fm["currency"].(string)
		if currency == "" {
			continue
		}
		if first, dup := seen[currency]; dup {
			problems = append(problems, kit.Problem{
				Path: fmt.Sprintf("/spec/funding/%d/currency", i),
				Message: fmt.Sprintf(
					"funding currency %q duplicates the line at index %d; at most one funding line per currency", currency, first),
			})
		} else {
			seen[currency] = i
		}
	}
	return problems
}

// referenceProblems checks every field that carries a reference: the role
// that verifies an acceptance criterion, the roles that track and confirm a
// success criterion, and the far end of a dependency. Cross-manifest
// references are resolved by the engine's generic walker; what is left for
// a kind rule is the local form, which the walker cannot resolve because
// resolving it means reading a sibling list of the same manifest.
func referenceProblems(spec map[string]any) []kit.Problem {
	var problems []kit.Problem
	add := func(p *kit.Problem) {
		if p != nil {
			problems = append(problems, *p)
		}
	}

	deliverables, _ := spec["deliverables"].([]any)
	for di, d := range deliverables {
		dm, ok := d.(map[string]any)
		if !ok {
			continue
		}
		criteria, _ := dm["acceptance"].([]any)
		for ci, cr := range criteria {
			cm, ok := cr.(map[string]any)
			if !ok {
				continue
			}
			add(kit.LocalRefProblem(spec, cm["by"],
				fmt.Sprintf("/spec/deliverables/%d/acceptance/%d/by", di, ci)))
		}
		tasks, _ := dm["tasks"].([]any)
		for ti, t := range tasks {
			if tm, ok := t.(map[string]any); ok {
				add(kit.LocalRefProblem(spec, tm["role"],
					fmt.Sprintf("/spec/deliverables/%d/tasks/%d/role", di, ti)))
			}
		}
	}

	criteria, _ := spec["successCriteria"].([]any)
	for i, cr := range criteria {
		cm, ok := cr.(map[string]any)
		if !ok {
			continue
		}
		// The outputs behind this outcome, where any were named. Optional
		// by design — a criterion may assess schedule, budget, compliance
		// or a dimension no deliverable produces — but a link to a
		// deliverable this project does not have is still wrong.
		if from, ok := cm["from"].([]any); ok {
			for j, f := range from {
				add(kit.LocalRefProblem(spec, f, fmt.Sprintf("/spec/successCriteria/%d/from/%d", i, j)))
			}
		}
		add(kit.LocalRefProblem(spec, cm["owner"], fmt.Sprintf("/spec/successCriteria/%d/owner", i)))
		add(kit.LocalRefProblem(spec, cm["confirmedBy"], fmt.Sprintf("/spec/successCriteria/%d/confirmedBy", i)))
	}

	risks, _ := spec["risks"].([]any)
	for i, r := range risks {
		rm, ok := r.(map[string]any)
		if !ok {
			continue
		}
		dep, ok := rm["depends"].(map[string]any)
		if !ok {
			continue
		}
		add(kit.LocalRefProblem(spec, dep["on"], fmt.Sprintf("/spec/risks/%d/depends/on", i)))
	}
	return problems
}

// affectsProblems holds a risk's place on the triple constraint
// (TAXONOMY.md D60) to the project: each side named once, and what it
// bears on a deliverable, milestone or cost line this project has.
func affectsProblems(spec map[string]any) []kit.Problem {
	items := map[string]bool{}
	for _, list := range []string{"deliverables", "milestones", "costs"} {
		entries, _ := spec[list].([]any)
		for _, e := range entries {
			if em, ok := e.(map[string]any); ok {
				if id, _ := em["id"].(string); id != "" {
					items[id] = true
				}
			}
		}
	}
	// The scope's lines, in and out, as written: a risk names one word for
	// word (TAXONOMY.md D62).
	lines := map[string]bool{}
	if summary, ok := spec["summary"].(map[string]any); ok {
		for _, list := range []string{"scopeIn", "scopeOut"} {
			entries, _ := summary[list].([]any)
			for _, e := range entries {
				if t, ok := e.(string); ok {
					lines[t] = true
				}
			}
		}
	}
	var problems []kit.Problem
	risks, _ := spec["risks"].([]any)
	for i, r := range risks {
		rm, ok := r.(map[string]any)
		if !ok {
			continue
		}
		if line, _ := rm["scopeLine"].(string); line != "" && !lines[line] {
			problems = append(problems, kit.Problem{Path: fmt.Sprintf("/spec/risks/%d/scopeLine", i),
				Message: fmt.Sprintf("names the scope line %q, which this project's scope does not have, in or out: name one as it is written", line)})
		}
		affects, _ := rm["affects"].([]any)
		seen := map[string]bool{}
		for j, a := range affects {
			am, ok := a.(map[string]any)
			if !ok {
				continue
			}
			path := fmt.Sprintf("/spec/risks/%d/affects/%d", i, j)
			side, _ := am["constraint"].(string)
			if seen[side] {
				problems = append(problems, kit.Problem{Path: path + "/constraint", Message: fmt.Sprintf("names %s twice: give each side once, with its impact", side)})
			}
			seen[side] = true
			if on, _ := am["on"].(string); on != "" && !items[on] {
				problems = append(problems, kit.Problem{Path: path + "/on", Message: fmt.Sprintf("bears on %q, which is not a deliverable, milestone or cost line of this project", on)})
			}
		}
	}
	return problems
}
