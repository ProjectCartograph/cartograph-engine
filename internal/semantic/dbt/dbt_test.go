package dbt_test

import (
	"strings"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/semantic/conformance"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/semantic/dbt"
)

func TestConformance(t *testing.T) {
	t.Parallel()
	conformance.Run(t, dbt.New())
}

// The file is dbt's own shape: a semantic model on ref(), its measures,
// and each metric type with its type_params.
func TestTheFileIsDbts(t *testing.T) {
	t.Parallel()
	files, err := dbt.New().Export(conformance.Layer())
	if err != nil {
		t.Fatal(err)
	}
	got := string(files[0].Content)
	for _, want := range []string{
		"semantic_models:\n  - name: quality_checks\n",
		"model: ref('fct_quality_checks')",
		"agg_time_dimension: checked_on",
		"type: primary\n        expr: check_id",
		"time_granularity: day",
		"expr: depot_code",
		"- name: checks_made\n        agg: sum\n        expr: \"1\"",
		"type: ratio\n    type_params:\n      numerator: checks_passed\n      denominator: checks_made",
		"window: 28 days",
		"metrics:\n        - name: checks_made",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("lacks %q in:\n%s", want, got)
		}
	}
}
