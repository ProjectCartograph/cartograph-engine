package engine_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// A budget is declared once and referenced, so two projects drawing on the
// same budget are visibly drawing on the same budget.
func TestFundingDrawsOnADeclaredBudget(t *testing.T) {
	e := seededEngine(t)
	ctx := context.Background()

	mustCommit(t, e, "FundingSource", "operating-budget", "local",
		"apiVersion: cartograph/v1\nkind: FundingSource\nmetadata:\n  id: operating-budget\n  name: Operating budget\n"+
			"spec:\n  name: Operating budget\n  code: \"26-02-001\"\n  heldBy: r1\n  period: \"2026\"\n")

	mustCommit(t, e, "Project", "funded", "local",
		projectYAML("funded", "  funding:\n    - {amount: 1000, currency: TTD, source: operating-budget, status: approved}\n"))

	// A budget the vault does not hold is refused, which is the whole
	// point of it being a reference.
	if _, err := e.Commit(ctx, "Project", "misfunded", []byte(projectYAML("misfunded",
		"  funding:\n    - {amount: 1000, currency: TTD, source: no-such-budget, status: approved}\n")),
		"local", "seed"); err == nil {
		t.Fatal("expected a reference to a budget that does not exist to be refused")
	}

	// An amount nobody has found a budget for yet is a real state.
	mustCommit(t, e, "Project", "unbudgeted", "local",
		projectYAML("unbudgeted", "  funding:\n    - {amount: 1000, currency: TTD, status: requested}\n"))

	// And a currency outside the standard is refused, which is why it is
	// picked rather than typed.
	if _, err := e.Commit(ctx, "Project", "bad-currency", []byte(projectYAML("bad-currency",
		"  funding:\n    - {amount: 1000, currency: ZZZ, status: approved}\n")), "local", "seed"); err == nil {
		t.Fatal("expected a currency outside ISO 4217 to be refused")
	}
}

// A file written before budgets were a kind names its budget in prose.
// Prose cannot become a reference without inventing a manifest, so it is
// reported rather than guessed at, and the amount survives.
func TestLegacyFundingReportsTheProseItCannotResolve(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "Team", "t1.yaml"),
		"apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: t1\n  name: Team One\nspec:\n  name: Team One\n")
	mustWriteFile(t, filepath.Join(dir, "Project", "old-funding.yaml"),
		"apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: old-funding\n  name: P\nspec:\n  team: t1\n"+
			"  summary:\n    problems:\n      - problem: {situation: A gap}\n        change: {what: No more gap}\n"+
			"  funding:\n    - amount: 42000\n      currency: USD\n      source: Cooperative operating budget\n"+
			"      reference: BUD-2025-Q4-07\n      status: approved\n")

	e, cleanup := seededEngineWithVault(t, dir)
	defer cleanup()

	v, err := e.Get(context.Background(), "Project", "old-funding")
	if err != nil {
		t.Fatal(err)
	}
	read := string(v.YAML)
	// What the line was for survives.
	for _, want := range []string{"amount: 42000", "currency: USD", "status: approved"} {
		if !strings.Contains(read, want) {
			t.Fatalf("expected %q to survive, got:\n%s", want, read)
		}
	}
	// The prose does not, because it cannot be a reference.
	for _, gone := range []string{"Cooperative operating budget", "BUD-2025-Q4-07"} {
		if strings.Contains(read, gone) {
			t.Fatalf("expected %q dropped, got:\n%s", gone, read)
		}
	}
}
