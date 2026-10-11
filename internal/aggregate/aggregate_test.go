package aggregate

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// What reads as an aggregation says what it reads, the names it uses, and
// the measure a semantic layer builds from it.
func TestAnAggregationReadsAsWritten(t *testing.T) {
	for _, c := range []struct {
		text, reading, agg, expr string
		names                    []string
	}{
		{"COUNT(*)", "the number of entries", "count", "1", nil},
		{"count(*) where graded_at = 'intake'", "the number of entries, where graded_at is 'intake'", "count", "case when graded_at = 'intake' then 1 end", []string{"graded_at"}},
		{"SUM(weight_kg)", "the sum of weight_kg", "sum", "weight_kg", []string{"weight_kg"}},
		{"COUNT(DISTINCT member_id) WHERE season = 2026", "the number of distinct member_id, where season is 2026", "count_distinct", "case when season = 2026 then member_id end", []string{"member_id", "season"}},
		{"AVG(days_to_pay) WHERE paid_by <> 'cheque' AND days_to_pay >= 0", "the average days_to_pay, where paid_by is not 'cheque' and days_to_pay is at least 0", "average", "case when paid_by != 'cheque' and days_to_pay >= 0 then days_to_pay end", []string{"days_to_pay", "paid_by"}},
		{"SUM(weight_kg) WHERE grade IN ('first', 'second') OR (grade IS NULL AND NOT rejected = true)", "the sum of weight_kg, where grade is one of 'first', 'second' or (grade is empty and not rejected is true)", "sum", "case when grade in ('first', 'second') or (grade is null and not (rejected = true)) then weight_kg end", []string{"weight_kg", "grade", "rejected"}},
		{"MAX(temperature_c) WHERE recorded_at BETWEEN 6 AND 18", "the largest temperature_c, where recorded_at is between 6 and 18", "max", "case when recorded_at between 6 and 18 then temperature_c end", []string{"temperature_c", "recorded_at"}},
		{"MIN(weight_kg) WHERE depot NOT LIKE 'hill%'", "the smallest weight_kg, where depot does not match 'hill%'", "min", "case when depot not like 'hill%' then weight_kg end", []string{"weight_kg", "depot"}},
	} {
		a, err := Parse(c.text)
		if err != nil {
			t.Errorf("%s: %v", c.text, err)
			continue
		}
		if got := a.Reading(); got != c.reading {
			t.Errorf("%s reads %q, want %q", c.text, got, c.reading)
		}
		if agg, expr := a.Measure(); agg != c.agg || expr != c.expr {
			t.Errorf("%s: measure %s(%s), want %s(%s)", c.text, agg, expr, c.agg, c.expr)
		}
		if got := a.Names(); !reflect.DeepEqual(got, c.names) {
			t.Errorf("%s names %v, want %v", c.text, got, c.names)
		}
	}
}

// What is not an aggregation is refused, saying why and where: a table,
// a join, another table's field, a query.
func TestWhatIsNotAnAggregationIsRefused(t *testing.T) {
	for _, c := range []struct{ text, says string }{
		{"", "Write the aggregation"},
		{"SELECT COUNT(*) FROM deliveries", "SELECT is not part of a metric"},
		{"COUNT(*) FROM deliveries", "FROM is not part of a metric"},
		{"SUM(d.weight_kg)", "d.weight_kg reads another table"},
		{"COUNT(*) WHERE depot.region = 'north'", "depot.region reads another table"},
		{"COUNT(*) WHERE depot_id IN (SELECT id FROM depots)", "SELECT is not part of a metric"},
		{"COUNT(*) JOIN depots ON depot_id = id", "JOIN is not part of a metric"},
		{"SUM(*)", "SUM(*) has nothing to add up"},
		{"TOTAL(weight_kg)", "TOTAL is not an aggregate"},
		{"COUNT(*) WHERE grade = first", "put text in quotes, such as 'first'"},
		{"COUNT(*) WHERE grade = 'first", "not closed"},
		{"COUNT(*); DROP", "leave out the ;"},
		{"COUNT(*) WHERE", "It ends early"},
		{"SUM(weight_kg) GROUP BY depot", "GROUP is not part of a metric"},
	} {
		_, err := Parse(c.text)
		var p Problem
		if !errors.As(err, &p) {
			t.Errorf("%q: read as an aggregation, want refused", c.text)
			continue
		}
		if !strings.Contains(p.Message, c.says) {
			t.Errorf("%q: %q, want it to say %q", c.text, p.Message, c.says)
		}
	}
}
