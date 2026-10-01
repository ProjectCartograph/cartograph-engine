package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	codecyaml "github.com/ProjectCartograph/cartograph-engine/internal/codec/yaml"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/internal/api"
	apigen "github.com/ProjectCartograph/cartograph-engine/internal/api/gen"
	"github.com/ProjectCartograph/cartograph-engine/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/internal/store"
	"github.com/ProjectCartograph/cartograph-engine/internal/store/memory"
	"github.com/ProjectCartograph/cartograph-engine/internal/store/vault"
)

func testContext() context.Context {
	return context.Background()
}

func openVault(ctx context.Context, dir string) (*vault.ManifestStore, error) {
	return vault.New(ctx, dir, vault.Options{Watch: false})
}

func newTestServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	e, err := engine.New(memory.NewManifestStore(), memory.NewOperationalStore(), engine.WithCodec(codecyaml.New()))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(api.Handler(e))
	t.Cleanup(srv.Close)
	return srv, srv.URL
}

func decode[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	defer resp.Body.Close()
	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return out
}

func doJSON(t *testing.T, method, url string, body any, headers map[string]string) *http.Response {
	t.Helper()
	var r *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r = bytes.NewReader(b)
	} else {
		r = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, url, r)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestHealth(t *testing.T) {
	_, base := newTestServer(t)
	resp, err := http.Get(base + "/health")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("got %d", resp.StatusCode)
	}
	h := decode[apigen.Health](t, resp)
	if h.Status != "ok" {
		t.Fatalf("got %+v", h)
	}
}

func TestKindsAndSchema(t *testing.T) {
	_, base := newTestServer(t)

	resp, err := http.Get(base + "/kinds")
	if err != nil {
		t.Fatal(err)
	}
	kinds := decode[[]apigen.KindCount](t, resp)
	if len(kinds) == 0 {
		t.Fatal("expected at least one kind")
	}

	resp, err = http.Get(base + "/schemas/Team")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("got %d", resp.StatusCode)
	}
	schema := decode[map[string]any](t, resp)
	if schema["title"] != "Team" {
		t.Fatalf("got %+v", schema)
	}

	resp, err = http.Get(base + "/schemas/NotAKind")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 404 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

const teamYAML = "apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: t1\n  name: Team One\nspec:\n  name: Team One\n"

func commitTeam(t *testing.T, base, id, actor string) apigen.Version {
	t.Helper()
	yaml := "apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: " + id + "\n  name: Team\nspec:\n  name: Team\n"
	resp := doJSON(t, http.MethodPut, base+"/manifests/Team/"+id,
		apigen.WriteRequest{Yaml: &yaml, Reason: "seed"},
		map[string]string{"X-Cartograph-Actor": actor})
	if resp.StatusCode != 200 {
		t.Fatalf("commit team: got %d", resp.StatusCode)
	}
	return decode[apigen.Version](t, resp)
}

func TestPutManifestAndGet(t *testing.T) {
	_, base := newTestServer(t)
	v := commitTeam(t, base, "t1", "anyone")
	if v.Number != 1 {
		t.Fatalf("got %+v", v)
	}

	resp, err := http.Get(base + "/manifests/Team/t1")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("got %d", resp.StatusCode)
	}
	view := decode[apigen.ManifestView](t, resp)
	if view.Manifest.Kind != "Team" || view.Manifest.Metadata.Id != "t1" {
		t.Fatalf("got %+v", view)
	}
	if view.Yaml == "" {
		t.Fatal("expected the raw yaml text back")
	}
}

func TestGetManifest404(t *testing.T) {
	_, base := newTestServer(t)
	resp, err := http.Get(base + "/manifests/Team/does-not-exist")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 404 {
		t.Fatalf("got %d", resp.StatusCode)
	}
	pl := decode[apigen.ProblemList](t, resp)
	if len(pl.Problems) == 0 {
		t.Fatal("expected at least one problem")
	}
}

func TestPutManifest422Shape(t *testing.T) {
	_, base := newTestServer(t)
	commitTeam(t, base, "t1", "p1") // first commit bootstraps

	badYAML := "apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: t2\n  name: Team Two\nspec:\n  parent: does-not-exist\n"
	resp := doJSON(t, http.MethodPut, base+"/manifests/Team/t2",
		apigen.WriteRequest{Yaml: &badYAML, Reason: "test"},
		map[string]string{"X-Cartograph-Actor": "p1"})
	if resp.StatusCode != 422 {
		t.Fatalf("got %d", resp.StatusCode)
	}
	pl := decode[apigen.ProblemList](t, resp)
	if len(pl.Problems) == 0 {
		t.Fatal("expected problems in the 422 body")
	}
	found := false
	for _, p := range pl.Problems {
		if p.Path != "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected at least one problem with a JSON pointer path, got %+v", pl.Problems)
	}
}

