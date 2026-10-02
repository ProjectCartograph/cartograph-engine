package computed_test

import (
	"context"
	"path/filepath"
	"testing"

	codecyaml "github.com/ProjectCartograph/cartograph-engine/v2/internal/codec/yaml"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/reporting"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/reporting/computed"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store/memory"
)

// Every standard report, over the example, has rows under its columns,
// and an unknown one is refused.
func TestReportsOverTheExample(t *testing.T) {
	ctx := context.Background()
	ms := memory.NewManifestStore()
	e, err := engine.New(ms, memory.NewOperationalStore().LogTo(ms), engine.WithCodec(codecyaml.New()))
	if err != nil {
		t.Fatal(err)
	}
	dir, _ := filepath.Abs("../../../examples/minimal")
	if rep, err := e.ImportDir(ctx, dir, "seed@example.org", "the example"); err != nil || len(rep.Problems) > 0 {
		t.Fatalf("import: %v %+v", err, rep.Problems)
	}
	r := computed.Reporter{E: e}
	for _, name := range r.Names() {
		tab, err := r.Run(ctx, name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(tab.Rows) == 0 {
			t.Errorf("%s has no rows", name)
		}
		for _, row := range tab.Rows {
			if len(row) != len(tab.Columns) {
				t.Fatalf("%s: %d values under %d columns", name, len(row), len(tab.Columns))
			}
		}
	}
	if _, err := r.Run(ctx, "nope"); err == nil {
		t.Error("an unknown report was answered")
	}
	var _ reporting.Reporter = r
}
