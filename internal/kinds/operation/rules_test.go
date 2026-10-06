package operation

import (
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/kinds/kit"
)

func TestOneFundingLinePerCurrencyAndPeriod(t *testing.T) {
	line := func(cur, per string) map[string]any {
		return map[string]any{"amount": 10, "currency": cur, "per": per, "status": "approved"}
	}
	doc := map[string]any{"spec": map[string]any{"funding": []any{
		line("TTD", "year"), line("TTD", "month"), line("USD", "year"), line("TTD", "year"),
	}}}
	problems := Rules(doc, kit.RuleContext{})
	if len(problems) != 1 || problems[0].Path != "/spec/funding/3/currency" {
		t.Fatalf("want one problem at the fourth line, got %+v", problems)
	}
}
