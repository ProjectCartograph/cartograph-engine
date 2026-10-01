package engine_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestImportRewritesLegacyGoalLevel exercises I3.1 and I3.2's one-release
// backward compatibility: a Goal file still using the retired level value
// "team" (I3.1's own retired name) is accepted and rewritten to "strategic";
// a Goal at "strategic" with no parent (I3.1's own root level) is rewritten
// to "pillar" (I3.2's new root level). Both are reported as notes, not
// problems. Note: I3.2's "functional" level was the predecessor to I3.4a's
// "functional", so "functional" with no parent gets rewritten to "strategic",
// and "functional" with a strategic parent is treated as I3.4a format and
// left unchanged.
func TestImportRewritesLegacyGoalLevel(t *testing.T) {
	e := newTestEngine(t)
	ctx := context.Background()
	dir := t.TempDir()

	mustWriteFile(t, filepath.Join(dir, "Goal", "old-goal.yaml"), "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: old-goal\n  name: Old Goal\nspec:\n  level: team\n  parent: old-strategic\n  objective: Something old\n")
	mustWriteFile(t, filepath.Join(dir, "Goal", "old-strategic.yaml"), "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: old-strategic\n  name: Old Strategic\nspec:\n  level: strategic\n  objective: A strategic objective\n")

	report, err := e.ImportDir(ctx, dir, "bootstrap", "legacy import")
	if err != nil {
		t.Fatalf("ImportDir returned an error: %v", err)
	}
	if len(report.Problems) != 0 {
		t.Fatalf("expected a clean import, got problems: %+v", report.Problems)
	}
	if len(report.Notes) != 2 {
		t.Fatalf("expected both files to carry a rewrite note, got %+v", report.Notes)
	}

	v, err := e.Get(ctx, "Goal", "old-goal")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(v.YAML), "level: team") {
		t.Fatalf("expected the committed YAML to use the new level value, got:\n%s", v.YAML)
	}
	if !strings.Contains(string(v.YAML), "level: objective") {
		t.Fatalf("expected the committed YAML to read level: objective, got:\n%s", v.YAML)
	}

	sv, err := e.Get(ctx, "Goal", "old-strategic")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(sv.YAML), "level: strategic") {
		t.Fatalf("expected the committed YAML to use the new root level, got:\n%s", sv.YAML)
	}
	if !strings.Contains(string(sv.YAML), "level: goal") {
		t.Fatalf("expected the committed YAML to read level: goal, got:\n%s", sv.YAML)
	}
}

// TestImportRewritesLegacyKeyResultAndKPIDates covers the same one-release
// compatibility for KeyResult.baseline.asOf/target.by (on both a Goal's own
// key results and a KPI) rewritten to baseline.date/target.date.
func TestImportRewritesLegacyKeyResultAndKPIDates(t *testing.T) {
	e := newTestEngine(t)
	ctx := context.Background()
	dir := t.TempDir()

	mustWriteFile(t, filepath.Join(dir, "Team", "t1.yaml"), "apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: t1\n  name: Team One\nspec:\n  name: Team One\n")
	mustWriteFile(t, filepath.Join(dir, "DataSource", "ds1.yaml"), "apiVersion: cartograph/v1\nkind: DataSource\nmetadata:\n  id: ds1\n  name: DS One\nspec:\n  name: DS One\n  category: database\n  team: t1\n")
	mustWriteFile(t, filepath.Join(dir, "Goal", "old-goal.yaml"), "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: old-goal\n  name: Old Goal\nspec:\n  level: strategic\n  objective: Something old\n  keyResults:\n    - id: kr-1\n      metric: Old metric\n      direction: increase\n      kind: percent\n      baseline: {value: 10, asOf: \"2025-01\"}\n      target: {value: 20, by: \"2026-01\"}\n")
	mustWriteFile(t, filepath.Join(dir, "Unit", "percent.yaml"), "apiVersion: cartograph/v1\nkind: Unit\nmetadata:\n  id: percent\n  name: Percent\nspec:\n  name: Percent\n  dimension: percent\n")
	mustWriteFile(t, filepath.Join(dir, "KPI", "old-kpi.yaml"), "apiVersion: cartograph/v1\nkind: KPI\nmetadata:\n  id: old-kpi\n  name: Old KPI\nspec:\n  name: Old KPI\n  definition: A definition\n  unit: percent\n  direction: increase\n  baseline: {value: 5, asOf: \"2025-01\"}\n  target: {value: 10, by: \"2026-01\"}\n  source: ds1\n")

	report, err := e.ImportDir(ctx, dir, "bootstrap", "legacy import")
	if err != nil {
		t.Fatalf("ImportDir returned an error: %v", err)
	}
	if len(report.Problems) != 0 {
		t.Fatalf("expected a clean import, got problems: %+v", report.Problems)
	}
	if len(report.Notes) != 2 {
		t.Fatalf("expected two files to carry rewrite notes, got %+v", report.Notes)
	}

	goalV, err := e.Get(ctx, "Goal", "old-goal")
	if err != nil {
		t.Fatal(err)
	}
	for _, old := range []string{"asOf", "by:"} {
		if strings.Contains(string(goalV.YAML), old) {
			t.Fatalf("expected the committed Goal YAML to use the new field names, still has %q:\n%s", old, goalV.YAML)
		}
	}
	if !strings.Contains(string(goalV.YAML), "date:") {
		t.Fatalf("expected the committed Goal YAML to use date:, got:\n%s", goalV.YAML)
	}

	kpiV, err := e.Get(ctx, "KPI", "old-kpi")
	if err != nil {
		t.Fatal(err)
	}
	for _, old := range []string{"asOf", "by:"} {
		if strings.Contains(string(kpiV.YAML), old) {
			t.Fatalf("expected the committed KPI YAML to use the new field names, still has %q:\n%s", old, kpiV.YAML)
		}
	}
}

