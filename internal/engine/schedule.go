package engine

import (
	"context"
	"fmt"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/timing"
)

// ScheduleItem is one milestone placed on time (TAXONOMY.md D48): the
// month it falls in as far as its timing says, its window, what it waits
// on, and whether it is on the chain that decides the last date.
type ScheduleItem struct {
	ID        string
	Name      string
	Form      string
	Month     string
	NotBefore string
	NotAfter  string
	WaitsOn   []string
	// Pending is a milestone set once something happens; Late is one whose
	// expected month has passed.
	Pending, Late bool
	Critical      bool
	// Unplaced is a milestone that follows something outside the project,
	// so no month can be worked out for it.
	Unplaced bool
}

// Schedule places a project's milestones on time, as ctx reads it. Each
// after-timing takes the month of what it follows plus its lag, in this
// project or another (TAXONOMY.md D47); a loop leaves the milestones on
// it unplaced. Nothing is rescheduled: this says what the definition
// implies.
func (e *Engine) Schedule(ctx context.Context, id string) ([]ScheduleItem, error) {
	p := &placer{e: e, ctx: ctx, specs: map[string]map[string]any{}, month: map[spot]string{}, state: map[spot]int{}, from: map[spot]string{}}
	spec, found, err := p.spec(id)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("%w: Project/%s", ErrNotFound, id)
	}
	ms, _ := spec["milestones"].([]any)
	chain := timing.Milestones(spec)
	order := []string{}
	byID := map[string]map[string]any{}
	for _, it := range ms {
		m, _ := it.(map[string]any)
		mid, _ := m["id"].(string)
		if mid == "" {
			continue
		}
		byID[mid] = m
		order = append(order, mid)
	}
	for _, mid := range order {
		p.place(id, mid)
	}
	month := func(mid string) string { return p.month[spot{id, mid}] }
	// The chain that decides the last date: from the latest milestone back
	// through whatever set each month, within this project.
	critical := map[string]bool{}
	last := ""
	// Of milestones in the latest month, the one with the longest chain
	// behind it ends the chain, then the later one listed.
	depth := func(mid string) int {
		n := 0
		for at := p.from[spot{id, mid}]; at != "" && n <= len(order); at = p.from[spot{id, at}] {
			n++
		}
		return n
	}
	for _, mid := range order {
		if last == "" || month(mid) > month(last) || (month(mid) == month(last) && depth(mid) >= depth(last)) {
			last = mid
		}
	}
	for at, n := last, 0; at != "" && n <= len(order); at, n = p.from[spot{id, at}], n+1 {
		critical[at] = true
	}
	if len(order) < 2 {
		critical = map[string]bool{}
	}
	var out []ScheduleItem
	for _, mid := range order {
		m := byID[mid]
		t := timing.Read(m["timing"])
		tm, _ := m["timing"].(map[string]any)
		nb, _ := tm["notBefore"].(string)
		na, _ := tm["notAfter"].(string)
		name, _ := m["name"].(string)
		out = append(out, ScheduleItem{
			ID: mid, Name: name, Form: t.Form, Month: month(mid), NotBefore: timing.TrimMonth(nb), NotAfter: timing.TrimMonth(na),
			WaitsOn: chain.Waits[mid], Pending: t.Form == "when", Late: t.Late, Critical: critical[mid],
			Unplaced: month(mid) == "",
		})
	}
	return out, nil
}

// spot is a milestone in a project.
type spot struct{ project, milestone string }

// placer places milestones across projects, each once, so a milestone that
// waits on another project's is placed from there and a loop of waits,
// in one project or across several, ends unplaced.
type placer struct {
	e     *Engine
	ctx   context.Context
	specs map[string]map[string]any
	month map[spot]string
	state map[spot]int // 0 new, 1 placing, 2 placed
	// from is the milestone in the same project that set each month.
	from map[spot]string
}

func (p *placer) spec(project string) (map[string]any, bool, error) {
	if s, ok := p.specs[project]; ok {
		return s, s != nil, nil
	}
	doc, found, err := p.e.docInPlay(p.ctx, "Project", project)
	if err != nil {
		return nil, false, err
	}
	var s map[string]any
	if found {
		s = specOf(doc)
	}
	p.specs[project] = s
	return s, found, nil
}

// item is the named item of a project's list.
func (p *placer) item(project, list, id string) map[string]any {
	spec, _, _ := p.spec(project)
	items, _ := spec[list].([]any)
	for _, it := range items {
		m, _ := it.(map[string]any)
		if s, _ := m["id"].(string); s == id {
			return m
		}
	}
	return nil
}

// due is the month a deliverable or condition is due, by its timing.
func (p *placer) due(project, list, id string) string {
	return timing.Read(p.item(project, list, id)["due"]).Month
}

// place is the month a project's milestone falls in, "" when it cannot be
// placed.
func (p *placer) place(project, mid string) string {
	at := spot{project, mid}
	switch p.state[at] {
	case 1:
		return "" // a loop: unplaced
	case 2:
		return p.month[at]
	}
	m := p.item(project, "milestones", mid)
	if m == nil {
		return ""
	}
	p.state[at] = 1
	t := timing.Read(m["timing"])
	out := t.Month
	follow := func(ev map[string]any, lagMonths, lagDays int) {
		on, _ := ev["on"].(map[string]any)
		list, _ := on["local"].(string)
		lid, _ := on["id"].(string)
		var base string
		switch list {
		case "milestones":
			base = p.place(project, lid)
		case "deliverables", "conditions":
			base = p.due(project, list, lid)
		}
		if k, _ := on["kind"].(string); k == "Project" && lid != "" {
			item, _ := ev["item"].(string)
			switch {
			case item == "":
			case p.item(lid, "milestones", item) != nil:
				base = p.place(lid, item)
			case p.item(lid, "deliverables", item) != nil:
				base = p.due(lid, "deliverables", item)
			default:
				base = p.due(lid, "conditions", item)
			}
		}
		if base == "" {
			return
		}
		got, err := addMonths(base, lagMonths+lagDays/30)
		if err != nil {
			return
		}
		if got > out {
			out = got
			if list == "milestones" {
				p.from[at] = lid
			} else {
				delete(p.from, at)
			}
		}
	}
	if t.Form == "after" && t.Event != nil {
		tm, _ := m["timing"].(map[string]any)
		follow(t.Event, intOf(tm["lagMonths"]), intOf(tm["lagDays"]))
	}
	ws, _ := m["waitsOn"].([]any)
	for _, w := range ws {
		if ev, ok := w.(map[string]any); ok {
			follow(ev, 0, 0)
		}
	}
	p.month[at] = out
	p.state[at] = 2
	return out
}
