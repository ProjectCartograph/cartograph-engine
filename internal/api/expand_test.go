package api_test

import (
	"net/http"
	"testing"

	apigen "github.com/ProjectCartograph/cartograph-engine/v2/internal/api/gen"
)

// expand=spec carries each record's spec on its summary, so a register
// can summarise every row from one request; without it the summary stays
// as it was.
func TestListManifestsExpandSpec(t *testing.T) {
	_, base := newTestServer(t)
	commitTeam(t, base, "t1", "anyone")

	plain := decode[[]apigen.Summary](t, doJSON(t, http.MethodGet, base+"/manifests/Team", nil, nil))
	if len(plain) != 1 || plain[0].Spec != nil {
		t.Fatalf("plain list = %+v, want one summary without a spec", plain)
	}
	expanded := decode[[]apigen.Summary](t, doJSON(t, http.MethodGet, base+"/manifests/Team?expand=spec", nil, nil))
	if len(expanded) != 1 || expanded[0].Spec == nil || (*expanded[0].Spec)["description"] != "Team" {
		t.Fatalf("expanded list = %+v, want the spec carried", expanded)
	}
}
