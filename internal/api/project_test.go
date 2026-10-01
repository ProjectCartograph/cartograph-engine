package api_test

import (
	"context"
	codecyaml "github.com/ProjectCartograph/cartograph-engine/internal/codec/yaml"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/internal/api"
	apigen "github.com/ProjectCartograph/cartograph-engine/internal/api/gen"
	"github.com/ProjectCartograph/cartograph-engine/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/internal/store/sqlite"
	"github.com/ProjectCartograph/cartograph-engine/internal/store/vault"
)

// I3a (2026-09-17): the project journey backend. seedProjectFixtures
// commits the directory manifests a Project's sections reference (Team,
// DataSource, Goal, KPI, BeneficiaryGroup), through the ordinary
// API, actor "p1" (the bootstrap actor for every write).
func seedProjectFixtures(t *testing.T, base string) {
	t.Helper()
	put := func(kind, id, y string) {
		t.Helper()
		resp := doJSON(t, http.MethodPut, base+"/manifests/"+kind+"/"+id,
			apigen.WriteRequest{Yaml: &y, Reason: "seed"}, map[string]string{})
		if resp.StatusCode != 200 {
			t.Fatalf("seed %s/%s: got %d", kind, id, resp.StatusCode)
		}
	}
	put("Team", "t1", "apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: t1\n  name: Team One\nspec:\n  name: Team One\n")
	put("DataSource", "d1", "apiVersion: cartograph/v1\nkind: DataSource\nmetadata:\n  id: d1\n  name: Data Source One\nspec:\n  name: Data Source One\n  category: database\n  team: t1\n")
	// A success criterion names the cycle it is read on.
	put("ReportingCycle", "c1", "apiVersion: cartograph/v1\nkind: ReportingCycle\nmetadata:\n  id: c1\n  name: Cycle One\nspec:\n  name: Cycle One\n  periodMonths: 3\n  startMonth: 1\n")
	put("Goal", "g1", "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: g1\n  name: Goal One\nspec:\n  level: goal\n  objective: Improve outcomes\n")
	put("Goal", "g1-s", "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: g1-s\n  name: Goal One Strategic\nspec:\n  level: objective\n  parent: g1\n  objective: Achieve strategic outcome\n")
	put("Goal", "g1-f", "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: g1-f\n  name: Goal One Functional\nspec:\n  level: outcome\n  parent: g1-s\n  objective: Achieve functional outcome\n")
	put("Unit", "percent", "apiVersion: cartograph/v1\nkind: Unit\nmetadata:\n  id: percent\n  name: Percent\nspec:\n  name: Percent\n  dimension: percent\n")
	put("KPI", "k1", "apiVersion: cartograph/v1\nkind: KPI\nmetadata:\n  id: k1\n  name: KPI One\nspec:\n  name: KPI One\n  definition: A measured thing\n  unit: percent\n  direction: increase\n  source: d1\n  goals: [g1-f]\n")
	put("BeneficiaryGroup", "bg1", "apiVersion: cartograph/v1\nkind: BeneficiaryGroup\nmetadata:\n  id: bg1\n  name: Group One\nspec:\n  name: Group One\n  source: d1\n")
}

