package engine_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/auth"
	codecyaml "github.com/ProjectCartograph/cartograph-engine/v2/internal/codec/yaml"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store/memory"
)

func portfolioYAML(id, extra string) string {
	return fmt.Sprintf("apiVersion: cartograph/v1\nkind: Portfolio\nmetadata:\n  id: %s\n  name: %s\n"+
		"spec:\n  aim: Fund the work that most raises what members earn\n  leadTeam: t1\n%s", id, id, extra)
}

func decisionsYAML(id, portfolio, decisions string) string {
	return fmt.Sprintf("apiVersion: cartograph/v1\nkind: PortfolioDecisions\nmetadata:\n  id: %s\n  name: %s\n"+
		"spec:\n  portfolio: %s\n  decisions:\n%s", id, id, portfolio, decisions)
}

func portfolioChecksByID(t *testing.T, e *engine.Engine, id string) map[string]engine.ProgrammeCheck {
	t.Helper()
	checks, err := e.PortfolioChecks(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]engine.ProgrammeCheck{}
	for _, c := range checks {
		out[c.ID] = c
	}
	return out
}

// A portfolio holds what names it, and its checks ask what it decides on:
// the objectives it is prioritised against, what it holds, and a decision
// for each of those and nothing else (TAXONOMY.md D32).
func TestAPortfolioDecidesOnWhatItHolds(t *testing.T) {
	e := seededEngine(t)
	mustCommit(t, e, "Portfolio", "invest", "local", portfolioYAML("invest", ""))
	c := portfolioChecksByID(t, e, "invest")
	if c["portfolio-objectives"].State != "warn" || c["portfolio-holds"].State != "warn" {
		t.Fatalf("a bare portfolio: %+v", c)
	}
	if _, asked := c["portfolio-decided"]; asked {
		t.Fatal("decisions were asked of a portfolio holding nothing")
	}

	mustCommit(t, e, "Portfolio", "invest", "local", portfolioYAML("invest", "  objectives: [g1, g1-s]\n"))
	mustCommit(t, e, "Programme", "quality", "local", programmeYAML("quality", "  portfolios: [invest]\n"))
	mustCommit(t, e, "Project", "app", "local", projectYAML("app", "  alignment:\n    goals: [g1-f]\n    portfolios: [invest]\n"))
	mustCommit(t, e, "Project", "other", "local", projectYAML("other", "  alignment:\n    goals: [g1-f]\n"))
	c = portfolioChecksByID(t, e, "invest")
	if c["portfolio-objectives"].State != "ok" {
		t.Fatalf("objectives: %+v", c["portfolio-objectives"])
	}
	if got := c["portfolio-holds"]; got.State != "ok" || got.Message != "1 programme and 1 project named." {
		t.Fatalf("holds: %+v", got)
	}
	if got := c["portfolio-decided"]; got.State != "warn" || !strings.Contains(got.Message, "No decision yet") {
		t.Fatalf("undecided: %+v", got)
	}

	// A decision about something it does not hold is advised first.
	mustCommit(t, e, "PortfolioDecisions", "review", "local", decisionsYAML("review", "invest",
		"    - {kind: Programme, id: quality, priority: 1, decision: invest}\n"+
			"    - {kind: Project, id: app, priority: 2, decision: hold}\n"+
			"    - {kind: Project, id: other, priority: 3, decision: stop}\n"))
	if got := portfolioChecksByID(t, e, "invest")["portfolio-decided"]; got.State != "warn" || !strings.Contains(got.Message, "does not name this portfolio") {
		t.Fatalf("a stranger decided on: %+v", got)
	}
	mustCommit(t, e, "PortfolioDecisions", "review", "local", decisionsYAML("review", "invest",
		"    - {kind: Programme, id: quality, priority: 1, decision: invest}\n"+
			"    - {kind: Project, id: app, priority: 2, decision: hold}\n"))
	if got := portfolioChecksByID(t, e, "invest")["portfolio-decided"]; got.State != "ok" {
		t.Fatalf("all decided: %+v", got)
	}
}

// What cannot be a portfolio's or a programme's shape is refused: a
// portfolio prioritised against an outcome, a loop in either nesting, and
// two decisions about one thing.
func TestPortfolioAndProgrammeShapes(t *testing.T) {
	e := seededEngine(t)
	ctx := context.Background()
	refused := func(kind, id, y, want string) {
		t.Helper()
		_, err := e.Commit(ctx, kind, id, []byte(y), "local", "test")
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s/%s: want a refusal saying %q, got %v", kind, id, want, err)
		}
	}
	refused("Portfolio", "pf", portfolioYAML("pf", "  objectives: [g1-f]\n"), "is an outcome")

	mustCommit(t, e, "Portfolio", "top", "local", portfolioYAML("top", ""))
	mustCommit(t, e, "Portfolio", "region", "local", portfolioYAML("region", "  portfolios: [top]\n"))
	refused("Portfolio", "top", portfolioYAML("top", "  portfolios: [region]\n"), "part of itself")

	mustCommit(t, e, "Programme", "big", "local", programmeYAML("big", ""))
	mustCommit(t, e, "Programme", "sub", "local", programmeYAML("sub", "  programmes: [big]\n"))
	refused("Programme", "big", programmeYAML("big", "  programmes: [sub]\n"), "part of itself")
	if got := programmeChecksByID(t, e, "big")["components-present"]; got.State != "ok" || got.Message != "1 sub-programme named." {
		t.Fatalf("a sub-programme is a component: %+v", got)
	}

	refused("PortfolioDecisions", "twice", decisionsYAML("twice", "top",
		"    - {kind: Portfolio, id: region, decision: invest}\n    - {kind: Portfolio, id: region, decision: stop}\n"), "one decision each")
}

// A programme without a theory of change is told it may be a portfolio.
func TestAProgrammeWithoutATheoryOfChangeMayBeAPortfolio(t *testing.T) {
	e := seededEngine(t)
	mustCommit(t, e, "Programme", "pg", "local", programmeYAML("pg", ""))
	if got := programmeChecksByID(t, e, "pg")["pathway-steps"]; got.State != "warn" || !strings.Contains(got.Message, "may be a portfolio") {
		t.Fatalf("pathway: %+v", got)
	}
}

// A portfolio's decisions belong to the team that governs the portfolio:
// a write to them is asked with that team's chain.
func TestPortfolioDecisionsBelongToItsLeadTeam(t *testing.T) {
	r := &recorder{}
	e, err := engine.New(memory.NewManifestStore(), memory.NewOperationalStore(), engine.WithCodec(codecyaml.New()), engine.WithAuthorizer(r))
	if err != nil {
		t.Fatal(err)
	}
	system := context.Background()
	for _, tm := range [][2]string{{"board", ""}, {"t1", "board"}} {
		if _, err := e.Commit(system, "Team", tm[0], team(tm[0], tm[1]), "directory", "set up"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.Commit(system, "Portfolio", "invest", []byte(portfolioYAML("invest", "")), "local", "set up"); err != nil {
		t.Fatal(err)
	}
	lee := auth.WithPrincipal(context.Background(), auth.Principal{Subject: "lee"})
	if err := e.PutWorking(lee, "PortfolioDecisions", "review", []byte(decisionsYAML("review", "invest", "    []\n"))); err != nil {
		t.Fatal(err)
	}
	a := r.seen[len(r.seen)-1]
	if a.Kind != "PortfolioDecisions" || !slices.Equal(a.Change.TeamsAfter, []string{"t1", "board"}) {
		t.Fatalf("asked: %+v %+v", a, a.Change)
	}
}
