// Package reportingcycle holds the ReportingCycle kind's rules and the one
// derivation every reader of a cycle must agree on: which periods it has.
// Periods are derived, never stored (TAXONOMY.md D8); named periods
// (TAXONOMY.md D40) are listed by the month each ends, so readings, filed
// by the month their period ends, need no change.
package reportingcycle

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Period is one period of a cycle.
type Period struct {
	// End is the month the period ends, as YYYY-MM: the key a reading is
	// filed under.
	End string
	// Start is the month it starts, as YYYY-MM.
	Start string
	// Name is the period's own name, for a cycle of named periods.
	Name string
	// Label is what a person reads: the name with its year ("Term I
	// 2026/27") for a yearly named period, the name alone for a dated one,
	// and empty for a regular cycle, whose periods are named by month.
	Label string
	// Due is the day its reading is due: the period's last day plus the
	// cycle's dueOffsetDays, as YYYY-MM-DD.
	Due string
}

// maxPeriods bounds one derivation, so a range a caller makes up cannot
// make the walk run long. A monthly cycle over a century is 1,200.
const maxPeriods = 1200

// Between returns the cycle's periods that overlap the months from and to
// (YYYY-MM, inclusive), in order. A regular cycle's periods line up with
// its own start month rather than with the range, so the same cycle gives
// the same periods whatever is asked of it.
func Between(spec map[string]any, from, to string) ([]Period, error) {
	f, err := monthIndex(from)
	if err != nil {
		return nil, fmt.Errorf("from: %w", err)
	}
	t, err := monthIndex(to)
	if err != nil {
		return nil, fmt.Errorf("to: %w", err)
	}
	if t < f {
		return nil, nil
	}
	if t-f > maxPeriods {
		return nil, fmt.Errorf("a range of at most %d months", maxPeriods)
	}
	due := intOf(spec["dueOffsetDays"])
	var out []Period
	switch periods, _ := spec["periods"].([]any); {
	case len(periods) > 0 && isDated(periods):
		out = dated(periods, f, t)
	case len(periods) > 0:
		out = yearly(periods, f, t)
	default:
		out = regular(intOf(spec["periodMonths"]), intOf(spec["startMonth"]), f, t)
	}
	for i := range out {
		out[i].Due = dueOn(out[i].End, due)
	}
	return out, nil
}

// Ends reports whether month (YYYY-MM) is the end of one of the cycle's
// named periods. A regular cycle answers true for every month: its
// readings have never been held to its periods, and are not now.
func Ends(spec map[string]any, month string) bool {
	periods, _ := spec["periods"].([]any)
	if len(periods) == 0 {
		return true
	}
	if isDated(periods) {
		for _, p := range periods {
			if str(asMap(p)["end"]) == month {
				return true
			}
		}
		return false
	}
	i, err := monthIndex(month)
	if err != nil {
		return false
	}
	for _, p := range periods {
		if intOf(asMap(p)["endMonth"]) == i%12+1 {
			return true
		}
	}
	return false
}

func regular(step, startMonth, f, t int) []Period {
	if step < 1 {
		return nil
	}
	if startMonth < 1 || startMonth > 12 {
		startMonth = 1
	}
	anchor := startMonth - 1
	start := f
	for (start%12-anchor+12)%12 != 0 {
		start--
	}
	var out []Period
	for s := start; s <= t; s += step {
		if end := s + step - 1; end >= f {
			out = append(out, Period{End: month(end), Start: month(s)})
		}
	}
	return out
}

// yearly lays named periods out over each cycle year. The list runs in the
// order of the year, so the year starts the month after the last period
// ends: terms ending December, April and July make a year from August.
func yearly(periods []any, f, t int) []Period {
	ends := make([]int, len(periods))
	for i, p := range periods {
		ends[i] = intOf(asMap(p)["endMonth"]) - 1
	}
	yearStart := (ends[len(ends)-1] + 1) % 12
	var out []Period
	for y := f/12 - 1; y <= t/12+1; y++ {
		first := y*12 + yearStart
		prev := first - 1
		for i, p := range periods {
			end := first + (ends[i]-yearStart+12)%12
			if end >= f && prev+1 <= t {
				name := str(asMap(p)["name"])
				out = append(out, Period{End: month(end), Start: month(prev + 1), Name: name, Label: strings.TrimSpace(name + " " + yearLabel(y, yearStart))})
			}
			prev = end
		}
	}
	return out
}

func dated(periods []any, f, t int) []Period {
	type dp struct {
		end  int
		name string
	}
	var all []dp
	for _, p := range periods {
		m := asMap(p)
		if e, err := monthIndex(str(m["end"])); err == nil {
			all = append(all, dp{e, str(m["name"])})
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].end < all[j].end })
	var out []Period
	for i, p := range all {
		start := p.end
		if i > 0 {
			start = all[i-1].end + 1
		}
		if p.end >= f && start <= t {
			out = append(out, Period{End: month(p.end), Start: month(start), Name: p.name, Label: p.name})
		}
	}
	return out
}

// yearLabel names a cycle year: 2026 when it is the calendar year, 2026/27
// when it runs across two.
func yearLabel(y, yearStart int) string {
	if yearStart == 0 {
		return strconv.Itoa(y)
	}
	return fmt.Sprintf("%d/%02d", y, (y+1)%100)
}

func isDated(periods []any) bool {
	_, ok := asMap(periods[0])["end"]
	return ok
}

func dueOn(end string, offset int) string {
	i, err := monthIndex(end)
	if err != nil {
		return ""
	}
	last := time.Date(i/12, time.Month(i%12+2), 0, 0, 0, 0, 0, time.UTC)
	return last.AddDate(0, 0, offset).Format("2006-01-02")
}

func monthIndex(s string) (int, error) {
	y, m, ok := strings.Cut(s, "-")
	yi, err1 := strconv.Atoi(y)
	mi, err2 := strconv.Atoi(m)
	if !ok || err1 != nil || err2 != nil || len(y) != 4 || mi < 1 || mi > 12 {
		return 0, fmt.Errorf("%q is not a year and month (YYYY-MM)", s)
	}
	return yi*12 + mi - 1, nil
}

func month(i int) string { return fmt.Sprintf("%04d-%02d", i/12, i%12+1) }

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func intOf(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case uint64:
		return int(n)
	}
	return 0
}
