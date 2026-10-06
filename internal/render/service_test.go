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

// A stakeholder table names a beneficiary group as readily as a resource,
// with its stake and the role that owns the relationship (TAXONOMY.md
// D42).
func TestStakeholdersShowGroupsStakesAndOwners(t *testing.T) {
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
	put("BeneficiaryGroup", "staff", "apiVersion: cartograph/v1\nkind: BeneficiaryGroup\nmetadata:\n  id: staff\n  name: Depot staff\nspec: {}\n")
	put("Operation", "checks", "apiVersion: cartograph/v1\nkind: Operation\nmetadata:\n  id: checks\n  name: Quality Check Service\nspec:\n  purpose: Check every delivery\n  team: t1\n")
	put("StakeholderMap", "checks-map", "apiVersion: cartograph/v1\nkind: StakeholderMap\nmetadata:\n  id: checks-map\n  name: Map\nspec:\n  scope: {kind: Operation, id: checks}\n  entries:\n    - group: staff\n      stake: Wants intake no slower than today\n      owner: {external: Operations lead}\n")
	html, err := OperationCharter(ctx, e, "checks")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Depot staff", "Wants intake no slower than today", "Operations lead", "Relationship owner"} {
		if !strings.Contains(string(html), want) {
			t.Errorf("the stakeholder table lacks %q", want)
		}
	}
}
