package render

import (
	"context"
	"crypto/sha256"
	"fmt"
	codecyaml "github.com/ProjectCartograph/cartograph-engine/v2/internal/codec/yaml"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store/memory"
)

// TestCharterDeterminism renders the same snapshot twice and verifies
// HTML and JSON are byte-identical (SHA-256).
func TestCharterDeterminism(t *testing.T) {
	ctx := context.Background()

	// Create an in-memory engine with test data
	manifests := memory.NewManifestStore()
	operational := memory.NewOperationalStore()
	e, err := engine.New(manifests, operational, engine.WithCodec(codecyaml.New()))
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	// Create a test project
	projectYAML := `apiVersion: cartograph/v1
kind: Project
metadata:
  id: test-project
  name: Test Project
spec:
  team: test-team
  summary:
    problems:
      - problem: {situation: Test problem}
        change: {what: Test change}
    scopeIn:
      - Item 1
      - Item 2
    scopeOut:
      - Item 3
  objectives:
    - objective: Test objective
      keyResults:
        - id: kr-1
          metric: Test metric
          direction: increase
          kind: count
          unit: items
          baseline: {value: 0, date: "2025-01"}
          target: {value: 100, date: "2025-12"}
          source: test-source
  deliverables:
    - id: dv-1
      name: Deliverable 1
      description: Test deliverable
      acceptance:
        - by: Test reviewer
          outcome: confirms the test criteria
  successCriteria:
    - id: sc-1
      statement: Test success criterion
      metric: business
      confirmedBy: {external: Sponsor}
      when: at landing
  timeline:
    start: "2025-01"
    phases:
      - name: Phase 1
        months: 3
      - name: Phase 2
        months: 3
  risks:
    - description: Test risk
      type: risk
      impact: low
      likelihood: low
  compliance:
    - item: Test compliance item
      status: not started
  resources:
    - role: sponsor
    - role: manager
`

	if err := e.PutWorking(ctx, "Project", "test-project", []byte(projectYAML)); err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	// Render twice
	html1, json1, err := Charter(ctx, e, "test-project", 0)
	if err != nil {
		t.Fatalf("first render failed: %v", err)
	}

	html2, json2, err := Charter(ctx, e, "test-project", 0)
	if err != nil {
		t.Fatalf("second render failed: %v", err)
	}

	// Verify byte-identical hashes
	hash1HTML := sha256.Sum256(html1)
	hash2HTML := sha256.Sum256(html2)
	if hash1HTML != hash2HTML {
		t.Errorf("HTML not deterministic: hash1=%x, hash2=%x", hash1HTML, hash2HTML)
	}

	hash1JSON := sha256.Sum256(json1)
	hash2JSON := sha256.Sum256(json2)
	if hash1JSON != hash2JSON {
		t.Errorf("JSON not deterministic: hash1=%x, hash2=%x", hash1JSON, hash2JSON)
	}
}

// TestCharterSectionsHeld verifies that sections held elsewhere are marked correctly.
func TestCharterSectionsHeld(t *testing.T) {
	ctx := context.Background()

	// Create an in-memory engine with test data
	manifests := memory.NewManifestStore()
	operational := memory.NewOperationalStore()
	e, err := engine.New(manifests, operational, engine.WithCodec(codecyaml.New()))
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	// Create a minimal test project
	projectYAML := `apiVersion: cartograph/v1
kind: Project
metadata:
  id: test-project
  name: Test Project
spec:
  team: test-team
  summary:
    problems:
      - problem: {situation: Test problem}
        change: {what: Test change}
  timeline:
    start: "2025-01"
    phases:
      - name: Phase 1
        months: 3
  resources:
    - role: sponsor
`

	if err := e.PutWorking(ctx, "Project", "test-project", []byte(projectYAML)); err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	// Render the charter
	html, _, err := Charter(ctx, e, "test-project", 0)
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}

	htmlStr := string(html)

	// Verify that sections held elsewhere are present
	expectedSections := []string{
		"Related plans",
		"Detailed plan",
		"Procurement",
		"Communications and training",
		"Approval",
	}

	for _, section := range expectedSections {
		if !contains(htmlStr, section) {
			t.Errorf("expected section %q not found in HTML", section)
		}
	}
}