// TestImportDoesNotRewriteCurrentFieldNames is the negative case: a file
// already in the current shape produces no note and is committed
// byte-identical in substance (re-marshaled only when a rewrite actually
// happens).
func TestImportDoesNotRewriteCurrentFieldNames(t *testing.T) {
	e := newTestEngine(t)
	ctx := context.Background()
	dir := t.TempDir()

	mustWriteFile(t, filepath.Join(dir, "Goal", "g1.yaml"), "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: g1\n  name: G1\nspec:\n  level: goal\n  objective: Already current\n")

	report, err := e.ImportDir(ctx, dir, "bootstrap", "no rewrite needed")
	if err != nil {
		t.Fatalf("ImportDir returned an error: %v", err)
	}
	if len(report.Problems) != 0 {
		t.Fatalf("expected a clean import, got problems: %+v", report.Problems)
	}
	if len(report.Notes) != 0 {
		t.Fatalf("expected no rewrite notes, got %+v", report.Notes)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A deliverable's acceptance used to be one sentence, which left nowhere
// to say who verifies it and nowhere to put a second signature. Importing
// a file still written that way keeps the sentence, as one criterion with
// no verifier named yet.
func TestImportRewritesLegacyDeliverableAcceptance(t *testing.T) {
	e := newTestEngine(t)
	ctx := context.Background()
	dir := t.TempDir()

	mustWriteFile(t, filepath.Join(dir, "Team", "t1.yaml"), "apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: t1\n  name: Team One\nspec:\n  name: Team One\n")
	mustWriteFile(t, filepath.Join(dir, "Project", "old-project.yaml"),
		"apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: old-project\n  name: Old Project\nspec:\n  team: t1\n"+
			"  summary:\n    problems:\n      - problem: {situation: A gap}\n        change: {what: No more gap}\n"+
			"  deliverables:\n    - id: dv-1\n      name: Training pack\n      acceptance: Reviewer signs off\n")

	report, err := e.ImportDir(ctx, dir, "bootstrap", "legacy import")
	if err != nil {
		t.Fatalf("ImportDir returned an error: %v", err)
	}
	if len(report.Problems) != 0 {
		t.Fatalf("expected a clean import, got problems: %+v", report.Problems)
	}
	if len(report.Notes) != 1 {
		t.Fatalf("expected the project file to carry a rewrite note, got %+v", report.Notes)
	}

	v, err := e.Get(ctx, "Project", "old-project")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(v.YAML), "outcome: Reviewer signs off") {
		t.Fatalf("expected the sentence kept as one criterion's outcome, got:\n%s", v.YAML)
	}

	// The check that reads it now sees a criterion with no verifier.
	checks, err := e.ProjectChecks(ctx, "old-project", false)
	if err != nil {
		t.Fatal(err)
	}
	byID := checksByID(checks.Items)
	if byID["deliverables-acceptance"].State != "ok" {
		t.Fatalf("expected deliverables-acceptance ok, got %+v", byID["deliverables-acceptance"])
	}
	if byID["deliverables-verifier"].State != "warn" {
		t.Fatalf("expected deliverables-verifier warn for a criterion with no verifier, got %+v", byID["deliverables-verifier"])
	}
}

// A vault is a directory people also edit by hand and sync with whatever
// their organisation already uses, so a file in last release's shape can
// appear without ever going through an import. Reading one gives the
// current shape anyway; the file itself is left alone until the next save.
func TestReadingAVaultFileInTheOldShapeGivesTheCurrentOne(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	mustWriteFile(t, filepath.Join(dir, "Team", "t1.yaml"), "apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: t1\n  name: Team One\nspec:\n  name: Team One\n")
	oldShape := "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: old-shape\n  name: P\nspec:\n  team: t1\n" +
		"  summary:\n    problems:\n      - problem: {situation: A gap}\n        change: {what: No more gap}\n" +
		"  deliverables:\n    - id: dv-1\n      name: Training pack\n      acceptance: Reviewer signs off\n"
	mustWriteFile(t, filepath.Join(dir, "Project", "old-shape.yaml"), oldShape)

	e, cleanup := engineWithVault(t, dir)
	defer cleanup()

	v, err := e.Get(ctx, "Project", "old-shape")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(v.YAML), "outcome: Reviewer signs off") {
		t.Fatalf("expected Get to hand back the current shape, got:\n%s", v.YAML)
	}

	checks, err := e.ProjectChecks(ctx, "old-shape", false)
	if err != nil {
		t.Fatal(err)
	}
	if got := checksByID(checks.Items)["deliverables-acceptance"]; got.State != "ok" {
		t.Fatalf("expected the old sentence to still count as acceptance, got %+v", got)
	}
	if got := checksByID(checks.Items)["deliverables-verifier"]; got.State != "warn" {
		t.Fatalf("expected the rewritten criterion to have no verifier yet, got %+v", got)
	}

	// The file on disk is untouched: a vault is the person's own directory.
	onDisk, err := os.ReadFile(filepath.Join(dir, "Project", "old-shape.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != oldShape {
		t.Fatalf("reading must not rewrite the file, got:\n%s", onDisk)
	}
}

// Cartograph used to ask which sections of the definition each accountable
// role answered for. No charter template asks that: a RACI's rows are
// deliverables and decisions, which is a planning judgement about work,
// not about the paragraphs of a document. A file still carrying the list
// reads without it rather than failing validation.
func TestReadingAFileWithPerSectionAccountabilityDropsIt(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	mustWriteFile(t, filepath.Join(dir, "Team", "t1.yaml"), "apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: t1\n  name: Team One\nspec:\n  name: Team One\n")
	mustWriteFile(t, filepath.Join(dir, "Project", "old-raci.yaml"),
		"apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: old-raci\n  name: P\nspec:\n  team: t1\n"+
			"  summary:\n    problems:\n      - problem: {situation: A gap}\n        change: {what: No more gap}\n"+
			"  resources:\n    - {role: sponsor}\n    - {role: lead}\n"+
			"    - {role: owner, title: Delivery lead, accountableFor: [summary, deliverables]}\n")

	e, cleanup := engineWithVault(t, dir)
	defer cleanup()

	v, err := e.Get(ctx, "Project", "old-raci")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(v.YAML), "accountableFor") {
		t.Fatalf("expected the section list dropped on read, got:\n%s", v.YAML)
	}
	// The row survives; the free-text name it carried does not. A role is
	// named by the catalogue entry it references since 2026-09-29, and a
	// legacy row that named one only in prose has nothing to point at.
	if strings.Contains(string(v.YAML), "Delivery lead") {
		t.Fatalf("expected the free-text role name dropped on read, got:\n%s", v.YAML)
	}
	if n := strings.Count(string(v.YAML), "role:"); n != 3 {
		t.Fatalf("expected all three rows to survive, got %d:\n%s", n, v.YAML)
	}

	checks, err := e.ProjectChecks(ctx, "old-raci", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, present := checksByID(checks.Items)["resources-accountable"]; present {
		t.Fatal("the per-section accountability check should no longer exist")
	}
}

// A project written before 2026-09-28 names its verifier, owner and
// confirmer by copying the role's title. Importing one back-fills an id on
// every role and rewrites each copy into a reference to it, so the title
// exists once and renaming it can no longer strand anything. A title that
// matches no role is kept as an external reference rather than dropped: a
// confirmer is often a governance body the vault has no manifest for.
func TestImportRewritesRoleTitlesToReferences(t *testing.T) {
	e := newTestEngine(t)
	ctx := context.Background()
	dir := t.TempDir()

	mustWriteFile(t, filepath.Join(dir, "Team", "t1.yaml"), "apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: t1\n  name: Team One\nspec:\n  name: Team One\n")
	mustWriteFile(t, filepath.Join(dir, "Project", "old-roles.yaml"),
		"apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: old-roles\n  name: Old Roles\nspec:\n  team: t1\n"+
			"  summary:\n    problems:\n      - problem: {situation: A gap}\n        change: {what: No more gap}\n"+
			"  resources:\n    - {role: owner, title: Quality reviewer}\n"+
			"  timeline:\n    start: \"2026-01\"\n    phases:\n      - {name: Design, months: 3}\n"+
			"  deliverables:\n    - id: dv-1\n      name: Training pack\n      acceptance: [{by: Quality reviewer, outcome: signs it off}]\n"+
			"  successCriteria:\n    - id: sc-1\n      statement: Coverage is close to complete\n      metric: compliance\n      owner: Quality reviewer\n      confirmedBy: The the board\n      when: atClosing\n")

	report, err := e.ImportDir(ctx, dir, "bootstrap", "legacy import")
	if err != nil {
		t.Fatalf("ImportDir returned an error: %v", err)
	}
	if len(report.Problems) != 0 {
		t.Fatalf("expected a clean import, got problems: %+v", report.Problems)
	}

	v, err := e.Get(ctx, "Project", "old-roles")
	if err != nil {
		t.Fatal(err)
	}
	got := string(v.YAML)
	// The role gained an id derived from its own title, and the phase one
	// derived from its name, so a reader can tell what each id is.
	for _, want := range []string{"id: quality-reviewer", "id: design"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in the rewritten YAML, got:\n%s", want, got)
		}
	}
	// A title that matched a role became a local reference; one that
	// matched nothing was kept as external rather than lost.
	for _, want := range []string{"local: resources", "external: The the board"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in the rewritten YAML, got:\n%s", want, got)
		}
	}
	if strings.Contains(got, "by: Quality reviewer") || strings.Contains(got, "owner: Quality reviewer") {
		t.Fatalf("expected no bare title left, got:\n%s", got)
	}
}

