package render

import (
	"context"
	"strings"
	"testing"

	codecyaml "github.com/ProjectCartograph/cartograph-engine/v2/internal/codec/yaml"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store/memory"
)

// A charter says where a service is in its life (TAXONOMY.md D30): the
// service's own charter gives its status, and a project that sets up a
// planned service says so where it names what it hands over to.
func TestChartersSayWhereAServiceIs(t *testing.T) {
	ctx := context.Background()
	e, err := engine.New(memory.NewManifestStore(), memory.NewOperationalStore(), engine.WithCodec(codecyaml.New()))
	if err != nil {
		t.Fatal(err)
	}
	put := func(kind, id, y string) {
		t.Helper()
		if err := e.PutWorking(ctx, kind, id, []byte(y)); err != nil {
			t.Fatal(err)
		}
	}
	put("Operation", "checks", "apiVersion: cartograph/v1\nkind: Operation\nmetadata:\n  id: checks\n  name: Quality Check Service\nspec:\n  purpose: Check every delivery\n  status: planned\n  team: t1\n")
	put("Operation", "collection", "apiVersion: cartograph/v1\nkind: Operation\nmetadata:\n  id: collection\n  name: Collection Service\nspec:\n  purpose: Collect produce\n  team: t1\n")
	put("Project", "rollout", "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: rollout\n  name: Quality Check Rollout\nspec:\n  team: t1\n  operation: checks\n")

	for id, want := range map[string]string{"checks": "Planned", "collection": "Running"} {
		html, err := OperationCharter(ctx, e, id)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(html), want) {
			t.Errorf("the %s charter does not say %s", id, want)
		}
	}
	html, _, err := Charter(ctx, e, "rollout", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(html), "Quality Check Service (planned, set up by this project)") {
		t.Errorf("the project charter does not say its service is planned")
	}
}

// A service's running costs are printed with the period each is for
// (TAXONOMY.md D39).
func TestServiceChartersPrintRunningCosts(t *testing.T) {
	ctx := context.Background()
	e, err := engine.New(memory.NewManifestStore(), memory.NewOperationalStore(), engine.WithCodec(codecyaml.New()))
	if err != nil {
		t.Fatal(err)
	}
	y := "apiVersion: cartograph/v1\nkind: Operation\nmetadata:\n  id: checks\n  name: Quality Check Service\nspec:\n  purpose: Check every delivery\n  status: planned\n  team: t1\n  funding:\n    - {amount: 85000, currency: USD, per: year, status: requested}\n"
	if err := e.PutWorking(ctx, "Operation", "checks", []byte(y)); err != nil {
		t.Fatal(err)
	}
	html, err := OperationCharter(ctx, e, "checks")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Running costs", "USD 85,000 a year"} {
		if !strings.Contains(string(html), want) {
			t.Errorf("the service charter lacks %q", want)
		}
	}
}
