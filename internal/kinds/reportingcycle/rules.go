package reportingcycle

import (
	"fmt"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/kinds/kit"
)

// Rules holds a cycle to one of its two forms: equal periods of
// periodMonths from startMonth, or named periods (TAXONOMY.md D40). A
// list of named periods is all yearly ({name, endMonth}) or all dated
// ({name, end}), each name used once, and yearly ones run in the order of
// the year, so no two overlap and the year's start can be read off the
// last.
func Rules(doc map[string]any, _ kit.RuleContext) []kit.Problem {
	spec, _ := doc["spec"].(map[string]any)
	if spec == nil {
		return nil
	}
	periods, _ := spec["periods"].([]any)
	_, regular := spec["periodMonths"]
	var problems []kit.Problem
	add := func(path, format string, args ...any) {
		problems = append(problems, kit.Problem{Path: path, Message: fmt.Sprintf(format, args...)})
	}
	switch {
	case len(periods) > 0 && regular:
		add("/spec/periods", "a cycle has equal periods (periodMonths) or named periods, not both")
		return problems
	case len(periods) == 0 && !regular:
		add("/spec", "a cycle needs periodMonths for equal periods, or a list of named periods")
		return problems
	case regular:
		if _, ok := spec["startMonth"]; !ok {
			add("/spec/startMonth", "a cycle of equal periods needs the month its year starts")
		}
		return problems
	}

	dated := isDated(periods)
	names := map[string]int{}
	lastOffset := -1
	yearStart := 0
	if !dated {
		yearStart = intOf(asMap(periods[len(periods)-1])["endMonth"]) % 12
	}
	for i, p := range periods {
		m := asMap(p)
		path := fmt.Sprintf("/spec/periods/%d", i)
		if _, isDatedItem := m["end"]; isDatedItem != dated {
			add(path, "every named period is yearly (endMonth) or every one is dated (end), not a mix")
			continue
		}
		name := str(m["name"])
		if first, dup := names[name]; dup {
			add(path+"/name", "%q names period %d already; each name is used once", name, first+1)
		} else {
			names[name] = i
		}
		if dated {
			continue
		}
		offset := (intOf(m["endMonth"]) - 1 - yearStart + 12) % 12
		if offset <= lastOffset {
			add(path+"/endMonth", "list the periods in the order the year runs: this one ends before the one above it")
		}
		lastOffset = offset
	}
	if dated {
		ends := map[string]int{}
		for i, p := range periods {
			end := str(asMap(p)["end"])
			if first, dup := ends[end]; dup {
				add(fmt.Sprintf("/spec/periods/%d/end", i), "period %d already ends in %s; one period per month", first+1, end)
			} else {
				ends[end] = i
			}
		}
	}
	return problems
}
