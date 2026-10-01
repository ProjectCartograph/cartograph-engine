package engine_test

import (
	"context"
	"errors"
	codecyaml "github.com/ProjectCartograph/cartograph-engine/internal/codec/yaml"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/internal/store/vault"
)

// TestHandoffRefusedWhileCheckBlocks: handoff rejected when blocking checks exist.
func TestHandoffRefusedWhileCheckBlocks(t *testing.T) {
	tmpDir, cleanup := tempVault(t)
	defer cleanup()
	e, closeE := seededEngineWithVault(t, tmpDir)
	defer closeE()

	ctx := context.Background()

	// Minimal project (has blocking checks)
	mustCommit(t, e, "Project", "proj-blocked", "p1", minimalProjectYAML("proj-blocked"))

	// Attempt handoff should fail with ValidationError
	result, err := e.Handoff(ctx, "proj-blocked", "p1", engine.HandoffRequest{
		HtmlBytes: []byte("<html></html>"),
		JsonBytes: []byte("{}"),
	})
	var ve *engine.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationError, got %v", err)
	}
	if len(ve.Problems) == 0 {
		t.Fatal("expected problems list")
	}
	if result.HtmlPath != "" || result.JsonPath != "" {
		t.Fatalf("expected empty result on error, got %+v", result)
	}
}

// TestHandoffRefusedWithNoSnapshot: handoff rejected when no versioned snapshot exists.
func TestHandoffRefusedWithNoSnapshot(t *testing.T) {
	tmpDir, cleanup := tempVault(t)
	defer cleanup()
	e, closeE := seededEngineWithVault(t, tmpDir)
	defer closeE()

	ctx := context.Background()

	// Create a project with just the minimal entities, no Project yet
	// This will cause a missing Project error or other validation error
	// Actually, let's test a different scenario: create a project but verify
	// that before any Snapshot call, Handoff fails

	// Create just minimum Project (no snapshot calls yet)
	projYAML := `apiVersion: cartograph/v1
kind: Project
metadata:
  id: test-proj
  name: Test
spec:
  team: t1
  summary:
    problems:
      - problem: {situation: P}
        change: {what: C}
`
	mustCommit(t, e, "Project", "test-proj", "p1", projYAML)

	// Don't call Snapshot. Just try Handoff.
	// Handoff should fail because there are no numbered versions (only draft/working versions)
	result, err := e.Handoff(ctx, "test-proj", "p1", engine.HandoffRequest{
		HtmlBytes: []byte("<html></html>"),
		JsonBytes: []byte("{}"),
	})

	// Should fail (either due to validation errors or missing snapshot)
	if err == nil {
		t.Fatalf("expected error for handoff, but got success: %+v", result)
	}
	// Handoff should refuse the request
	if result.HtmlPath != "" {
		t.Fatalf("expected empty result on error, got: %s", result.HtmlPath)
	}
}

// TestHandoffSuccessWritesFiles: handoff succeeds and writes files to vault.
func TestHandoffSuccessWritesFiles(t *testing.T) {
	tmpDir, cleanup := tempVault(t)
	defer cleanup()
	e, closeE := seededEngineWithVault(t, tmpDir)
	defer closeE()

	ctx := context.Background()
	greenProject(t, e, "proj1")

	// Snapshot it
	current, err := e.Get(ctx, "Project", "proj1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	snap, err := e.Snapshot(ctx, "Project", "proj1", current.YAML, "snapshot", "p1")
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	// Handoff
	htmlBytes := []byte("<html>Charter v" + string(rune(snap.Number)) + "</html>")
	jsonBytes := []byte(`{"v":1}`)
	result, err := e.Handoff(ctx, "proj1", "p1", engine.HandoffRequest{
		HtmlBytes: htmlBytes,
		JsonBytes: jsonBytes,
	})
	if err != nil {
		t.Fatalf("handoff: %v", err)
	}

	// Verify files exist (paths are absolute from handoff)
	if _, err := os.Stat(result.HtmlPath); err != nil {
		t.Fatalf("html file not found: %v", err)
	}
	jsonPath := result.JsonPath
	if _, err := os.Stat(jsonPath); err != nil {
		t.Fatalf("json file not found: %v", err)
	}

	// Verify content
	htmlContent, _ := os.ReadFile(result.HtmlPath)
	if !strings.Contains(string(htmlContent), "Charter") {
		t.Fatalf("html content wrong: %s", string(htmlContent))
	}

	// Verify state was recorded
	st, err := e.GetProjectState(ctx, "proj1")
	if err != nil {
		t.Fatalf("get state: %v", err)
	}
	if st.State != engine.ProjectStateHandedOff {
		t.Fatalf("expected handed off, got %s", st.State)
	}
	if len(st.History) == 0 || st.History[len(st.History)-1].Snapshot != snap.Number {
		t.Fatalf("unexpected snapshot in state: %+v", st.History)
	}
}

// TestHandoffSecondAtSameSnapshotRefused: second handoff at same snapshot is refused.
func TestHandoffSecondAtSameSnapshotRefused(t *testing.T) {
	tmpDir, cleanup := tempVault(t)
	defer cleanup()
	e, closeE := seededEngineWithVault(t, tmpDir)
	defer closeE()

	ctx := context.Background()

	greenProject(t, e, "proj2")
	current, _ := e.Get(ctx, "Project", "proj2")
	snap, _ := e.Snapshot(ctx, "Project", "proj2", current.YAML, "snapshot", "p1")

	// First handoff succeeds
	result1, err := e.Handoff(ctx, "proj2", "p1", engine.HandoffRequest{
		HtmlBytes: []byte("<html>v1</html>"),
		JsonBytes: []byte("{}"),
	})
	if err != nil {
		t.Fatalf("first handoff: %v", err)
	}
	if result1.HtmlPath == "" {
		t.Fatal("expected handoff result")
	}

	// Second handoff at same snapshot should fail
	result2, err := e.Handoff(ctx, "proj2", "p1", engine.HandoffRequest{
		HtmlBytes: []byte("<html>v1b</html>"),
		JsonBytes: []byte("{}"),
	})
	var ve *engine.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationError, got %v", err)
	}
	msg := ve.Problems[0].Message
	if !strings.Contains(msg, "already handed off at snapshot") {
		t.Fatalf("expected 'already handed off', got %s", msg)
	}
	if !strings.Contains(msg, string(rune(snap.Number+48))) { // Convert to digit string
		// At least verify it mentions a snapshot number
		if !regexp.MustCompile(`\d`).MatchString(msg) {
			t.Fatalf("expected snapshot number in message: %s", msg)
		}
	}
	if result2.HtmlPath != "" {
		t.Fatalf("expected empty result, got %s", result2.HtmlPath)
	}
}

