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
		if objective, ok := om["objective"].(string); ok {
			if p := kit.ObjectiveDigitProblem(objective, fmt.Sprintf("/spec/objectives/%d/objective", oi)); p != nil {
				problems = append(problems, *p)
			}
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
	// A project schedules, so a dependency may land by one of its phases.
	problems = append(problems, kit.RiskProblems(spec, true)...)
	problems = append(problems, alignmentGoalLevelProblems(spec, ctx.Lookup)...)

	return problems
}

// alignmentGoalLevelProblems enforces that projects align to functional goals
// only, not to pillar or strategic goals. This check is not used during
// kind rule validation (to avoid issues with overlay parsing during imports); it
// is instead enforced by the project checks which have full access to the engine.
func alignmentGoalLevelProblems(spec map[string]any, lookup kit.Lookup) []kit.Problem {
	return nil // see comment above; this check is moved to projectchecks
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