// fullProjectSpecYAML (I3a.1) builds a project that passes every blocking
// check, referencing only the fixtures seedProjectFixtures commits
// (t1, d1, c1, g1, g1-s, g1-f, k1, bg1).
func fullProjectSpecYAML(id string) string {
	return "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: " + id + "\n  name: Full Project\nspec:\n" +
		"  team: t1\n" +
		"  summary:\n" +
		"    problems:\n      - problem: {situation: Quality issues surface too late}\n" +
		"        change: {what: Every order is checked early}\n" +
		"    scopeIn: [Checking every order]\n" +
		"    scopeOut: [Checking orders already sold]\n" +
		"    beneficiaries:\n      - {group: bg1}\n" +
		"  funding:\n    - {amount: 1000, currency: USD, status: approved}\n" +
		"  alignment:\n    goals: [g1-f]\n" +
		"  objectives:\n" +
		"    - objective: Every order is checked\n" +
		"      keyResults:\n" +
		"        - {id: kr-1, metric: Orders checked, direction: increase, kind: count, unit: orders, baseline: {value: 0, date: \"2025-09\"}, target: {value: 100, date: \"2026-06\"}, source: d1}\n" +
		"  deliverables:\n    - {id: dv-1, name: Training pack, acceptance: [{by: {external: Quality reviewer}, outcome: signs it off}]}\n" +
		"  timeline:\n    start: \"2025-09\"\n    phases:\n      - {name: Pilot, months: 6}\n" +
		"  operation: \"new\"\n" +
		"  data:\n" +
		"    consumes:\n      - {source: d1, purpose: Identify records, handoff: apiOrFeed}\n" +
		"    produces:\n      - {output: recordsInExistingSource, sink: d1, purpose: Record results, personalData: sensitive}\n" +
		"  risks:\n    - {id: r-1, description: Something might go wrong, type: risk, mitigation: Mitigate it}\n" +
		"  successCriteria:\n" +
		"    - {id: sc-1, statement: Coverage is close to complete, metric: compliance, owner: {external: Quality reviewer}, confirmedBy: {external: Quality reviewer}, when: atClosing}\n" +
		"    - {id: sc-2, statement: The outcome is met, metric: business, standard: 90 percent, source: d1, cycle: c1, owner: {external: Quality reviewer}, confirmedBy: {external: Quality reviewer}, when: atLanding}\n" +
		"  resources:\n" +
		"    - {role: sponsor}\n    - {role: manager}\n" +
		"    - {role: teamMember}\n" +
		"    - {role: serviceOwner}\n"
}

func getChecks(t *testing.T, base, id string) apigen.ProjectChecks {
	t.Helper()
	url := base + "/manifests/Project/" + id + "/checks"
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("checks: got %d", resp.StatusCode)
	}
	return decode[apigen.ProjectChecks](t, resp)
}

func TestProjectStateEndpoints(t *testing.T) {
	_, base := newTestServer(t)
	seedProjectFixtures(t, base)

	y := "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: proj1\n  name: P\nspec:\n  team: t1\n  summary:\n    problems:\n      - problem: {situation: A gap}\n        change: {what: No more gap}\n"
	resp := doJSON(t, http.MethodPut, base+"/manifests/Project/proj1", apigen.WriteRequest{Yaml: &y, Reason: "seed"}, map[string]string{})
	if resp.StatusCode != 200 {
		t.Fatalf("got %d", resp.StatusCode)
	}

	resp, err := http.Get(base + "/manifests/Project/proj1/state")
	if err != nil {
		t.Fatal(err)
	}
	st := decode[apigen.ProjectState](t, resp)
	if st.State != "draft" || len(st.History) != 0 {
		t.Fatalf("got %+v", st)
	}

	// Blocked: this project is nowhere near ready.
	resp = doJSON(t, http.MethodPost, base+"/manifests/Project/proj1/state",
		apigen.ProjectStateTransitionRequest{To: "handed off"}, map[string]string{})
	if resp.StatusCode != 422 {
		t.Fatalf("expected 422 for a blocked submission, got %d", resp.StatusCode)
	}
	pl := decode[apigen.ProblemList](t, resp)
	if len(pl.Problems) == 0 {
		t.Fatal("expected the blocking checks as problems")
	}

	// A GET on a project's list of Summaries gains "state" for Project.
	resp, err = http.Get(base + "/manifests/Project")
	if err != nil {
		t.Fatal(err)
	}
	list := decode[[]apigen.Summary](t, resp)
	if len(list) != 1 || list[0].State == nil || *list[0].State != "draft" {
		t.Fatalf("expected Summary.state draft for the project, got %+v", list)
	}
}