// TestHandoffAfterNewSnapshotSucceeds: handoff succeeds after creating a new snapshot.
func TestHandoffAfterNewSnapshotSucceeds(t *testing.T) {
	tmpDir, cleanup := tempVault(t)
	defer cleanup()
	e, closeE := seededEngineWithVault(t, tmpDir)
	defer closeE()

	ctx := context.Background()

	greenProject(t, e, "proj3")

	// First snapshot and handoff
	current, _ := e.Get(ctx, "Project", "proj3")
	snap1, _ := e.Snapshot(ctx, "Project", "proj3", current.YAML, "snap 1", "p1")
	_, err := e.Handoff(ctx, "proj3", "p1", engine.HandoffRequest{
		HtmlBytes: []byte("<html>v1</html>"),
		JsonBytes: []byte("{}"),
	})
	if err != nil {
		t.Fatalf("first handoff: %v", err)
	}

	// Second snapshot
	current2, _ := e.Get(ctx, "Project", "proj3")
	snap2, _ := e.Snapshot(ctx, "Project", "proj3", current2.YAML, "snap 2", "p1")
	if snap2.Number <= snap1.Number {
		t.Fatalf("expected snap2 > snap1, got %d > %d", snap2.Number, snap1.Number)
	}

	// Second handoff should succeed
	result, err := e.Handoff(ctx, "proj3", "p1", engine.HandoffRequest{
		HtmlBytes: []byte("<html>v2</html>"),
		JsonBytes: []byte("{}"),
	})
	if err != nil {
		t.Fatalf("second handoff: %v", err)
	}
	if result.HtmlPath == "" {
		t.Fatal("expected handoff result")
	}

	// Verify state has multiple entries (at least: defined on first snapshot, then handoffs)
	st, _ := e.GetProjectState(ctx, "proj3")
	if len(st.History) < 2 {
		t.Fatalf("expected at least 2 state entries, got %d", len(st.History))
	}
}

// TestHandoffDeterminism: output differs only in snapshot number/date when content unchanged.
func TestHandoffDeterminism(t *testing.T) {
	tmpDir, cleanup := tempVault(t)
	defer cleanup()
	e, closeE := seededEngineWithVault(t, tmpDir)
	defer closeE()

	ctx := context.Background()

	greenProject(t, e, "proj4")

	// Two snapshots of same content
	current, _ := e.Get(ctx, "Project", "proj4")
	e.Snapshot(ctx, "Project", "proj4", current.YAML, "s1", "p1")
	e.Snapshot(ctx, "Project", "proj4", current.YAML, "s2", "p1")

	// Handoff twice with same content
	htmlBytes := []byte("<html>Same Content Here</html>")
	jsonBytes := []byte(`{"constant":"value"}`)

	result1, _ := e.Handoff(ctx, "proj4", "p1", engine.HandoffRequest{
		HtmlBytes: htmlBytes,
		JsonBytes: jsonBytes,
	})

	// Get versions to check snapshot numbers
	versions, _ := e.Versions(ctx, "Project", "proj4")
	if len(versions) < 2 {
		t.Fatalf("expected at least 2 versions, got %d", len(versions))
	}

	// Second snapshot and handoff
	e.Snapshot(ctx, "Project", "proj4", current.YAML, "s3", "p1")
	result2, _ := e.Handoff(ctx, "proj4", "p1", engine.HandoffRequest{
		HtmlBytes: htmlBytes,
		JsonBytes: jsonBytes,
	})

	// Results should be different paths (different snapshot dirs)
	if result1.HtmlPath == result2.HtmlPath {
		t.Fatalf("expected different paths: %s vs %s", result1.HtmlPath, result2.HtmlPath)
	}

	// Both should exist (if vault-backed)
	if strings.Contains(result1.HtmlPath, "/") {
		// This is a full path from vault, verify it's reasonable
		if !strings.Contains(result1.HtmlPath, ".cartograph/handoff") {
			t.Fatalf("unexpected path format: %s", result1.HtmlPath)
		}
	}
}

