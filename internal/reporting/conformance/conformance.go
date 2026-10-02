// Package conformance holds a reporting adapter to another's rows over
// the same record: every standard report, column for column, row for
// row, in any order.
package conformance

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/reporting"
)

// Run checks that got answers every standard report as want does, and
// refuses a report it does not know.
func Run(t *testing.T, got, want reporting.Reporter) {
	t.Helper()
	ctx := context.Background()
	for _, name := range reporting.Standard {
		w, err := want.Run(ctx, name)
		if err != nil {
			t.Fatalf("%s, reference: %v", name, err)
		}
		g, err := got.Run(ctx, name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !reflect.DeepEqual(g.Columns, w.Columns) {
			t.Errorf("%s columns %v, want %v", name, g.Columns, w.Columns)
		}
		gt, wt := text(g), text(w)
		if len(wt) == 0 {
			t.Errorf("%s: the reference has no rows to compare", name)
		}
		if !reflect.DeepEqual(gt, wt) {
			t.Errorf("%s:\ngot  %v\nwant %v", name, gt, wt)
		}
	}
	if _, err := got.Run(ctx, "no-such-report"); err == nil {
		t.Error("an unknown report was answered")
	}
}

func text(t reporting.Table) []string {
	out := make([]string, len(t.Rows))
	for i, r := range t.Rows {
		parts := make([]string, len(r))
		for j, v := range r {
			if v != nil {
				parts[j] = fmt.Sprint(v)
			}
		}
		out[i] = strings.Join(parts, "|")
	}
	sort.Strings(out)
	return out
}
