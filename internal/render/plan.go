package render

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// The plan parts of a charter (TAXONOMY.md D47 to D53): timings in words,
// milestones and what each waits on, the deliverable register,
// responsibilities, costs, procurement, conditions, sign-off and what has
// happened. Each prints only when the definition has it.

// plan reads one project's own lists, so a local reference prints a name.
type plan struct {
	n    names
	spec map[string]any
	// events by the local item they happened to, newest last.
	events map[string][]map[string]any
}

func newPlan(n names, spec map[string]any) plan {
	p := plan{n: n, spec: spec, events: map[string][]map[string]any{}}
	evs := list(spec["events"])
	sort.SliceStable(evs, func(i, j int) bool { return str(evs[i]["date"]) < str(evs[j]["date"]) })
	for _, ev := range evs {
		on := obj(ev["on"])
		key := str(on["local"]) + "/" + str(on["id"])
		if str(on["local"]) == "" {
			key = "ext/" + str(on["external"]) + str(on["kind"]) + str(on["id"])
		}
		p.events[key] = append(p.events[key], ev)
	}
	return p
}

// when prints "2026-09-02" as "2 September 2026" and "2026-09" as
// "September 2026".
func when(s string) string {
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.Format("2 January 2006")
	}
	return month(s)
}

// local names an item of this project's own lists.
func (p plan) local(listName, id string) string {
	for _, it := range list(p.spec[listName]) {
		if str(it["id"]) == id {
			for _, k := range []string{"name", "statement", "action", "label", "description", "category"} {
				if s := str(it[k]); s != "" {
					return s
				}
			}
		}
	}
	return id
}

// item names whatever a reference points at.
func (p plan) item(v any) string {
	r := obj(v)
	if ext := str(r["external"]); ext != "" {
		return ext
	}
	if l := str(r["local"]); l != "" {
		if l == "resources" {
			return p.n.ref(v, "Resource", p.spec)
		}
		return p.local(l, str(r["id"]))
	}
	return p.n.ref(v, "", p.spec)
}

var happensWords = map[string]string{
	"reached": "is reached", "accepted": "is accepted", "met": "is met", "firstReading": "has its first reading",
	"issued": "is issued", "decided": "is decided", "approved": "is approved", "closed": "closes",
	"landed": "is handed over", "occurred": "happens",
}

// event says an event in words. A milestone is named by its number and
// name, which already say it is a point reached ("M10 Technical Handbook
// content finalised"); a deliverable is accepted and a condition met.
func (p plan) event(v any) string {
	ev := obj(v)
	on := obj(ev["on"])
	what := p.item(ev["on"])
	if what == "" {
		return ""
	}
	if l := str(on["local"]); l == "milestones" || l == "deliverables" {
		prefix := map[string]string{"milestones": "M", "deliverables": "D"}[l]
		for i, it := range list(p.spec[l]) {
			if str(it["id"]) == str(on["id"]) {
				what = itemNumber(prefix, str(it["id"]), i) + " " + what
			}
		}
	}
	verb := happensWords[str(ev["happens"])]
	if verb == "" {
		switch str(on["local"]) {
		case "milestones":
			verb = ""
		case "deliverables":
			verb = "is accepted"
		case "conditions":
			verb = "is met"
		case "":
			verb = ""
		default:
			verb = "happens"
		}
	}
	if verb == "" {
		return what
	}
	return what + " " + verb
}

// timing says a timing in words.
func (p plan) timing(v any) string {
	t := obj(v)
	var out string
	switch str(t["form"]) {
	case "date":
		out = when(str(t["date"]))
	case "window":
		from, to := when(str(t["notBefore"])), when(str(t["notAfter"]))
		switch {
		case from != "" && to != "":
			out = span2(str(t["notBefore"]), str(t["notAfter"]))
		case from != "":
			out = "Not before " + from
		default:
			out = "By " + to
		}
	case "after":
		lag := ""
		if m := number(t["lagMonths"]); m != "" && m != "0" {
			lag = m + " month" + map[bool]string{true: "", false: "s"}[m == "1"] + " after "
		} else if dd := number(t["lagDays"]); dd != "" && dd != "0" {
			lag = dd + " day" + map[bool]string{true: "", false: "s"}[dd == "1"] + " after "
		} else {
			lag = "After "
		}
		out = capital(lag + p.event(t["event"]))
	case "when":
		out = "When " + p.event(t["event"])
		if e := when(str(t["expectedBy"])); e != "" {
			out += ", expected by " + e
		}
		if by := p.who(t["decidedBy"]); by != "" {
			out += "; set by " + by
		}
	}
	if note := str(t["note"]); note != "" && out != "" {
		out += " (" + note + ")"
	}
	return out
}