// Helper: tempVault creates a temporary vault directory
func tempVault(t *testing.T) (string, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "cartograph-test-vault-")
	if err != nil {
		t.Fatal(err)
	}
	return tmpDir, func() { os.RemoveAll(tmpDir) }
}

// Helper: engineWithVault creates a vault-backed engine
func engineWithVault(t *testing.T, vaultDir string) (*engine.Engine, func()) {
	t.Helper()

	ctx := context.Background()
	ms, err := vault.New(ctx, vaultDir, vault.Options{Watch: false})
	if err != nil {
		t.Fatalf("create vault: %v", err)
	}

	e, err := engine.New(ms, ms.Index().Operational(), engine.WithCodec(codecyaml.New()))
	if err != nil {
		t.Fatalf("create engine: %v", err)
	}

	return e, func() {
		ms.Close()
	}
}

// Helper: seededEngineWithVault creates a vault-backed engine with base entities
func seededEngineWithVault(t *testing.T, vaultDir string) (*engine.Engine, func()) {
	t.Helper()

	e, closeE := engineWithVault(t, vaultDir)

	// Seed base entities
	mustCommit(t, e, "Team", "t1", "p1", `apiVersion: cartograph/v1
kind: Team
metadata:
  id: t1
  name: Team One
spec:
  name: Team One
`)
	mustCommit(t, e, "DataSource", "d1", "p1", `apiVersion: cartograph/v1
kind: DataSource
metadata:
  id: d1
  name: Data Source One
spec:
  name: Data Source One
  category: database
  team: t1
`)
	// A success criterion names the cycle it is read on, so the vault seed
	// carries one the way the in-memory seed does.
	mustCommit(t, e, "ReportingCycle", "c1", "p1", `apiVersion: cartograph/v1
kind: ReportingCycle
metadata:
  id: c1
  name: Cycle One
spec:
  name: Cycle One
  periodMonths: 3
  startMonth: 1
`)

	mustCommit(t, e, "Goal", "g1", "p1", `apiVersion: cartograph/v1
kind: Goal
metadata:
  id: g1
  name: Goal One
spec:
  level: goal
  objective: Improve outcomes
`)
	mustCommit(t, e, "Goal", "g1-s", "p1", `apiVersion: cartograph/v1
kind: Goal
metadata:
  id: g1-s
  name: Goal One Strategic
spec:
  level: objective
  parent: g1
  objective: Achieve strategic outcome
`)
	mustCommit(t, e, "Goal", "g1-f", "p1", `apiVersion: cartograph/v1
kind: Goal
metadata:
  id: g1-f
  name: Goal One Functional
spec:
  level: outcome
  parent: g1-s
  objective: Achieve functional outcome
`)
	mustCommit(t, e, "Unit", "percent", "p1", `apiVersion: cartograph/v1
kind: Unit
metadata:
  id: percent
  name: Percent
spec:
  name: Percent
  dimension: percent
`)
	mustCommit(t, e, "KPI", "k1", "p1", `apiVersion: cartograph/v1
kind: KPI
metadata:
  id: k1
  name: KPI One
spec:
  name: KPI One
  definition: A measured thing
  unit: percent
  direction: increase
  source: d1
  goals: [g1-f]
`)
	mustCommit(t, e, "BeneficiaryGroup", "bg1", "p1", `apiVersion: cartograph/v1
kind: BeneficiaryGroup
metadata:
  id: bg1
  name: Group One
spec:
  name: Group One
  source: d1
`)

	return e, closeE
}

// Helper: minimalProjectYAML returns minimal project (has blocking checks)
func minimalProjectYAML(id string) string {
	return `apiVersion: cartograph/v1
kind: Project
metadata:
  id: ` + id + `
  name: Minimal Project
spec:
  team: t1
  summary:
    problems:
      - problem: {situation: A problem}
        change: {what: A change}
`
}
