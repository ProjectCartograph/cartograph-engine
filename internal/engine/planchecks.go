package engine

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/timing"
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
		chain := timing.Milestones(spec)
		var open, late, dangling []string
		for _, m := range ms {
			t := timing.Read(m["timing"])
			switch {
			case !t.Complete:
				open = append(open, name(m, "name"))
			case t.Late:
				late = append(late, fmt.Sprintf("%s (expected by %s)", name(m, "name"), timing.MonthWords(t.Month)))
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
			due := timing.Read(d["due"]).Complete
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
			c.add("costs-funded", "costs", phaseInitiation, checkOK, "Every cost line is funded, or a condition decides it.")
		} else {
			c.add("costs-funded", "costs", phaseInitiation, checkWarn,
				fmt.Sprintf("%s %s no funding yet. Add the condition that decides it.", englishList(open), map[bool]string{true: "has", false: "have"}[len(open) == 1]))
		}
	}

	// Conditions: each has an owner and a due.
	if cds := list("conditions"); len(cds) > 0 {
		var missing []string
		for _, cd := range cds {
			_, owner := cd["owner"].(map[string]any)
			if !owner || !timing.Read(cd["due"]).Complete {
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

// addAllocationChecks holds a project's cost lines to what each funding
// source has allocated it, as Allocation records say (TAXONOMY.md D69):
// the lines on one source come to no more than its allocation, in its
// currency, and say the status the allocation has. A source with no
// allocation to the project is not held to one.
func (e *Engine) addAllocationChecks(ctx context.Context, c checkAdder, id string, spec map[string]any) error {
	costs, _ := spec["costs"].([]any)
	type sum struct {
		amount   float64
		currency string
		statuses map[string]bool
	}
	bySource := map[string]*sum{}
	var order []string
	for _, cl := range costs {
		cm, _ := cl.(map[string]any)
		source, _ := cm["source"].(string)
		if source == "" {
			continue
		}
		s := bySource[source]
		if s == nil {
			s = &sum{statuses: map[string]bool{}}
			bySource[source] = s
			order = append(order, source)
		}
		s.amount += numberOf(cm["amount"])
		if cur, _ := cm["currency"].(string); cur != "" {
			s.currency = cur
		}
		if st, _ := cm["status"].(string); st != "" {
			s.statuses[st] = true
		}
	}
	// The allocations to this project, by the source they are from.
	allocs, err := e.allInPlay(ctx, "Allocation")
	if err != nil {
		return err
	}
	toProject := map[string]map[string]any{}
	for _, a := range allocs {
		as := specOf(a.doc)
		if p, _ := as["project"].(string); p == id {
			if src, _ := as["source"].(string); src != "" {
				toProject[src] = as
			}
		}
	}
	var over, unlike []string
	held := 0
	for _, source := range order {
		alloc := toProject[source]
		if alloc == nil {
			continue
		}
		doc, _, err := e.currentDoc(ctx, "FundingSource", source)
		if err != nil {
			return err
		}
		held++
		name := nameOf(doc, source)
		s := bySource[source]
		amount := numberOf(alloc["amount"])
		currency, _ := alloc["currency"].(string)
		if (currency == "" || currency == s.currency) && s.amount > amount {
			over = append(over, fmt.Sprintf("%s (%s %s planned, %s %s allocated)", name, s.currency, money(s.amount), currency, money(amount)))
		}
		status, _ := alloc["status"].(string)
		for st := range s.statuses {
			if st != status && st != "beingCosted" {
				unlike = append(unlike, fmt.Sprintf("%s (the lines say %s, the allocation %s)", name, st, status))
				break
			}
		}
	}
	switch {
	case len(over) > 0:
		c.add("costs-allocation", "costs", phaseInitiation, checkWarn,
			"Cost lines come to more than their source allocated: "+englishList(over)+". Lower the lines, or ask the budget holder for more.")
	case len(unlike) > 0:
		c.add("costs-allocation", "costs", phaseInitiation, checkWarn,
			"A cost line's status differs from its source's allocation: "+englishList(unlike)+". The allocation decides it.")
	case held > 0:
		c.add("costs-allocation", "costs", phaseInitiation, checkOK, "Every cost line is within what its source allocated.")
	}
	return nil
}

// money writes an amount with its thousands grouped: 1500000 is 1,500,000.
func money(v float64) string {
	s := strconv.FormatFloat(v, 'f', -1, 64)
	whole, frac, _ := strings.Cut(s, ".")
	var b strings.Builder
	for i, r := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	if frac != "" {
		return b.String() + "." + frac
	}
	return b.String()
}

// numberOf reads a number from a decoded document: YAML gives ints and
// floats, JSON floats.
func numberOf(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case uint64:
		return float64(n)
	}
	return 0
}
