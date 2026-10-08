// Package timing reads when things fall (TAXONOMY.md D47, D48): a
// timing in its four forms, a target in its shapes, and a project's
// milestones as a chain of what waits on what. It is pure: the current
// month is the only thing it asks of the world.
package timing

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/document"
)

// now is the clock a passed expected month is read against.
var now = time.Now

// ThisMonth is the current month as "YYYY-MM".
func ThisMonth() string { return now().Format("2006-01") }

// Timing is a timing read for the checks (TAXONOMY.md D47).
type Timing struct {
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

// Read reads a timing in any of its four forms.
func Read(v any) Timing {
	t, _ := v.(map[string]any)
	if t == nil {
		return Timing{}
	}
	var out Timing
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
		out.Late = out.Month != "" && out.Month < ThisMonth()
	}
	return out
}

// Target is a target in any of its shapes (TAXONOMY.md D47).
type Target struct {
	Value    float64
	HasValue bool
	// Month is the month it is due by, or for one set when an event
	// happens the month that is expected by; empty for one that follows
	// an event with no date of its own.
	Month string
	// Pending is a target set when an event happens.
	Pending bool
	Timing  Timing
	// Set is a target defined in any form; Dated is one that says when.
	Set, Dated bool
}

// ReadTarget reads a target in any of its shapes.
func ReadTarget(v any) Target {
	t, _ := v.(map[string]any)
	if t == nil {
		return Target{}
	}
	var out Target
	out.Value, out.HasValue = document.Number(t["value"])
	if _, has := t["value"]; has {
		out.Set = true
	}
	if d, _ := t["date"].(string); len(d) >= 7 {
		out.Month = d[:7]
		out.Dated = true
	}
	if due, ok := t["due"]; ok {
		out.Timing = Read(due)
		out.Month = out.Timing.Month
		out.Dated = out.Timing.Complete
	}
	if sw, ok := t["setWhen"]; ok {
		out.Pending = true
		out.Timing = Read(sw)
		// It is set by the month it is expected by, which is the month a
		// horizon is held to.
		out.Month = out.Timing.Month
		out.Set = out.Timing.Complete
		out.Dated = out.Timing.Complete
	}
	return out
}

// Pending says when a target that waits to be set is set, for a
// check's message.
func Pending(t Target) string {
	if t.Timing.Late {
		return fmt.Sprintf("was to be set by %s, which has passed: set it, or move the month it is expected by", MonthWords(t.Timing.Month))
	}
	return fmt.Sprintf("is set when its event happens, expected by %s", MonthWords(t.Timing.Month))
}

// MonthWords prints "2026-11" as "November 2026".
func MonthWords(m string) string {
	if t, err := time.Parse("2006-01", m); err == nil {
		return t.Format("January 2006")
	}
	return m
}

// Chain resolves a project's milestones (TAXONOMY.md D48): which
// waits on which (an after or set-when on another milestone of the project,
// or a waitsOn line), any loop, and the longest chain by the order of their
// months.
type Chain struct {
	Names map[string]string
	Waits map[string][]string
	Loop  []string
}

// Milestones reads a project's milestones as a chain.
func Milestones(spec map[string]any) Chain {
	ch := Chain{Names: map[string]string{}, Waits: map[string][]string{}}
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
		if t := Read(m["timing"]); t.Event != nil {
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

// TrimMonth is a date cut to its month, YYYY-MM.
func TrimMonth(s string) string {
	if len(s) >= 7 {
		return s[:7]
	}
	return s
}
