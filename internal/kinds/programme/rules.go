// Package programme implements the Programme kind's rules beyond its JSON
// Schema: the risk list a project and a programme share, checked here
// without phases, because a programme holds no timeline, and the gap
// citations they also share.
package programme

import (
	"fmt"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/kinds/kit"
)

func Rules(doc map[string]any, ctx kit.RuleContext) []kit.Problem {
	spec, _ := doc["spec"].(map[string]any)
	if spec == nil {
		return nil
	}
	// A programme schedules nothing: how it is staged is the delivery
	// tool's, so a dependency on a programme lands by no phase of its own.
	problems := kit.RiskProblems(spec, false)

	// A programme's problems carry the same citations a project's do, so
	// they answer to the same rule.
	if list, ok := spec["problems"].([]any); ok {
		problems = append(problems, kit.GapCitationProblems(list, "/spec/problems", ctx.Lookup)...)
	}
	problems = append(problems, pathwayProblems(spec)...)
	return problems
}

// pathwayProblems refuses a theory of change that has no beginning.
//
// A pathway is a set of causal steps: this outcome, from those. If the
// steps close on themselves then every outcome is waiting on another and
// nothing rests on the work — which is not a deep theory, it is one with
// no starting point, and everything that walks it to draw or roll it up
// would walk it forever.
//
// Everything else about a pathway is advisory. A programme with no
// pathway at all is one nobody has thought through yet, which is worth
// showing rather than refusing (TAXONOMY.md D12).
func pathwayProblems(spec map[string]any) []kit.Problem {
	steps, _ := spec["pathway"].([]any)
	if len(steps) == 0 {
		return nil
	}
	// The outcome each step reaches, and what it waits on.
	needs := map[string][]string{}
	at := map[string]int{}
	for i, s := range steps {
		sm, ok := s.(map[string]any)
		if !ok {
			continue
		}
		outcome, _ := sm["outcome"].(string)
		if outcome == "" {
			continue
		}
		if _, seen := at[outcome]; seen {
			return []kit.Problem{{
				Path: fmt.Sprintf("/spec/pathway/%d/outcome", i),
				Message: fmt.Sprintf(
					"%q is reached twice; one step per outcome, so a reader knows which reasoning applies", outcome),
			}}
		}
		at[outcome] = i
		needs[outcome] = append(needs[outcome], asStrings(sm["from"])...)
	}

	// Depth-first, marking what is on the stack: the standard walk, and
	// bounded by the number of steps.
	state := map[string]int{} // 0 unseen, 1 on the stack, 2 done
	var loop []string
	var walk func(string) bool
	walk = func(node string) bool {
		state[node] = 1
		for _, next := range needs[node] {
			if state[next] == 1 {
				loop = []string{next, node}
				return true
			}
			if state[next] == 0 && walk(next) {
				return true
			}
		}
		state[node] = 2
		return false
	}
	for outcome := range needs {
		if state[outcome] == 0 && walk(outcome) {
			return []kit.Problem{{
				Path: fmt.Sprintf("/spec/pathway/%d/from", at[loop[1]]),
				Message: fmt.Sprintf(
					"%q waits on %q, which waits back on it; a pathway that closes on itself has no first step",
					loop[1], loop[0]),
			}}
		}
	}
	return nil
}

// asStrings reads a list of ids, ignoring anything that is not one.
func asStrings(v any) []string {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		if s, ok := item.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}
