// Package conformance holds any semantic layer exporter to the port's
// promises: every KPI and data source in the layer is in what it writes,
// and the same layer always gives the same files.
package conformance

import (
	"bytes"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/semantic"
)

// Layer is a small layer with every metric type.
func Layer() semantic.Layer {
	return semantic.Layer{
		Models: []semantic.Model{{Name: "quality_checks", Source: "quality-checks", Ref: "fct_quality_checks", Entity: "check", Key: "check_id",
			TimeColumn: "checked_on", Grain: "day", Dimensions: []semantic.Dimension{{Name: "depot", Column: "depot_code"}},
			Measures: []semantic.Measure{{Name: "checks_passed", Agg: "sum", Expr: "passed"}, {Name: "checks_made", Agg: "count"}}}},
		Metrics: []semantic.Metric{
			{Name: "checks_passed", KPI: "checks-passed", Label: "Checks passed", Type: semantic.Simple, Measure: "checks_passed"},
			{Name: "checks_made", KPI: "checks-made", Label: "Checks made", Type: semantic.Cumulative, Measure: "checks_made", Window: "28 days"},
			{Name: "pass_rate", KPI: "pass-rate", Label: "Pass rate", Type: semantic.Ratio, Numerator: "checks_passed", Denominator: "checks_made", Filter: "{{ Dimension('check__depot') }} = 'north'"},
			{Name: "fail_count", KPI: "fail-count", Label: "Checks failed", Type: semantic.Derived, Expr: "checks_made - checks_passed", Uses: []string{"checks_made", "checks_passed"}},
		},
	}
}

// Run holds an exporter to the port.
func Run(t *testing.T, x semantic.Exporter) {
	t.Helper()
	if x.Format() == "" {
		t.Fatal("an exporter names its format")
	}
	files, err := x.Export(Layer())
	if err != nil {
		t.Fatal(err)
	}
	var all []byte
	for _, f := range files {
		if f.Path == "" {
			t.Error("a file has no path")
		}
		all = append(all, f.Content...)
	}
	for _, want := range []string{"quality_checks", "fct_quality_checks", "checks_passed", "checks_made", "pass_rate", "fail_count", "28 days", "depot"} {
		if !bytes.Contains(all, []byte(want)) {
			t.Errorf("the export lacks %q", want)
		}
	}
	again, err := x.Export(Layer())
	if err != nil {
		t.Fatal(err)
	}
	var second []byte
	for _, f := range again {
		second = append(second, f.Content...)
	}
	if !bytes.Equal(all, second) {
		t.Error("the same layer gave different files")
	}
}
