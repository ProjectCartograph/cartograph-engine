package engine

import (
	"context"
	"fmt"
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
// after-timing takes the month of what it follows plus its lag; a loop
// leaves the milestones on it unplaced. Nothing is rescheduled: this says
// what the definition implies.
func (e *Engine) Schedule(ctx context.Context, id string) ([]ScheduleItem, error) {
	doc, found, err := e.docInPlay(ctx, "Project", id)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("%w: Project/%s", ErrNotFound, id)
	}
	spec := specOf(doc)
	ms, _ := spec["milestones"].([]any)
	chain := readMilestones(spec)
	byID := map[string]map[string]any{}
	order := []string{}
	for _, it := range ms {
		m, _ := it.(map[string]any)
		mid, _ := m["id"].(string)
		if mid == "" {
			continue
		}
		byID[mid] = m
		order = append(order, mid)
	}
	// Deliverables and conditions can be waited on too, through their own
	// due timing when it names a month.
	localMonth := func(list, lid string) string {
		items, _ := spec[list].([]any)
		for _, it := range items {
			m, _ := it.(map[string]any)
			if s, _ := m["id"].(string); s == lid {
				return readTiming(m["due"]).Month
			}
		}
		return ""
	}
	month := map[string]string{}
	from := map[string]string{}
	state := map[string]int{} // 0 new, 1 visiting, 2 done
	var place func(mid string) string
	place = func(mid string) string {
		switch state[mid] {
		case 1:
			return "" // a loop: unplaced
		case 2:
			return month[mid]
		}
		state[mid] = 1
		m := byID[mid]
		t := readTiming(m["timing"])
		out := t.Month
		follow := func(ev map[string]any, lagMonths, lagDays int) {
			on, _ := ev["on"].(map[string]any)
			list, _ := on["local"].(string)
			lid, _ := on["id"].(string)
			var base string
			switch list {
			case "milestones":
				base = place(lid)
			case "deliverables", "conditions":
				base = localMonth(list, lid)
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
					from[mid] = lid
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
		month[mid] = out
		state[mid] = 2
		return out
	}
	for _, mid := range order {
		place(mid)
	}
	// The chain that decides the last date: from the latest milestone back
	// through whatever set each month.
	critical := map[string]bool{}
	last := ""
	for _, mid := range order {
		if month[mid] > month[last] || last == "" {
			last = mid
		}
	}
	for at, n := last, 0; at != "" && n <= len(order); at, n = from[at], n+1 {
		critical[at] = true
	}
	if len(order) < 2 {
		critical = map[string]bool{}
	}
	var out []ScheduleItem
	for _, mid := range order {
		m := byID[mid]
		t := readTiming(m["timing"])
		tm, _ := m["timing"].(map[string]any)
		nb, _ := tm["notBefore"].(string)
		na, _ := tm["notAfter"].(string)
		name, _ := m["name"].(string)
		out = append(out, ScheduleItem{
			ID: mid, Name: name, Form: t.Form, Month: month[mid], NotBefore: trimMonth(nb), NotAfter: trimMonth(na),
			WaitsOn: chain.Waits[mid], Pending: t.Form == "when", Late: t.Late, Critical: critical[mid],
			Unplaced: month[mid] == "",
		})
	}
	return out, nil
}

func trimMonth(s string) string {
	if len(s) >= 7 {
		return s[:7]
	}
	return s
}