func TestProjectChecksEndpoint(t *testing.T) {
	_, base := newTestServer(t)
	seedProjectFixtures(t, base)

	y := "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: proj1\n  name: P\nspec:\n  team: t1\n  summary:\n    problems:\n      - problem: {situation: A gap}\n        change: {what: No more gap}\n" +
		"  objectives:\n    - objective: Do the thing\n      keyResults:\n        - {id: kr-1, metric: Orders checked, direction: increase, kind: count, unit: orders, baseline: {value: 0, date: \"2025-09\"}, target: {value: 100, date: \"2026-06\"}, source: d1}\n" +
		"  deliverables:\n    - {id: dv-1, name: Something, acceptance: [{by: {external: Quality reviewer}, outcome: signs it off}]}\n"
	resp := doJSON(t, http.MethodPut, base+"/manifests/Project/proj1", apigen.WriteRequest{Yaml: &y, Reason: "seed"}, map[string]string{})
	if resp.StatusCode != 200 {
		t.Fatalf("got %d", resp.StatusCode)
	}

	checks := getChecks(t, base, "proj1")
	found := false
	for _, item := range checks.Items {
		if item.Id == "deliverables-count" && item.State == "ok" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected deliverables-count ok, got %+v", checks.Items)
	}
}

// Closing stands on its own since 2026-09-27: nothing is derived from the
// project's other sections, so the endpoint that used to offer proposed
// lines is gone and asking for it is a plain 404.
func TestProposedCriteriaEndpointIsGone(t *testing.T) {
	_, base := newTestServer(t)
	seedProjectFixtures(t, base)

	resp, err := http.Get(base + "/manifests/Project/proj1/proposed-criteria")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("expected 404 for a removed endpoint, got %d", resp.StatusCode)
	}
}

// Helper: newVaultTestServer creates a vault-backed test server for handoff tests
func newVaultTestServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "cartograph-test-vault-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	ctx := context.Background()
	ms, err := vault.New(ctx, tmpDir, vault.Options{OpenIndex: sqlite.OpenVaultIndexIn, Watch: false})
	if err != nil {
		t.Fatalf("create vault: %v", err)
	}
	t.Cleanup(func() { ms.Close() })

	e, err := engine.New(ms, ms.Index().Operational(), engine.WithCodec(codecyaml.New()))
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(api.Handler(e))
	t.Cleanup(srv.Close)
	return srv, srv.URL
}

// I3.3c.3: Handoff API tests
func TestProjectHandoffEndpoint(t *testing.T) {
	_, base := newVaultTestServer(t)
	seedProjectFixtures(t, base)

	// Create a complete project
	projYAML := fullProjectSpecYAML("proj-handoff")
	resp := doJSON(t, http.MethodPut, base+"/manifests/Project/proj-handoff",
		apigen.WriteRequest{Yaml: &projYAML, Reason: "seed"}, map[string]string{})
	if resp.StatusCode != 200 {
		t.Fatalf("create project: got %d", resp.StatusCode)
	}

	// Test 1: Handoff should succeed and create snapshot, return 200
	resp = doJSON(t, http.MethodPost, base+"/manifests/Project/proj-handoff/state",
		apigen.ProjectStateTransitionRequest{To: "handed off"}, map[string]string{})
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200 for handoff, got %d", resp.StatusCode)
	}

	st := decode[apigen.ProjectState](t, resp)
	if st.State != "handed off" {
		t.Fatalf("expected handed off state, got %s", st.State)
	}
	if len(st.History) == 0 {
		t.Fatal("expected state history")
	}
	lastEntry := st.History[len(st.History)-1]
	if lastEntry.State != "handed off" {
		t.Fatalf("expected handed off, got %s", lastEntry.State)
	}
	if lastEntry.Snapshot == nil || *lastEntry.Snapshot != 1 {
		t.Fatalf("expected snapshot 1, got %v", lastEntry.Snapshot)
	}
	if lastEntry.Bundle == nil || *lastEntry.Bundle == "" {
		t.Fatalf("expected non-empty bundle, got %v", lastEntry.Bundle)
	}

	// Test 2: Second handoff without new snapshot should fail
	resp = doJSON(t, http.MethodPost, base+"/manifests/Project/proj-handoff/state",
		apigen.ProjectStateTransitionRequest{To: "handed off"}, map[string]string{})
	if resp.StatusCode != 422 {
		t.Fatalf("expected 422 for second handoff, got %d", resp.StatusCode)
	}
	pl := decode[apigen.ProblemList](t, resp)
	found := false
	for _, p := range pl.Problems {
		if strings.Contains(p.Message, "already handed off at snapshot 1") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected 'already handed off at snapshot 1', got %+v", pl.Problems)
	}
}
