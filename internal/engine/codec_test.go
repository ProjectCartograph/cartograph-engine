package engine_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	codecjson "github.com/ProjectCartograph/cartograph-engine/v2/internal/codec/json"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store/memory"
)

// The same manifests, written in JSON: the engine validates, commits,
// exports and re-imports them without knowing the syntax changed. This is
// what the codec port guarantees, so it is tested at the engine, not at
// an adapter.
func TestEngineReadsAndWritesJSONManifests(t *testing.T) {
	e, err := engine.New(memory.NewManifestStore(), memory.NewOperationalStore(), engine.WithCodec(codecjson.New()))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	team := []byte(`{"apiVersion":"cartograph/v1","kind":"Team","metadata":{"id":"t1","name":"Quality team"},"spec":{"description":"Checks the produce"}}`)
	if problems, err := e.Validate(ctx, "Team", team); err != nil || len(problems) != 0 {
		t.Fatalf("validate: %v %v", problems, err)
	}
	v, err := e.Commit(ctx, "Team", "t1", team, "a", "first")
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if v.Number != 1 || !strings.Contains(string(v.YAML), `"Quality team"`) {
		t.Fatalf("the stored text is what was written: %+v", v)
	}

	// A reference in JSON is found the same way: by walking the schema.
	ds := []byte(`{"apiVersion":"cartograph/v1","kind":"DataSource","metadata":{"id":"d1","name":"Register"},"spec":{"name":"Register","team":"nope"}}`)
	problems, err := e.Validate(ctx, "DataSource", ds)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range problems {
		if strings.Contains(p.Message, `references Team "nope"`) {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the missing reference to be named, got %v", problems)
	}

	// Export writes .json files; import reads them back, one manifest per file.
	dir := t.TempDir()
	if err := e.ExportDir(ctx, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Team", "t1.json")); err != nil {
		t.Fatalf("export should write Team/t1.json: %v", err)
	}
	e2, _ := engine.New(memory.NewManifestStore(), memory.NewOperationalStore(), engine.WithCodec(codecjson.New()))
	report, err := e2.ImportDir(ctx, dir, "a", "round trip")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Problems) != 0 || len(report.Imported) != 1 {
		t.Fatalf("import: %+v", report)
	}
	got, err := e2.Get(ctx, "Team", "t1")
	if err != nil || string(got.YAML) != string(team) {
		t.Fatalf("round trip changed the text: %q %v", got.YAML, err)
	}
}