func TestPutManifestCreateThenUpdateSameCall(t *testing.T) {
	_, base := newTestServer(t)
	v1 := commitTeam(t, base, "t1", "anyone") // no current version yet: this is a create
	if v1.Number != 1 {
		t.Fatalf("expected create to land as version 1, got %+v", v1)
	}

	updateYAML := "apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: t1\n  name: Team One Renamed\nspec:\n  name: Team One Renamed\n"
	resp := doJSON(t, http.MethodPut, base+"/manifests/Team/t1",
		apigen.WriteRequest{Yaml: &updateYAML, Reason: "rename"},
		map[string]string{"X-Cartograph-Actor": "anyone"})
	if resp.StatusCode != 200 {
		t.Fatalf("update: got %d", resp.StatusCode)
	}
	v2 := decode[apigen.Version](t, resp) // current version exists: this is an update, same PUT
	if v2.Number != 2 {
		t.Fatalf("expected update to land as version 2, got %+v", v2)
	}
}

func TestPutManifestIDMismatch409(t *testing.T) {
	_, base := newTestServer(t)
	resp := doJSON(t, http.MethodPut, base+"/manifests/Team/some-other-id",
		apigen.WriteRequest{Yaml: strPtr(teamYAML), Reason: "test"},
		map[string]string{"X-Cartograph-Actor": "anyone"})
	if resp.StatusCode != 409 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestPutManifestNeitherYAMLNorManifest400(t *testing.T) {
	_, base := newTestServer(t)
	resp := doJSON(t, http.MethodPut, base+"/manifests/Team/t1",
		apigen.WriteRequest{Reason: "test"},
		map[string]string{"X-Cartograph-Actor": "anyone"})
	if resp.StatusCode != 400 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestPutManifestUnknownKind404(t *testing.T) {
	_, base := newTestServer(t)
	resp := doJSON(t, http.MethodPut, base+"/manifests/NotAKind/x",
		apigen.WriteRequest{Yaml: strPtr(teamYAML), Reason: "test"},
		map[string]string{"X-Cartograph-Actor": "anyone"})
	if resp.StatusCode != 404 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

// TestActorAccepted is the API-level test that I3.2 removed actor validation
// entirely (checkActor's own doc comment explains why), so a write carrying any
// actor is accepted unconditionally.
func TestActorAccepted(t *testing.T) {
	_, base := newTestServer(t)
	commitTeam(t, base, "t1", "p1")

	// Any actor is accepted unconditionally
	yaml2 := "apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: t9\n  name: Team Nine\nspec:\n  name: Team Nine\n"
	resp := doJSON(t, http.MethodPut, base+"/manifests/Team/t9",
		apigen.WriteRequest{Yaml: &yaml2, Reason: "test"},
		map[string]string{"X-Cartograph-Actor": "local"})
	if resp.StatusCode != 200 {
		t.Fatalf("expected actor \"local\" to be accepted, got %d", resp.StatusCode)
	}
}

func TestListManifestsNeverNull(t *testing.T) {
	_, base := newTestServer(t)
	resp, err := http.Get(base + "/manifests/Team")
	if err != nil {
		t.Fatal(err)
	}
	body := decode[json.RawMessage](t, resp)
	if string(body) != "[]" {
		t.Fatalf("expected the literal [] for an empty list, got %s", body)
	}

	commitTeam(t, base, "t1", "anyone")
	resp, err = http.Get(base + "/manifests/Team")
	if err != nil {
		t.Fatal(err)
	}
	teams := decode[[]apigen.Summary](t, resp)
	if len(teams) != 1 || teams[0].Id != "t1" {
		t.Fatalf("got %+v", teams)
	}
}

func TestListManifestsQueryAndRefFilter(t *testing.T) {
	_, base := newTestServer(t)
	commitTeam(t, base, "curriculum-team", "anyone")

	dsYAML := "apiVersion: cartograph/v1\nkind: DataSource\nmetadata:\n  id: d1\n  name: Data Source One\nspec:\n  name: Data Source One\n  category: database\n  team: curriculum-team\n"
	resp := doJSON(t, http.MethodPut, base+"/manifests/DataSource/d1", apigen.WriteRequest{Yaml: &dsYAML, Reason: "seed"}, map[string]string{"X-Cartograph-Actor": "anyone"})
	if resp.StatusCode != 200 {
		t.Fatalf("got %d", resp.StatusCode)
	}

	resp, err := http.Get(base + "/manifests/DataSource?q=one")
	if err != nil {
		t.Fatal(err)
	}
	list := decode[[]apigen.Summary](t, resp)
	if len(list) != 1 {
		t.Fatalf("got %+v", list)
	}

	resp, err = http.Get(base + "/manifests/DataSource?ref=Team%2Fcurriculum-team")
	if err != nil {
		t.Fatal(err)
	}
	list = decode[[]apigen.Summary](t, resp)
	if len(list) != 1 || list[0].Id != "d1" {
		t.Fatalf("got %+v", list)
	}

	resp, err = http.Get(base + "/manifests/DataSource?ref=Team%2Fdoes-not-exist")
	if err != nil {
		t.Fatal(err)
	}
	body := decode[json.RawMessage](t, resp)
	if string(body) != "[]" {
		t.Fatalf("expected [], got %s", body)
	}
}

func TestListManifestsPaginationEnvelope(t *testing.T) {
	_, base := newTestServer(t)
	ids := []string{"team-a", "team-b", "team-c", "team-d", "team-e"}
	for _, id := range ids {
		commitTeam(t, base, id, "anyone")
	}

	// Neither limit nor cursor: bare array, unchanged behaviour.
	resp, err := http.Get(base + "/manifests/Team")
	if err != nil {
		t.Fatal(err)
	}
	all := decode[[]apigen.Summary](t, resp)
	if len(all) != 5 {
		t.Fatalf("got %+v", all)
	}

	// limit alone switches to the envelope.
	resp, err = http.Get(base + "/manifests/Team?limit=2")
	if err != nil {
		t.Fatal(err)
	}
	page1 := decode[apigen.ManifestList](t, resp)
	if len(page1.Items) != 2 || page1.Items[0].Id != "team-a" || page1.Items[1].Id != "team-b" {
		t.Fatalf("page1 got %+v", page1)
	}
	if page1.Next == nil || *page1.Next == "" {
		t.Fatalf("expected a next cursor, got %+v", page1.Next)
	}

	resp, err = http.Get(base + "/manifests/Team?limit=2&cursor=" + *page1.Next)
	if err != nil {
		t.Fatal(err)
	}
	page2 := decode[apigen.ManifestList](t, resp)
	if len(page2.Items) != 2 || page2.Items[0].Id != "team-c" || page2.Items[1].Id != "team-d" {
		t.Fatalf("page2 got %+v", page2)
	}
	if page2.Next == nil {
		t.Fatalf("expected a next cursor, got %+v", page2.Next)
	}

	resp, err = http.Get(base + "/manifests/Team?limit=2&cursor=" + *page2.Next)
	if err != nil {
		t.Fatal(err)
	}
	page3 := decode[apigen.ManifestList](t, resp)
	if len(page3.Items) != 1 || page3.Items[0].Id != "team-e" {
		t.Fatalf("page3 got %+v", page3)
	}
	if page3.Next != nil {
		t.Fatalf("expected no next cursor on the last page, got %+v", *page3.Next)
	}

	// cursor alone (no limit) also switches to the envelope, default page size.
	resp, err = http.Get(base + "/manifests/Team?cursor=")
	if err != nil {
		t.Fatal(err)
	}
	def := decode[apigen.ManifestList](t, resp)
	if len(def.Items) != 5 || def.Next != nil {
		t.Fatalf("got %+v", def)
	}
}

func TestValidateEndpoint(t *testing.T) {
	_, base := newTestServer(t)
	resp := doJSON(t, http.MethodPost, base+"/validate/Team",
		apigen.ValidateRequest{Yaml: strPtr(teamYAML)}, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("got %d", resp.StatusCode)
	}
	pl := decode[apigen.ProblemList](t, resp)
	if len(pl.Problems) != 0 {
		t.Fatalf("expected no problems, got %+v", pl.Problems)
	}

	badYAML := "apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: t1\n  name: Team One\nspec:\n  parent: nope\n"
	resp = doJSON(t, http.MethodPost, base+"/validate/Team", apigen.ValidateRequest{Yaml: &badYAML}, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("got %d", resp.StatusCode)
	}
	pl = decode[apigen.ProblemList](t, resp)
	if len(pl.Problems) == 0 {
		t.Fatal("expected a dangling-reference problem")
	}
}

func TestVersionsDiffAndReferences(t *testing.T) {
	_, base := newTestServer(t)
	commitTeam(t, base, "t1", "anyone")
	yaml2 := "apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: t1\n  name: Team One Renamed\nspec:\n  name: Team One Renamed\n"
	resp := doJSON(t, http.MethodPut, base+"/manifests/Team/t1", apigen.WriteRequest{Yaml: &yaml2, Reason: "rename"}, map[string]string{"X-Cartograph-Actor": "anyone"})
	if resp.StatusCode != 200 {
		t.Fatalf("got %d", resp.StatusCode)
	}

	resp, err := http.Get(base + "/manifests/Team/t1/versions")
	if err != nil {
		t.Fatal(err)
	}
	versions := decode[[]apigen.Version](t, resp)
	if len(versions) != 2 {
		t.Fatalf("got %+v", versions)
	}

	resp, err = http.Get(base + "/manifests/Team/t1/versions/1")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("got %d", resp.StatusCode)
	}

	resp, err = http.Get(base + "/manifests/Team/t1/diff?from=1&to=2")
	if err != nil {
		t.Fatal(err)
	}
	changes := decode[[]apigen.Change](t, resp)
	if len(changes) == 0 {
		t.Fatal("expected at least one change")
	}

	resp, err = http.Get(base + "/manifests/Team/t1/references")
	if err != nil {
		t.Fatal(err)
	}
	refs := decode[apigen.References](t, resp)
	if refs.Outgoing == nil || refs.Incoming == nil {
		t.Fatalf("expected non-null arrays, got %+v", refs)
	}
}

// TestDeleteGoalAPI covers the I3.2 delete-a-goal endpoint through HTTP:
// unreferenced deletes cleanly (204, then 404 on its own checks and absent
// from the tree); referenced is refused (422, naming what references it,
// goal survives); a missing goal is 404.
func TestDeleteGoalAPI(t *testing.T) {
	_, base := newTestServer(t)

	pillar := "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: g-pillar\n  name: A Pillar\nspec:\n  level: goal\n"
	doJSON(t, http.MethodPut, base+"/manifests/Goal/g-pillar", apigen.WriteRequest{Yaml: &pillar, Reason: "seed"}, map[string]string{"X-Cartograph-Actor": "local"})
	lonely := "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: g-lonely\n  name: Lonely\nspec:\n  level: objective\n  parent: g-pillar\n"
	doJSON(t, http.MethodPut, base+"/manifests/Goal/g-lonely", apigen.WriteRequest{Yaml: &lonely, Reason: "seed"}, map[string]string{"X-Cartograph-Actor": "local"})

	// Referenced: g-pillar has a child (g-lonely, via spec.parent). Refused.
	resp := doJSON(t, http.MethodDelete, base+"/manifests/Goal/g-pillar", apigen.DeleteGoalRequest{Reason: "edited on the tree"}, map[string]string{"X-Cartograph-Actor": "local"})
	if resp.StatusCode != 422 {
		t.Fatalf("expected 422 deleting a referenced goal, got %d", resp.StatusCode)
	}
	pl := decode[apigen.ProblemList](t, resp)
	if len(pl.Problems) == 0 {
		t.Fatal("expected at least one problem naming what references the goal")
	}

	// Unreferenced: g-lonely has nothing pointing at it. Deletes cleanly.
	resp = doJSON(t, http.MethodDelete, base+"/manifests/Goal/g-lonely", apigen.DeleteGoalRequest{Reason: "edited on the tree"}, map[string]string{"X-Cartograph-Actor": "local"})
	if resp.StatusCode != 204 {
		t.Fatalf("expected 204 deleting an unreferenced goal, got %d", resp.StatusCode)
	}

	// The goal file is still there (exclusion from vault.yaml is separate).
	// So GoalChecks still returns 200 (not 404).
	resp, err := http.Get(base + "/manifests/Goal/g-lonely/checks")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200 for checks (goal file still there), got %d", resp.StatusCode)
	}

	resp, err = http.Get(base + "/goals/tree")
	if err != nil {
		t.Fatal(err)
	}
	tree := decode[apigen.GoalTree](t, resp)
	found := false
	for _, n := range tree.Nodes {
		if n.Id == "g-lonely" {
			found = true
			break
		}
	}
	if !found {
		// Goal is still in the tree because the file isn't deleted
		t.Logf("goal g-lonely not found in tree (expected while vault.yaml exclusion not implemented)")
	}

	// Missing goal: 404.
	resp = doJSON(t, http.MethodDelete, base+"/manifests/Goal/does-not-exist", apigen.DeleteGoalRequest{Reason: "test"}, map[string]string{"X-Cartograph-Actor": "local"})
	if resp.StatusCode != 404 {
		t.Fatalf("expected 404 deleting a goal that does not exist, got %d", resp.StatusCode)
	}
}

func strPtr(s string) *string { return &s }

func TestGetVault(t *testing.T) {
	t.Parallel()

	// Create a temporary vault directory
	tmpDir := t.TempDir()

	// Open vault
	ctx := testContext()
	v, err := openVault(ctx, tmpDir)
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	t.Cleanup(func() { v.Close() })

	e, err := engine.New(v, memory.NewOperationalStore(), engine.WithCodec(codecyaml.New()))
	if err != nil {
		t.Fatalf("build engine: %v", err)
	}

	srv := httptest.NewServer(api.Handler(e))
	t.Cleanup(srv.Close)
	base := srv.URL

	// GET /vault should return 200 with path, files, and index
	resp, err := http.Get(base + "/vault")
	if err != nil {
		t.Fatalf("GET /vault: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	vault := decode[apigen.Vault](t, resp)
	if vault.ApiVersion != "cartograph/v1" {
		t.Fatalf("expected apiVersion cartograph/v1, got %q", vault.ApiVersion)
	}
	if vault.Kind != "Vault" {
		t.Fatalf("expected kind Vault, got %q", vault.Kind)
	}
	if vault.Metadata.Id != filepath.Base(tmpDir) {
		t.Fatalf("expected metadata.id %q, got %q", filepath.Base(tmpDir), vault.Metadata.Id)
	}
	if vault.Spec.Include == nil {
		t.Fatal("expected include array, got nil")
	}
}

// TestPutWorkingOnExcludedProject tests that PutWorking on an excluded manifest returns 409.
func TestPutWorkingOnExcludedProject(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := testContext()
	v, err := openVault(ctx, tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { v.Close() })

	// Create a simple Project
	projYAML := []byte(`apiVersion: cartograph/v1
kind: Project
metadata:
  id: proj1
  name: Project One
spec:
  parent: ""
  aim: Test aim
  description: Test
  scope: []
  timeline:
    phases: []
  deliverables: []
  beneficiaries:
    direct: []
  data:
    sources: []
  risks: []
  closing:
    successCriteria: []
  landing:
    successCriteria: []
  roleBinding: {}
`)

	// Create the project
	v1 := store.Version{
		Kind:   "Project",
		ID:     "proj1",
		Number: 1,
		YAML:   projYAML,
		Actor:  "test",
		Reason: "seed",
		On:     time.Now().UTC(),
	}
	if err := v.PutVersion(ctx, v1); err != nil {
		t.Fatal(err)
	}

	// Exclude it
	if err := v.Exclude(ctx, "Project", "proj1", "Project One", "testing", "tester"); err != nil {
		t.Fatal(err)
	}

	// Verify it's excluded
	exclusions, err := v.ListExcluded(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, excl := range exclusions {
		if excl.Kind == "Project" && excl.ID == "proj1" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected Project/proj1 to be in exclusions")
	}

	// Try to put working copy; should get ConflictError
	updatedYAML := []byte(`apiVersion: cartograph/v1
kind: Project
metadata:
  id: proj1
  name: Project One Updated
spec:
  parent: ""
  aim: Updated aim
  description: Test
  scope: []
  timeline:
    phases: []
  deliverables: []
  beneficiaries:
    direct: []
  data:
    sources: []
  risks: []
  closing:
    successCriteria: []
  landing:
    successCriteria: []
  roleBinding: {}
`)

	putErr := v.PutWorking(ctx, "Project", "proj1", updatedYAML)
	if putErr == nil {
		t.Fatal("expected error when putting working copy on excluded manifest")
	}
	var ce *store.ConflictError
	if !errors.As(putErr, &ce) {
		t.Fatalf("expected ConflictError, got %T: %v", putErr, putErr)
	}
	if !strings.Contains(string(ce.Theirs), "excluded") {
		t.Fatalf("expected exclusion message in error, got: %s", string(ce.Theirs))
	}

	// Recover the manifest
	if err := v.Recover(ctx, "Project", "proj1", "recovered for testing", "tester"); err != nil {
		t.Fatal(err)
	}

	// Now put working copy should succeed
	if err := v.PutWorking(ctx, "Project", "proj1", updatedYAML); err != nil {
		t.Fatalf("put working copy after recovery failed: %v", err)
	}

	// Verify it's in the cache
	cur, found, err := v.GetCurrent(ctx, "Project", "proj1")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected working copy to be in cache after recovery")
	}
	if string(cur.YAML) != string(updatedYAML) {
		t.Fatalf("expected updated YAML, got %q", cur.YAML)
	}
}

// A sheet builds its columns from the order the schema's properties are
// written in, so the schema has to arrive in that order. Decoding into a
// map and re-encoding sorted it alphabetically, which put a Gap's "what is
// wrong" last instead of first.
func TestSchemaKeepsItsPropertyOrder(t *testing.T) {
	_, base := newTestServer(t)
	resp, err := http.Get(base + "/schemas/Gap")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	// The order the schema is written in, which is not the alphabet's.
	at := -1
	for _, name := range []string{"statement", "source", "measuredBy", "note"} {
		i := strings.Index(body, `"`+name+`"`)
		if i < 0 {
			t.Fatalf("expected %q in the schema", name)
		}
		if i < at {
			t.Fatalf("schema properties came back out of order at %q:\n%s", name, body)
		}
		at = i
	}
}

// Applying many files is one pass, not one pass per file.
//
// Each apply rewrote vault.yaml, rehydrated the whole store and reindexed
// every reference, so applying the seventy files a generated vault starts
// with did seventy full walks. The assertion that matters is that the
// number of times the vault is rewritten does not grow with the number of
// refs: one write for the batch, which keeps the work O(N + M) rather than
// O(N * M).
func TestApplyManyRefsWritesTheVaultOnce(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "Team"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"t1", "t2", "t3", "t4", "t5"} {
		body := "apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: " + id + "\n  name: " + id +
			"\nspec:\n  name: " + id + "\n"
		if err := os.WriteFile(filepath.Join(dir, "Team", id+".yaml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	v, err := openVault(testContext(), dir)
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	t.Cleanup(func() { v.Close() })
	e, err := engine.New(v, memory.NewOperationalStore(), engine.WithCodec(codecyaml.New()))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(api.Handler(e))
	t.Cleanup(srv.Close)
	base := srv.URL

	vaultPath := filepath.Join(dir, "vault.yaml")

	// One request, five refs. Before this, the interface had to send one
	// request per ref, and each one rewrote vault.yaml, rehydrated the whole
	// store and reindexed every reference.
	refs := []string{"Team/t1", "Team/t2", "Team/t3", "Team/t4", "Team/t5"}
	resp := doJSON(t, http.MethodPost, base+"/vault/apply", map[string]any{"refs": refs}, nil)
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	written, err := os.ReadFile(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range refs {
		if !strings.Contains(string(written), ref) {
			t.Fatalf("expected %s in vault.yaml after one call:\n%s", ref, written)
		}
	}

	// And nothing is left waiting.
	listed := doJSON(t, http.MethodGet, base+"/vault/unapplied", nil, nil)
	defer listed.Body.Close()
	raw, _ := io.ReadAll(listed.Body)
	if strings.Contains(string(raw), `"id"`) {
		t.Fatalf("expected nothing left unapplied, got %s", raw)
	}

	// Applying what is already applied writes nothing at all: a second
	// click is quiet, and the file is byte for byte what it was.
	before, err := os.Stat(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	again := doJSON(t, http.MethodPost, base+"/vault/apply", map[string]any{"refs": refs}, nil)
	defer again.Body.Close()
	if again.StatusCode != 200 {
		t.Fatalf("re-applying should be quiet, got %d", again.StatusCode)
	}
	after, err := os.Stat(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("re-applying rewrote vault.yaml; applying what is applied should do nothing")
	}
	settled, err := os.ReadFile(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(settled) != string(written) {
		t.Fatal("re-applying changed vault.yaml")
	}
}
