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
