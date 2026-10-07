package engine

import (
	"fmt"
	"strings"
)

// addPlanChecks are the checks on what TAXONOMY.md D47 to D52 added: the
// milestones and what each waits on, the deliverable register, cost lines,
// conditions and sign-off. Each asks only of a project that uses the part
// it is about, so an older definition is not asked for what it never had.
func addPlanChecks(c checkAdder, spec map[string]any) {
	list := func(k string) []map[string]any {
		raw, _ := spec[k].([]any)
		out := make([]map[string]any, 0, len(raw))
		for _, it := range raw {
			if m, ok := it.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	}
	name := func(m map[string]any, keys ...string) string {
		for _, k := range keys {
			if s, _ := m[k].(string); strings.TrimSpace(s) != "" {
				return s
			}
		}
		id, _ := m["id"].(string)
		return id
	}

	// Milestones: each says when it falls, and the chain has no loop.
	if ms := list("milestones"); len(ms) > 0 {
		chain := readMilestones(spec)
		var open, late, dangling []string
		for _, m := range ms {
			t := readTiming(m["timing"])
			switch {
			case !t.Complete:
				open = append(open, name(m, "name"))
			case t.Late:
				late = append(late, fmt.Sprintf("%s (expected by %s)", name(m, "name"), monthWords(t.Month)))
			}
			id, _ := m["id"].(string)
			for _, to := range chain.Waits[id] {
				if _, ok := chain.Names[to]; !ok {
					dangling = append(dangling, name(m, "name"))
				}
			}
		}
		switch {
		case len(open) > 0:
			c.add("milestones-timing", "timeline", phaseInitiation, checkWarn,
				fmt.Sprintf("Say when %s fall%s: a date, a window, after another milestone, or when something happens.", englishList(open), map[bool]string{true: "s", false: ""}[len(open) == 1]))
		case len(dangling) > 0:
			c.add("milestones-timing", "timeline", phaseInitiation, checkWarn,
				fmt.Sprintf("%s wait%s on a milestone this project does not have.", englishList(dangling), map[bool]string{true: "s", false: ""}[len(dangling) == 1]))
		case len(late) > 0:
			c.add("milestones-timing", "timeline", phaseInitiation, checkWarn,
				fmt.Sprintf("%s: the month it was expected by has passed. Record what happened, or move it.", englishList(late)))
		default:
			c.add("milestones-timing", "timeline", phaseInitiation, checkOK,
				fmt.Sprintf("%d milestone%s, each with when it falls.", len(ms), plural(len(ms))))
		}
		if len(chain.Loop) > 0 {
			names := make([]string, len(chain.Loop))
			for i, id := range chain.Loop {
				names[i] = chain.Names[id]
			}
			c.add("milestones-loop", "timeline", phaseInitiation, checkBlock,
				fmt.Sprintf("These milestones wait on each other in a loop: %s. Remove one link.", strings.Join(names, " waits on ")))
		} else {
			c.add("milestones-loop", "timeline", phaseInitiation, checkOK, "No milestone waits on itself.")
		}
	}

	// The deliverable register: owner and due for each.
	if ds := list("deliverables"); len(ds) > 0 {
		var missing []string
		for _, d := range ds {
			_, owner := d["owner"].(map[string]any)
			due := readTiming(d["due"]).Complete
			if !owner || !due {
				missing = append(missing, name(d, "name"))
			}
		}
		if len(missing) == 0 {
			c.add("deliverables-register", "deliverables", phaseInitiation, checkOK, "Each deliverable has an owner and a due date.")
		} else {
			c.add("deliverables-register", "deliverables", phaseInitiation, checkWarn,
				fmt.Sprintf("Name the owner and when it is due for %s.", englishList(missing)))
		}
	}

	// Costs: an unfunded line is named until a condition decides it.
	if cs := list("costs"); len(cs) > 0 {
		conditions := map[string]bool{}
		for _, cd := range list("conditions") {
			if id, _ := cd["id"].(string); id != "" {
				conditions[id] = true
			}
		}
		var open []string
		for _, cl := range cs {
			status, _ := cl["status"].(string)
			if status != "unfunded" && status != "beingCosted" {
				continue
			}
			if cond, _ := cl["condition"].(string); !conditions[cond] {
				open = append(open, name(cl, "category"))
			}
		}
		if len(open) == 0 {
			c.add("costs-funded", "resources", phaseInitiation, checkOK, "Every cost line is funded, or a condition decides it.")
		} else {
			c.add("costs-funded", "resources", phaseInitiation, checkWarn,
				fmt.Sprintf("%s %s no funding yet. Add the condition that decides it.", englishList(open), map[bool]string{true: "has", false: "have"}[len(open) == 1]))
		}
	}

	// Conditions: each has an owner and a due.
	if cds := list("conditions"); len(cds) > 0 {
		var missing []string
		for _, cd := range cds {
			_, owner := cd["owner"].(map[string]any)
			if !owner || !readTiming(cd["due"]).Complete {
				missing = append(missing, name(cd, "action"))
			}
		}
		if len(missing) == 0 {
			c.add("conditions-due", "approval", phaseInitiation, checkOK,
				fmt.Sprintf("%d condition%s, each with an owner and a due date.", len(cds), plural(len(cds))))
		} else {
			c.add("conditions-due", "approval", phaseInitiation, checkWarn,
				fmt.Sprintf("Name the owner and due date of %d condition%s.", len(missing), plural(len(missing))))
		}
	}

	// Sign-off: someone approves the definition.
	if so := list("signOffs"); len(so) > 0 {
		approves := false
		for _, s := range so {
			if st, _ := s["stage"].(string); st == "definition" {
				approves = true
			}
		}
		if approves {
			c.add("signoffs-present", "approval", phaseInitiation, checkOK,
				fmt.Sprintf("%d sign-off line%s, including the definition's approval.", len(so), plural(len(so))))
		} else {
			c.add("signoffs-present", "approval", phaseInitiation, checkWarn, "No line approves the definition: add the role that approves it.")
		}
	}
}
