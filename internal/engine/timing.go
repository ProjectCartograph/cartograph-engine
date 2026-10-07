package engine

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// now is the clock the checks read a passed expected month against. Tests
// set it; nothing else does.
var now = time.Now

// thisMonth is the current month as "YYYY-MM".
func thisMonth() string { return now().Format("2006-01") }

// timingOf is a timing read for the checks (TAXONOMY.md D47).
type timingOf struct {
	Form string
	// Month is the latest month it can fall in, where it says one: the
	// date, a window's end (else its start), or the month a set-when is
	// expected by. Empty for after an event with no date of its own.
	Month string
	// Event is the event an after or a set-when waits on.
	Event map[string]any
	// Late is a set-when whose expected month has passed.
	Late bool
	// Complete is a timing that says everything its form needs.
	Complete bool
}

func readTiming(v any) timingOf {
	t, _ := v.(map[string]any)
	if t == nil {
		return timingOf{}
	}
	var out timingOf
	out.Form, _ = t["form"].(string)
	out.Event, _ = t["event"].(map[string]any)
	m := func(k string) string {
		s, _ := t[k].(string)
		if len(s) >= 7 {
			return s[:7]
		}
		return ""
	}
	switch out.Form {
	case "date":
		out.Month = m("date")
		out.Complete = out.Month != ""
	case "window":
		out.Month = m("notAfter")
		if out.Month == "" {
			out.Month = m("notBefore")
		}
		out.Complete = out.Month != ""
	case "after":
		out.Complete = out.Event != nil
	case "when":
		out.Month = m("expectedBy")
		out.Complete = out.Event != nil && out.Month != ""
		out.Late = out.Month != "" && out.Month < thisMonth()
	}
	return out
}

// targetOf is a target in any of its shapes (TAXONOMY.md D47).
type targetOf struct {
	Value    float64
	HasValue bool
	// Month is the month it is due by; empty for one that follows an
	// event or waits to be set.
	Month string
	// Pending is a target set when an event happens.
	Pending bool
	Timing  timingOf
	// Set is a target defined in any form; Dated is one that says when.
	Set, Dated bool
}

func readTarget(v any) targetOf {
	t, _ := v.(map[string]any)
	if t == nil {
		return targetOf{}
	}
	var out targetOf
	out.Value, out.HasValue = toFloat(t["value"])
	if _, has := t["value"]; has {
		out.Set = true
	}
	if d, _ := t["date"].(string); len(d) >= 7 {
		out.Month = d[:7]
		out.Dated = true
	}
	if due, ok := t["due"]; ok {
		out.Timing = readTiming(due)
		out.Month = out.Timing.Month
		out.Dated = out.Timing.Complete
	}
	if sw, ok := t["setWhen"]; ok {
		out.Pending = true
		out.Timing = readTiming(sw)
		out.Set = out.Timing.Complete
		out.Dated = out.Timing.Complete
	}
	return out
}

// pendingWords says when a target that waits to be set is set, for a
// check's message.
func pendingWords(t targetOf) string {
	if t.Timing.Late {
		return fmt.Sprintf("was to be set by %s, which has passed: set it, or move the month it is expected by", monthWords(t.Timing.Month))
	}
	return fmt.Sprintf("is set when its event happens, expected by %s", monthWords(t.Timing.Month))
}

// monthWords prints "2026-11" as "November 2026".
func monthWords(m string) string {
	if t, err := time.Parse("2006-01", m); err == nil {
		return t.Format("January 2006")
	}
	return m
}

// milestoneChain resolves a project's milestones (TAXONOMY.md D48): which
// waits on which (an after or set-when on another milestone of the project,
// or a waitsOn line), any loop, and the longest chain by the order of their
// months.
type milestoneChain struct {
	Names map[string]string
	Waits map[string][]string
	Loop  []string
}

func readMilestones(spec map[string]any) milestoneChain {
	ch := milestoneChain{Names: map[string]string{}, Waits: map[string][]string{}}
	list, _ := spec["milestones"].([]any)
	onMilestone := func(ev map[string]any) string {
		on, _ := ev["on"].(map[string]any)
		if l, _ := on["local"].(string); l == "milestones" {
			id, _ := on["id"].(string)
			return id
		}
		return ""
	}
	for _, it := range list {
		m, _ := it.(map[string]any)
		id, _ := m["id"].(string)
		if id == "" {
			continue
		}
		name, _ := m["name"].(string)
		ch.Names[id] = name
		if t := readTiming(m["timing"]); t.Event != nil {
			if to := onMilestone(t.Event); to != "" {
				ch.Waits[id] = append(ch.Waits[id], to)
			}
		}
		ws, _ := m["waitsOn"].([]any)
		for _, w := range ws {
			if ev, ok := w.(map[string]any); ok {
				if to := onMilestone(ev); to != "" && !strings.EqualFold(to, id) {
					ch.Waits[id] = append(ch.Waits[id], to)
				}
			}
		}
	}
	// A loop: walk from each milestone along what it waits on.
	ids := make([]string, 0, len(ch.Names))
	for id := range ch.Names {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, start := range ids {
		path := []string{start}
		seen := map[string]bool{}
		var walk func(at string) bool
		walk = func(at string) bool {
			for _, n := range ch.Waits[at] {
				if n == start {
					path = append(path, n)
					return true
				}
				if seen[n] {
					continue
				}
				seen[n] = true
				path = append(path, n)
				if walk(n) {
					return true
				}
				path = path[:len(path)-1]
			}
			return false
		}
		if walk(start) {
			ch.Loop = path
			break
		}
	}
	return ch
}

// stampEvents records who entered each of a project's events, as a version
// records its author (TAXONOMY.md D52): an event the current version
// already holds keeps its recorder, and a new one takes the actor saving
// it, whatever it says. It returns the document re-encoded when it changed
// anything, and nil when it did not.
func (e *Engine) stampEvents(ctx context.Context, id string, doc map[string]any, actor string) ([]byte, error) {
	spec, _ := doc["spec"].(map[string]any)
	events, _ := spec["events"].([]any)
	if len(events) == 0 {
		return nil, nil
	}
	had := map[string]string{}
	if prev, err := e.loadProjectDoc(ctx, id); err == nil {
		ps, _ := prev["spec"].(map[string]any)
		pe, _ := ps["events"].([]any)
		for _, it := range pe {
			m, _ := it.(map[string]any)
			eid, _ := m["id"].(string)
			by, _ := m["recordedBy"].(string)
			had[eid] = by
		}
	}
	changed := false
	for _, it := range events {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		eid, _ := m["id"].(string)
		want, known := had[eid]
		if !known || want == "" {
			want = actor
		}
		if by, _ := m["recordedBy"].(string); by != want {
			m["recordedBy"] = want
			changed = true
		}
	}
	if !changed {
		return nil, nil
	}
	return e.codec.Encode(doc)
}
