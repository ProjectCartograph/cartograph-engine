package engine

import (
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

// A check left by way of another (an aim waiting on its KPI's figure)
// carries what the person was asked about that figure (docs/adr/0032).
func TestACheckLeftByWayOfAnotherCarriesWhatWasAsked(t *testing.T) {
	t.Parallel()
	waivers := []store.Waiver{
		{On: "KPI/k", Check: "kpi-target", Reason: "The board sets the target", Asked: "Asked the target; the board sets it"},
		{On: "Project/p", Check: "resources-funding", Reason: "No budget yet", Asked: "not available"},
	}
	if got := askedFor(waivers, "The board sets the target"); got != "Asked the target; the board sets it" {
		t.Errorf("by way of the KPI: %q", got)
	}
	if got := askedFor(waivers, "A reason nothing gave"); got != "" {
		t.Errorf("a reason nothing gave: %q", got)
	}
}
