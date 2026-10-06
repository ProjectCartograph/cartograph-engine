package api_test

import (
	"net/http"
	"testing"

	apigen "github.com/ProjectCartograph/cartograph-engine/v2/internal/api/gen"
)

// A cycle of named periods is laid out by the engine, labelled with each
// term's year and the day its reading is due (TAXONOMY.md D40).
func TestCyclePeriodsAreDerivedByTheEngine(t *testing.T) {
	_, base := newTestServer(t)
	y := "apiVersion: cartograph/v1\nkind: ReportingCycle\nmetadata:\n  id: termly\n  name: Termly\nspec:\n  dueOffsetDays: 14\n  periods:\n    - {name: Term I, endMonth: 12}\n    - {name: Term II, endMonth: 4}\n    - {name: Term III, endMonth: 7}\n"
	resp := doJSON(t, http.MethodPut, base+"/manifests/ReportingCycle/termly", apigen.WriteRequest{Yaml: &y, Reason: "seed"}, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("save the cycle: %d", resp.StatusCode)
	}
	resp = doJSON(t, http.MethodGet, base+"/manifests/ReportingCycle/termly/periods?from=2026-09&to=2027-07", nil, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("periods: %d", resp.StatusCode)
	}
	got := decode[[]apigen.CyclePeriod](t, resp)
	if len(got) != 3 || got[0].End != "2026-12" || *got[0].Label != "Term I 2026/27" || got[0].Due.String() != "2027-01-14" {
		t.Fatalf("periods: %+v", got)
	}
	if resp := doJSON(t, http.MethodGet, base+"/manifests/ReportingCycle/none/periods?from=2026-01&to=2026-12", nil, nil); resp.StatusCode != 404 {
		t.Fatalf("an unknown cycle: %d", resp.StatusCode)
	}
	if resp := doJSON(t, http.MethodGet, base+"/manifests/ReportingCycle/termly/periods?from=1000-01&to=2026-12", nil, nil); resp.StatusCode != 422 {
		t.Fatalf("a range too long: %d", resp.StatusCode)
	}
}
