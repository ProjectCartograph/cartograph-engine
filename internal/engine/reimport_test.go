package engine_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Importing again over a manifest someone has a working copy of adds the
// next version after the highest saved one. The working copy has no number
// of its own, so it must not decide the next one.
func TestReimportAfterAWorkingCopy(t *testing.T) {
	e := newTestEngine(t)
	ctx := context.Background()
	dir := t.TempDir()
	team := "apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: quality\n  name: Quality\nspec:\n  name: Quality\n"
	if err := os.MkdirAll(filepath.Join(dir, "Team"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Team", "quality.yaml"), []byte(team), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ImportDir(ctx, dir, "seed", "first"); err != nil {
		t.Fatal(err)
	}
	if err := e.PutWorking(ctx, "Team", "quality", []byte(team)); err != nil {
		t.Fatal(err)
	}
	report, err := e.ImportDir(ctx, dir, "seed", "again")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Imported) != 1 || report.Imported[0].Version != 2 {
		t.Fatalf("imported: %+v", report.Imported)
	}
}

// A directory mounted from a Kubernetes ConfigMap holds each file in a
// hidden, timestamped directory and links to it beside it. The import
// reads each manifest once, through the link, and nothing hidden.
func TestImportSkipsHiddenEntries(t *testing.T) {
	e := newTestEngine(t)
	dir := t.TempDir()
	team := "apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: quality\n  name: Quality\nspec:\n  name: Quality\n"
	hidden := filepath.Join(dir, "..2026_10_01_00_00_00.000000001")
	if err := os.MkdirAll(hidden, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hidden, "quality.yaml"), []byte(team), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(hidden, "quality.yaml"), filepath.Join(dir, "quality.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".draft.yaml"), []byte(team), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := e.ImportDir(context.Background(), dir, "seed", "from a ConfigMap")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Problems) != 0 || len(report.Imported) != 1 {
		t.Fatalf("imported %+v, problems %+v", report.Imported, report.Problems)
	}
}
