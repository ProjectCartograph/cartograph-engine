package engine_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
)

// A high-impact risk names the role that owns it, on a project and on a
// programme; a lower one may go unowned (TAXONOMY.md D41).
func TestAHighImpactRiskNamesItsOwner(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	ctx := context.Background()
	risks := func(owner string) string {
		y := "  resources:\n    - {id: lead, role: manager}\n  risks:\n    - {id: r1, description: Depots skip training, type: risk, impact: high, likelihood: medium, mitigation: Train before the season}\n    - {id: r2, description: Slides arrive late, type: risk, impact: low, likelihood: low}\n"
		if owner != "" {
			y = strings.Replace(y, "mitigation: Train before the season}", "mitigation: Train before the season, owner: "+owner+"}", 1)
		}
		return y
	}

	project := func(owner string) engine.ProjectCheckItem {
		t.Helper()
		if err := e.PutWorking(ctx, "Project", "pr", []byte(projectYAML("pr", risks(owner)))); err != nil {
			t.Fatal(err)
		}
		checks, err := e.ProjectChecks(ctx, "pr", false)
		if err != nil {
			t.Fatal(err)
		}
		return checksByID(checks.Items)["risks-owned"]
	}
	if c := project(""); c.State != "warn" || c.Message != "1 high-impact risk names no owner." {
		t.Fatalf("an unowned high risk on a project: %+v", c)
	}
	if c := project("{local: resources, id: lead}"); c.State != "ok" {
		t.Fatalf("an owned high risk on a project: %+v", c)
	}
	if _, err := e.Commit(ctx, "Project", "pr", []byte(projectYAML("pr", risks("{local: resources, id: nobody}"))), "p1", "test"); !problemAt(err, "/spec/risks/0/owner/id") {
		t.Fatalf("an owner no role on the project holds was accepted: %+v", err)
	}

	programme := func(owner string) engine.ProgrammeCheck {
		t.Helper()
		if err := e.PutWorking(ctx, "Programme", "pg", []byte(programmeYAML("pg", strings.Replace(risks(owner), "  resources:\n    - {id: lead, role: manager}\n", "", 1)))); err != nil {
			t.Fatal(err)
		}
		checks, err := e.ProgrammeChecks(ctx, "pg")
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range checks {
			if c.ID == "risks-owned" {
				return c
			}
		}
		return engine.ProgrammeCheck{}
	}
	if c := programme(""); c.State != "warn" {
		t.Fatalf("an unowned high risk on a programme: %+v", c)
	}
	if c := programme("{external: Programme board}"); c.State != "ok" {
		t.Fatalf("an owned high risk on a programme: %+v", c)
	}
}

func problemAt(err error, path string) bool {
	var ve *engine.ValidationError
	if !errors.As(err, &ve) {
		return false
	}
	for _, p := range ve.Problems {
		if p.Path == path {
			return true
		}
	}
	return false
}
