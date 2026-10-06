package api_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	apigen "github.com/ProjectCartograph/cartograph-engine/v2/internal/api/gen"
)

// A workspace whose policy requires change sets refuses every direct
// write with 409, naming change sets as the way (docs/adr/0024); one
// without the policy writes as before.
func TestAWorkspaceCanRequireChangeSets(t *testing.T) {
	t.Parallel()
	_, base := newTestServer(t)
	commitTeam(t, base, "t1", "anyone")
	settings := "apiVersion: cartograph/v1\nkind: Settings\nmetadata:\n  id: default\n  name: Settings\nspec:\n  changeControl:\n    changeSetsRequired: true\n"
	resp := doJSON(t, http.MethodPut, base+"/manifests/Settings/default", apigen.WriteRequest{Yaml: &settings, Reason: "turn on change control"}, nil)
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("set the policy: %d %s", resp.StatusCode, b)
	}
	resp.Body.Close()

	team := "apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: t2\n  name: Team Two\nspec:\n  description: Team\n"
	for _, w := range []struct {
		name, method, path string
		body               any
	}{
		{"a version", http.MethodPut, "/manifests/Team/t2", apigen.WriteRequest{Yaml: &team, Reason: "direct"}},
		{"a working copy", http.MethodPut, "/manifests/Team/t2/working", map[string]string{"yaml": team}},
		{"a snapshot", http.MethodPost, "/manifests/Team/t1/snapshots", map[string]string{"reason": "direct"}},
		{"a delete", http.MethodDelete, "/manifests/Team/t1", map[string]string{"reason": "direct"}},
	} {
		resp := doJSON(t, w.method, base+w.path, w.body, nil)
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusConflict || !strings.Contains(string(b), "change set") {
			t.Errorf("%s went straight to the record: %d %s", w.name, resp.StatusCode, b)
		}
	}
}
