package timing_test

import (
	"strings"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/timing"
)

// Each form says the latest month it can fall in, and whether it says
// everything its form needs.
func TestATimingReadsInEachForm(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		in       map[string]any
		month    string
		complete bool
	}{
		{map[string]any{"form": "date", "date": "2026-09-04"}, "2026-09", true},
		{map[string]any{"form": "window", "notBefore": "2026-01", "notAfter": "2027-12"}, "2027-12", true},
		{map[string]any{"form": "after"}, "", false},
		{map[string]any{"form": "when", "event": map[string]any{"on": "x"}, "expectedBy": "2027-07"}, "2027-07", true},
	} {
		got := timing.Read(c.in)
		if got.Month != c.month || got.Complete != c.complete {
			t.Errorf("%v: %+v", c.in, got)
		}
	}
}

// A target set when an event happens is pending, held to the month it
// is expected by.
func TestAPendingTargetIsHeldToItsExpectedMonth(t *testing.T) {
	t.Parallel()
	tg := timing.ReadTarget(map[string]any{"setWhen": map[string]any{"form": "when", "event": map[string]any{"on": "x"}, "expectedBy": "2099-11"}})
	if !tg.Pending || tg.Month != "2099-11" || !strings.Contains(timing.Pending(tg), "November 2099") {
		t.Fatalf("%+v: %s", tg, timing.Pending(tg))
	}
}

// Milestones that wait on each other in a loop are found.
func TestAMilestoneLoopIsFound(t *testing.T) {
	t.Parallel()
	on := func(id string) map[string]any {
		return map[string]any{"on": map[string]any{"local": "milestones", "id": id}}
	}
	ch := timing.Milestones(map[string]any{"milestones": []any{
		map[string]any{"id": "m1", "name": "A", "waitsOn": []any{on("m2")}},
		map[string]any{"id": "m2", "name": "B", "waitsOn": []any{on("m1")}},
	}})
	if len(ch.Loop) < 2 {
		t.Fatalf("no loop found: %+v", ch)
	}
}