// Ids that already exist are never reassigned: something may already point
// at one, and a reference that moves is worse than no reference at all.
func TestImportKeepsExistingLocalIDs(t *testing.T) {
	e := newTestEngine(t)
	ctx := context.Background()
	dir := t.TempDir()

	mustWriteFile(t, filepath.Join(dir, "Team", "t1.yaml"), "apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: t1\n  name: Team One\nspec:\n  name: Team One\n")
	mustWriteFile(t, filepath.Join(dir, "Project", "keep-ids.yaml"),
		"apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: keep-ids\n  name: Keep\nspec:\n  team: t1\n"+
			"  summary:\n    problems:\n      - problem: {situation: A gap}\n        change: {what: No more gap}\n"+
			"  resources:\n    - {id: r-original, role: owner, title: Quality reviewer}\n"+
			"  deliverables:\n    - id: dv-1\n      name: Training pack\n      acceptance: [{by: Quality reviewer, outcome: signs it off}]\n")

	if _, err := e.ImportDir(ctx, dir, "bootstrap", "legacy import"); err != nil {
		t.Fatalf("ImportDir returned an error: %v", err)
	}
	v, err := e.Get(ctx, "Project", "keep-ids")
	if err != nil {
		t.Fatal(err)
	}
	got := string(v.YAML)
	if !strings.Contains(got, "id: r-original") {
		t.Fatalf("expected the existing id kept, got:\n%s", got)
	}
	if strings.Contains(got, "id: quality-reviewer") {
		t.Fatalf("expected no second id derived from the title, got:\n%s", got)
	}
	if !strings.Contains(got, "id: r-original") || !strings.Contains(got, "local: resources") {
		t.Fatalf("expected the verifier to point at the existing id, got:\n%s", got)
	}
}