// target says a target in any of its shapes, with the measure's unit.
func (p plan) target(v any, unit func(string) string) string {
	t := obj(v)
	if sw, ok := t["setWhen"]; ok {
		s := "Set " + strings.TrimPrefix(lowerFirst(p.timing(sw)), "")
		if dir := str(t["direction"]); dir != "" {
			s = capital(dir) + ". " + s
		}
		return s
	}
	val := number(t["value"])
	if val == "" {
		return ""
	}
	val = unit(val)
	if due, ok := t["due"]; ok {
		return val + ", " + lowerFirst(p.timing(due))
	}
	if m := month(str(t["date"])); m != "" {
		val += ", " + m
	}
	return val
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	if len(r) > 1 && r[1] >= 'a' && r[1] <= 'z' {
		r[0] = []rune(strings.ToLower(string(r[0])))[0]
	}
	return string(r)
}

// status is what last happened to a local item, in words: "Reached, 31
// December 2025".
func (p plan) status(listName, id string) string {
	evs := p.events[listName+"/"+id]
	if len(evs) == 0 {
		return ""
	}
	last := evs[len(evs)-1]
	return label("happened", str(last["happened"])) + ", " + when(str(last["date"]))
}

func init() {
	labels["happened"] = map[string]string{
		"reached": "Reached", "slipped": "Slipped", "accepted": "Accepted", "rejected": "Rejected",
		"met": "Met", "notMet": "Not met", "occurred": "Occurred", "issued": "Issued",
		"decided": "Decided", "signed": "Signed",
	}
	labels["costStatus"] = map[string]string{
		"approved": "Approved", "requested": "Requested", "beingCosted": "Being costed", "unfunded": "Not funded",
	}
	labels["stage"] = map[string]string{"definition": "Definition", "closing": "Closure", "handover": "Handover"}
	labels["decision"] = map[string]string{"approve": "Approved", "approveWithConditions": "Approved with conditions", "reject": "Not approved"}
}

func anyList(v any) []any {
	l, _ := v.([]any)
	return l
}

// thousands groups digits: 171600 reads 171,600.
func thousands(s string) string {
	whole, frac, _ := strings.Cut(s, ".")
	neg := strings.HasPrefix(whole, "-")
	whole = strings.TrimPrefix(whole, "-")
	var b strings.Builder
	for i, r := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	out := b.String()
	if frac != "" {
		out += "." + frac
	}
	if neg {
		out = "-" + out
	}
	return out
}

// itemNumber is the number a charter gives an item: its own id when that
// is already the organisation's number (m10, d4, ws3), else its place.
func itemNumber(prefix, id string, i int) string {
	low := strings.ToLower(prefix)
	if rest, ok := strings.CutPrefix(strings.ToLower(id), low); ok && rest != "" && rest[0] >= '0' && rest[0] <= '9' && len(rest) <= 4 {
		return prefix + strings.ToUpper(rest)
	}
	return fmt.Sprintf("%s%d", prefix, i+1)
}

// span2 prints a window as people write it: "25 to 26 February 2026",
// "April to July 2027", or in full when the years differ.
func span2(a, b string) string {
	ta, ea := time.Parse("2006-01-02", a)
	tb, eb := time.Parse("2006-01-02", b)
	if ea == nil && eb == nil {
		switch {
		case ta.Year() == tb.Year() && ta.Month() == tb.Month():
			return fmt.Sprintf("%d to %s", ta.Day(), tb.Format("2 January 2006"))
		case ta.Year() == tb.Year():
			return ta.Format("2 January") + " to " + tb.Format("2 January 2006")
		}
		return ta.Format("2 January 2006") + " to " + tb.Format("2 January 2006")
	}
	ma, ea := time.Parse("2006-01", a)
	mb, eb := time.Parse("2006-01", b)
	if ea == nil && eb == nil && ma.Year() == mb.Year() {
		return ma.Format("January") + " to " + mb.Format("January 2006")
	}
	return when(a) + " to " + when(b)
}

// result is a success criterion's latest result, in words: "Met, 412,
// March 2027 (the depot audit)": what was judged, the value measured, when,
// and the evidence (TAXONOMY.md D52).
func (p plan) result(id string) string {
	evs := p.events["successCriteria/"+id]
	for i := len(evs) - 1; i >= 0; i-- {
		e := evs[i]
		h := str(e["happened"])
		if h != "met" && h != "notMet" {
			continue
		}
		out := label("happened", h)
		if v := number(e["value"]); v != "" {
			out += ", " + thousands(v)
		}
		if d := when(str(e["date"])); d != "" {
			out += ", " + d
		}
		if ev := str(e["evidence"]); ev != "" {
			out += " (" + ev + ")"
		}
		return out
	}
	return ""
}
