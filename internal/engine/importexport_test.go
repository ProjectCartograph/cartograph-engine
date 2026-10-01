package engine_test

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/internal/engine"
)

// exampleDir locates cartograph/examples/minimal relative to this package
// (github.com/ProjectCartograph/cartograph-engine/internal/engine), without hard-coding an absolute path.
func exampleDir(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(wd, "..", "..", "examples", "minimal")
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("example directory not found at %s: %v", dir, err)
	}
	return dir
}

func TestImportExampleInstance(t *testing.T) {
	e := newTestEngine(t)
	ctx := context.Background()

	report, err := e.ImportDir(ctx, exampleDir(t), "alice-nkemah", "seed the example instance")
	if err != nil {
		t.Fatalf("ImportDir returned an error: %v", err)
	}
	if len(report.Problems) != 0 {
		t.Fatalf("expected a clean import, got problems: %+v", report.Problems)
	}
	// Every manifest in the directory imports. A fixed count went stale the
	// first time the example grew, and never said what it was protecting:
	// that nothing is silently dropped on the way in.
	if files := countManifestFiles(t, exampleDir(t)); len(report.Imported) != files {
		t.Fatalf("expected all %d manifests to import, got %d: %+v", files, len(report.Imported), report.Imported)
	}

	// The example exists to be walked through. Every kind the project
	// definition flow reaches for has to be in it, or a step of that walk
	// is a dead end with nothing to pick.
	for _, kind := range []string{
		"Team", "Resource", "DataSource", "BeneficiaryGroup",
		"ReportingCycle", "Goal", "KPI", "Programme", "Operation", "Project",
	} {
		got, err := e.List(ctx, kind, engine.Filter{}, false)
		if err != nil {
			t.Fatalf("listing %s: %v", kind, err)
		}
		if len(got) == 0 {
			t.Errorf("the example holds no %s, so the step that picks one has nothing to offer", kind)
		}
	}

	proj, err := e.Get(ctx, "Project", "quality-check-rollout")
	if err != nil {
		t.Fatalf("expected the project to be committed: %v", err)
	}
	if proj.Number != 1 {
		t.Fatalf("expected version 1, got %d", proj.Number)
	}

	refs, err := e.References(ctx, "Project", "quality-check-rollout")
	if err != nil {
		t.Fatal(err)
	}
	wantKinds := map[string]bool{"Team": false, "Goal": false, "Programme": false, "Operation": false, "DataSource": false, "KPI": false, "BeneficiaryGroup": false}
	for _, r := range refs.Outgoing {
		if _, ok := wantKinds[r.Kind]; ok {
			wantKinds[r.Kind] = true
		}
	}
	for k, ok := range wantKinds {
		if !ok {
			t.Errorf("expected the project to reference something of kind %s", k)
		}
	}
}

func TestExportRoundTripIsReproducible(t *testing.T) {
	e := newTestEngine(t)
	ctx := context.Background()
	src := exampleDir(t)

	if _, err := e.ImportDir(ctx, src, "alice-nkemah", "seed"); err != nil {
		t.Fatal(err)
	}

	out1 := t.TempDir()
	if err := e.ExportDir(ctx, out1); err != nil {
		t.Fatal(err)
	}

	// Re-import what was just exported into a fresh engine, then export
	// again: export -> import -> export must give byte-identical output.
	e2 := newTestEngine(t)
	report, err := e2.ImportDir(ctx, out1, "alice-nkemah", "reimport")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Problems) != 0 {
		t.Fatalf("expected a clean reimport, got %+v", report.Problems)
	}

	out2 := t.TempDir()
	if err := e2.ExportDir(ctx, out2); err != nil {
		t.Fatal(err)
	}

	compareDirs(t, out1, out2)
}

func compareDirs(t *testing.T, a, b string) {
	t.Helper()
	var filesA []string
	if err := filepath.WalkDir(a, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(a, p)
			filesA = append(filesA, rel)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(filesA) == 0 {
		t.Fatal("expected exported files, found none")
	}
	for _, rel := range filesA {
		ca, err := os.ReadFile(filepath.Join(a, rel))
		if err != nil {
			t.Fatal(err)
		}
		cb, err := os.ReadFile(filepath.Join(b, rel))
		if err != nil {
			t.Fatalf("%s missing from second export: %v", rel, err)
		}
		if string(ca) != string(cb) {
			t.Fatalf("%s differs between exports:\n--- first ---\n%s\n--- second ---\n%s", rel, ca, cb)
		}
	}
}

func TestImportRejectsWholeBatchOnOneBadFile(t *testing.T) {
	e := newTestEngine(t)
	ctx := context.Background()

	dir := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(dir, "Team"), 0o755))
	must(t, os.WriteFile(filepath.Join(dir, "Team", "t1.yaml"),
		[]byte("apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: t1\n  name: Team One\nspec:\n  name: Team One\n"), 0o644))
	must(t, os.MkdirAll(filepath.Join(dir, "Resource"), 0o755))
	must(t, os.WriteFile(filepath.Join(dir, "Resource", "r1.yaml"),
		[]byte("apiVersion: cartograph/v1\nkind: Resource\nmetadata:\n  id: r1\n  name: Resource One\nspec:\n  name: Resource One\n  category: does-not-exist\n"), 0o644))

	report, err := e.ImportDir(ctx, dir, "p1", "test")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Problems) == 0 {
		t.Fatal("expected a problem for the dangling team reference")
	}
	if len(report.Imported) != 0 {
		t.Fatalf("expected nothing imported when any file has a problem, got %+v", report.Imported)
	}
	if _, err := e.Get(ctx, "Team", "t1"); err == nil {
		t.Fatal("expected the whole batch to be rejected, including the otherwise-valid Team")
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// countManifestFiles counts the YAML files under dir, the number the
// example is expected to import in full.
func countManifestFiles(t *testing.T, dir string) int {
	t.Helper()
	n := 0
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// .cartograph is the vault's own bookkeeping: an index, a journal
			// and staged drafts, none of them manifests.
			if d.Name() == ".cartograph" {
				return filepath.SkipDir
			}
			return nil
		}
		// vault.yaml is the vault's own index, which serve regenerates. The
		// importer skips it; counting it here made this fail because a
		// server had been run against the example once.
		if rel, relErr := filepath.Rel(dir, path); relErr == nil && rel == "vault.yaml" {
			return nil
		}
		if ext := filepath.Ext(path); ext == ".yaml" || ext == ".yml" {
			n++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", dir, err)
	}
	return n
}