// TestCharterWithSnapshot verifies that rendering a snapshot works.
func TestCharterWithSnapshot(t *testing.T) {
	ctx := context.Background()

	// Create an in-memory engine with test data
	manifests := memory.NewManifestStore()
	operational := memory.NewOperationalStore()
	e, err := engine.New(manifests, operational, engine.WithCodec(codecyaml.New()))
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	// Create a Team first
	teamYAML := `apiVersion: cartograph/v1
kind: Team
metadata:
  id: test-team
  name: Test Team
spec:
  accountability: "Entity-owner"
  mandate: |
    Test mandate
`

	if err := e.PutWorking(ctx, "Team", "test-team", []byte(teamYAML)); err != nil {
		t.Fatalf("failed to create team: %v", err)
	}

	// Create a test project
	projectYAML := `apiVersion: cartograph/v1
kind: Project
metadata:
  id: test-project
  name: Test Project
spec:
  team: test-team
  summary:
    problems:
      - problem: {situation: Test problem}
        change: {what: Test change}
  timeline:
    start: "2025-01"
    phases:
      - name: Phase 1
        months: 3
  resources:
    - role: sponsor
`

	// Put working copy
	if err := e.PutWorking(ctx, "Project", "test-project", []byte(projectYAML)); err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	// Create a snapshot
	_, err = e.Snapshot(ctx, "Project", "test-project", []byte(projectYAML), "initial version", "test-operator")
	if err != nil {
		t.Fatalf("failed to create snapshot: %v", err)
	}

	// Render the snapshot
	html, _, err := Charter(ctx, e, "test-project", 1)
	if err != nil {
		t.Fatalf("render snapshot failed: %v", err)
	}

	htmlStr := string(html)

	// Verify snapshot version is present (version field shows "1")
	if !contains(htmlStr, "Version 1, saved") {
		t.Errorf("expected snapshot version 1 in HTML")
	}
}

// TestCharterNoSnapshot verifies that working copy is returned when no snapshot.
func TestCharterWorkingCopy(t *testing.T) {
	ctx := context.Background()

	// Create an in-memory engine with test data
	manifests := memory.NewManifestStore()
	operational := memory.NewOperationalStore()
	e, err := engine.New(manifests, operational, engine.WithCodec(codecyaml.New()))
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	// Create a test project
	projectYAML := `apiVersion: cartograph/v1
kind: Project
metadata:
  id: test-project
  name: Test Project
spec:
  team: test-team
  summary:
    problems:
      - problem: {situation: Test problem}
        change: {what: Test change}
  timeline:
    start: "2025-01"
    phases:
      - name: Phase 1
        months: 3
  resources:
    - role: sponsor
`

	// Put working copy (no snapshot)
	if err := e.PutWorking(ctx, "Project", "test-project", []byte(projectYAML)); err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	// Render with snapshot=0 (working copy)
	html, _, err := Charter(ctx, e, "test-project", 0)
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}

	htmlStr := string(html)

	// Verify it's marked as working
	if !contains(htmlStr, "Working draft") {
		t.Errorf("expected 'Working draft' in HTML for version 0")
	}
}

// Helper function to check if HTML contains a string
func contains(html, str string) bool {
	return fmt.Sprintf("%v", html) != "" && findSubstring(html, str)
}

// findSubstring checks if a substring exists in a string
func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestCharterWorkBreakdown checks a deliverable's tasks are printed under
// it and carried in the JSON coded D1, D1.1, for the planning tool.
func TestCharterWorkBreakdown(t *testing.T) {
	ctx := context.Background()
	e, err := engine.New(memory.NewManifestStore(), memory.NewOperationalStore(), engine.WithCodec(codecyaml.New()))
	if err != nil {
		t.Fatal(err)
	}
	projectYAML := `apiVersion: cartograph/v1
kind: Project
metadata:
  id: test-project
  name: Test Project
spec:
  team: test-team
  deliverables:
    - id: dv-1
      name: Training pack
      tasks:
        - id: t-1
          name: Prepare the facilitator guide
          role: {external: Trainer}
        - id: t-2
          name: Record attendance
          note: Use the sign-in sheet
    - id: dv-2
      name: Results view
  resources:
    - role: sponsor
`
	if err := e.PutWorking(ctx, "Project", "test-project", []byte(projectYAML)); err != nil {
		t.Fatal(err)
	}
	html, js, err := Charter(ctx, e, "test-project", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Work breakdown", "D1.1", "Prepare the facilitator guide", "Trainer", "D2"} {
		if !contains(string(html), want) {
			t.Errorf("charter HTML lacks %q", want)
		}
	}
	for _, want := range []string{`"workBreakdown"`, `"code": "D1.2"`, `"note": "Use the sign-in sheet"`, `"code": "D2"`} {
		if !contains(string(js), want) {
			t.Errorf("charter JSON lacks %s", want)
		}
	}
}
