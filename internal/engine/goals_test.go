package engine_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	codecyaml "github.com/ProjectCartograph/cartograph-engine/v2/internal/codec/yaml"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store/memory"
)

func TestGetSettingsDefault(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	s, err := e.GetSettings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(s.GoalLevels) != 3 || s.GoalLevels[0] != "Goal" || s.GoalLevels[1] != "Objective" || s.GoalLevels[2] != "Outcome" {
		t.Fatalf("expected default goal levels [Goal Objective Outcome], got %v", s.GoalLevels)
	}
	if s.ProjectLevelName != "Project" {
		t.Fatalf("expected default project level name Project, got %q", s.ProjectLevelName)
	}
}

func TestGetSettingsOverride(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	ctx := context.Background()
	y := "apiVersion: cartograph/v1\nkind: Settings\nmetadata:\n  id: default\n  name: Settings\nspec:\n  goalLevels: [Focus, Strategic]\n  projectLevelName: Initiative\n"
	if _, err := e.Commit(ctx, "Settings", "default", []byte(y), "p1", "seed"); err != nil {
		t.Fatal(err)
	}
	s, err := e.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.GoalLevels) != 2 || s.GoalLevels[0] != "Focus" || s.GoalLevels[1] != "Strategic" {
		t.Fatalf("expected overridden goal levels [Focus Strategic], got %v", s.GoalLevels)
	}
	if s.ProjectLevelName != "Initiative" {
		t.Fatalf("expected overridden project level name Initiative, got %q", s.ProjectLevelName)
	}
}

func TestSettingsSingletonID(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	ctx := context.Background()
	y := "apiVersion: cartograph/v1\nkind: Settings\nmetadata:\n  id: not-default\n  name: Settings\nspec:\n  goalLevels: [Focus, Strategic]\n"
	problems, err := e.Validate(ctx, "Settings", []byte(y))
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) == 0 {
		t.Fatal("expected a problem for a non-default Settings id, got none")
	}
}

// seedGoalTreeFixture commits a Team, a Pillar, a Strategic goal beneath
// it, and a Functional goal beneath the Strategic goal, plus a Project and a
// KPI aligned to the Functional goal, so the tree's shape and its aligned
// counts both have something real to compute from (I3.4a).
func seedGoalTreeFixture(t *testing.T, e *engine.Engine) {
	t.Helper()
	ctx := context.Background()
	commit := func(kind, id, y string) {
		t.Helper()
		if _, err := e.Commit(ctx, kind, id, []byte(y), "p1", "seed"); err != nil {
			t.Fatalf("seed %s/%s: %v", kind, id, err)
		}
	}
	commit("Goal", "strat-1", "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: strat-1\n  name: Pillar One\nspec:\n  level: goal\n  objective: Improve outcomes broadly\n")
	commit("Goal", "team-1", "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: team-1\n  name: Strategic Goal One\nspec:\n  level: objective\n  parent: strat-1\n  objective: Improve our own outcome\n  keyResults:\n    - id: kr-1\n      metric: Quality rate\n      direction: increase\n      kind: percent\n      baseline: {value: 62, date: \"2025-09\"}\n      target: {value: 75, date: \"2026-06\"}\n")
	commit("Goal", "func-1", "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: func-1\n  name: Functional Goal One\nspec:\n  level: outcome\n  parent: team-1\n  objective: Achieve a specific outcome\n")
	commit("Project", "proj-1", "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: proj-1\n  name: Project One\nspec:\n  team: t1\n  summary:\n    problems:\n      - problem: {situation: Too many gaps}\n        change: {what: Fewer gaps}\n  alignment:\n    goals: [func-1]\n")
	commit("KPI", "kpi-1", "apiVersion: cartograph/v1\nkind: KPI\nmetadata:\n  id: kpi-1\n  name: KPI One\nspec:\n  name: KPI One\n  definition: A measured thing\n  unit: percent\n  direction: increase\n  source: d1\n  goals: [func-1]\n")
}

func TestGoalTreeShape(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	seedGoalTreeFixture(t, e)

	tree, err := e.GoalTree(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(tree.Levels) != 3 || tree.Levels[0] != "Goal" || tree.Levels[1] != "Objective" || tree.Levels[2] != "Outcome" {
		t.Fatalf("expected default levels [Goal Objective Outcome], got %v", tree.Levels)
	}

	// seededEngine itself commits one Goal ("g1", pillar, no children);
	// seedGoalTreeFixture adds strat-1 with one child team-1, which has one
	// child func-1. Both pillars are roots.
	var stratNode *engine.GoalNode
	rootCount := 0
	for _, n := range tree.Nodes {
		rootCount++
		if n.ID == "strat-1" {
			stratNode = n
		}
	}
	if rootCount != 2 {
		t.Fatalf("expected 2 root goals (g1, strat-1), got %d: %+v", rootCount, tree.Nodes)
	}
	if stratNode == nil {
		t.Fatal("strat-1 not found among root nodes")
	}
	if len(stratNode.Children) != 1 {
		t.Fatalf("expected strat-1 to have 1 child, got %d", len(stratNode.Children))
	}
	teamNode := stratNode.Children[0]
	if teamNode.ID != "team-1" {
		t.Fatalf("expected child team-1, got %s", teamNode.ID)
	}
	if teamNode.Level != "objective" {
		t.Fatalf("expected level strategic, got %s", teamNode.Level)
	}
	if teamNode.Parent != "strat-1" {
		t.Fatalf("expected parent strat-1, got %s", teamNode.Parent)
	}
	if teamNode.KeyResults != 1 {
		t.Fatalf("expected 1 key result, got %d", teamNode.KeyResults)
	}
	if teamNode.Aligned.Projects != 0 {
		t.Fatalf("expected 0 aligned projects on strategic goal, got %d", teamNode.Aligned.Projects)
	}
	if teamNode.Aligned.KPIs != 0 {
		t.Fatalf("expected 0 aligned KPIs on strategic goal, got %d", teamNode.Aligned.KPIs)
	}
	if len(teamNode.Children) != 1 {
		t.Fatalf("expected 1 functional child, got %d", len(teamNode.Children))
	}
	funcNode := teamNode.Children[0]
	if funcNode.ID != "func-1" {
		t.Fatalf("expected functional child func-1, got %s", funcNode.ID)
	}
	if funcNode.Level != "outcome" {
		t.Fatalf("expected level functional, got %s", funcNode.Level)
	}
	if funcNode.Parent != "team-1" {
		t.Fatalf("expected parent team-1, got %s", funcNode.Parent)
	}
	if funcNode.Aligned.Projects != 1 {
		t.Fatalf("expected 1 aligned project, got %d", funcNode.Aligned.Projects)
	}
	if funcNode.Aligned.KPIs != 1 {
		t.Fatalf("expected 1 aligned KPI, got %d", funcNode.Aligned.KPIs)
	}
	if len(funcNode.Children) != 0 {
		t.Fatalf("expected no grandchildren, got %d", len(funcNode.Children))
	}
}

func TestGoalTreeEmpty(t *testing.T) {
	t.Parallel()
	e := newTestEngine(t)
	tree, err := e.GoalTree(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if tree.Nodes == nil {
		t.Fatal("expected a non-nil, empty Nodes slice")
	}
	if len(tree.Nodes) != 0 {
		t.Fatalf("expected an empty tree, got %d nodes", len(tree.Nodes))
	}
}

func checkByID(checks []engine.GoalCheck, id string) (engine.GoalCheck, bool) {
	for _, c := range checks {
		if c.ID == id {
			return c, true
		}
	}
	return engine.GoalCheck{}, false
}

func wantCheck(t *testing.T, checks []engine.GoalCheck, id, state string) engine.GoalCheck {
	t.Helper()
	c, ok := checkByID(checks, id)
	if !ok || c.State != state {
		t.Fatalf("expected %s %s, got %+v (found=%v) among %+v", state, id, c, ok, checks)
	}
	return c
}

// TestGoalChecksSmartMinimal: an outcome with a statement, under an
// objective, measured by an indicator with no target yet (seededEngine's
// g1-f and KPI k1). Specific holds; the rest ask for what is missing.
func TestGoalChecksSmartMinimal(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	checks, err := e.GoalChecks(context.Background(), "g1-f")
	if err != nil {
		t.Fatal(err)
	}
	wantCheck(t, checks, "smart-specific", "ok")
	wantCheck(t, checks, "smart-measurable", "warn")
	wantCheck(t, checks, "smart-time-bound", "warn")
	// Placed under an objective but with no rationale: not yet relevant.
	c := wantCheck(t, checks, "smart-relevant", "warn")
	if c.Fix == nil || c.Fix.Section != "whyItMatters" {
		t.Fatalf("expected the fix to point at the rationale, got %+v", c.Fix)
	}
}

// A digit in the statement fails Specific. Goal.Rules refuses such a
// statement on commit, so this writes to the store directly, the way a
// file written before the rule would look.
func TestGoalChecksSpecificWithDigit(t *testing.T) {
	t.Parallel()
	manifests := memory.NewManifestStore()
	e, err := engine.New(manifests, memory.NewOperationalStore(), engine.WithCodec(codecyaml.New()))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	y := "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: g5\n  name: Goal Five\nspec:\n  level: goal\n  objective: Reach 90 percent coverage\n"
	if err := manifests.PutVersion(ctx, store.Version{Kind: "Goal", ID: "g5", Number: 1, YAML: []byte(y), Actor: "p1", Reason: "legacy", On: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	checks, err := e.GoalChecks(ctx, "g5")
	if err != nil {
		t.Fatal(err)
	}
	c := wantCheck(t, checks, "smart-specific", "warn")
	if c.Fix == nil || c.Fix.Section != "objective" {
		t.Fatalf("expected fix section objective, got %+v", c.Fix)
	}
	// A goal with no vision or mission above it cannot be judged relevant.
	wantCheck(t, checks, "smart-relevant", "warn")
}

func TestGoalChecksSmartFullyMet(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	ctx := context.Background()
	y := "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: g6\n  name: Goal Six\nspec:\n  level: objective\n  parent: g1\n  objective: Raise our own outcome\n  whyItMatters: Customers leave after a second failure.\n  keyResults:\n    - id: kr-1\n      metric: Quality rate\n      direction: increase\n      kind: percent\n      baseline: {value: 62, date: \"2025-09\"}\n      target: {value: 75, date: \"2026-06\"}\n"
	if _, err := e.Commit(ctx, "Goal", "g6", []byte(y), "p1", "seed"); err != nil {
		t.Fatal(err)
	}
	checks, err := e.GoalChecks(ctx, "g6")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"smart-specific", "smart-measurable", "smart-attainable", "smart-relevant", "smart-time-bound"} {
		wantCheck(t, checks, id, "ok")
	}
	tree, err := e.GoalTree(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, n := range tree.Nodes {
		for _, c := range n.Children {
			if c.ID == "g6" {
				found = true
				if c.Smart != (engine.Smart{Specific: true, Measurable: true, Attainable: true, Relevant: true, TimeBound: true}) {
					t.Fatalf("tree smart = %+v, want all five", c.Smart)
				}
			}
		}
	}
	if !found {
		t.Fatal("g6 not in the tree")
	}
}

// An admitted unknown baseline still counts: the gap is named, not hidden.
func TestGoalChecksAttainableWithUnknownBaseline(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	ctx := context.Background()
	y := "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: g7\n  name: Goal Seven\nspec:\n  level: objective\n  parent: g1\n  objective: Raise our own outcome\n  keyResults:\n    - id: kr-1\n      metric: Quality rate\n      direction: increase\n      kind: percent\n      baseline: {unknownReason: not measured yet, expectedBy: \"2026-01\"}\n      target: {value: 75, date: \"2026-06\"}\n"
	if _, err := e.Commit(ctx, "Goal", "g7", []byte(y), "p1", "seed"); err != nil {
		t.Fatal(err)
	}
	checks, err := e.GoalChecks(ctx, "g7")
	if err != nil {
		t.Fatal(err)
	}
	wantCheck(t, checks, "smart-attainable", "ok")
}

func TestGoalChecksObjectiveMustSitUnderAGoal(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	ctx := context.Background()
	commit := func(kind, id, y string) {
		t.Helper()
		if _, err := e.Commit(ctx, kind, id, []byte(y), "p1", "seed"); err != nil {
			t.Fatalf("seed %s/%s: %v", kind, id, err)
		}
	}
	commit("Goal", "g8", "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: g8\n  name: Goal Eight\nspec:\n  level: objective\n  parent: g1\n  objective: A first objective\n  whyItMatters: It matters.\n")
	checks, err := e.GoalChecks(ctx, "g8")
	if err != nil {
		t.Fatal(err)
	}
	wantCheck(t, checks, "smart-relevant", "ok")
	// An objective under an objective is refused on commit.
	y := "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: g9\n  name: Goal Nine\nspec:\n  level: objective\n  parent: g8\n  objective: A grandchild\n"
	_, err = e.Commit(ctx, "Goal", "g9", []byte(y), "p1", "seed")
	var ve *engine.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *ValidationError, got %T: %v", err, err)
	}
	found := false
	for _, p := range ve.Problems {
		if strings.Contains(p.Message, "is level \"objective\", not \"goal\"") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected problem about parent level, got %+v", ve.Problems)
	}
}

func TestGoalChecksKeyResultsCountTooMany(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	ctx := context.Background()
	y := "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: g10\n  name: Goal Ten\nspec:\n  level: objective\n  parent: g1\n  objective: Too many measures\n  keyResults:\n" +
		"    - id: kr-1\n      metric: One\n      direction: increase\n      kind: percent\n" +
		"    - id: kr-2\n      metric: Two\n      direction: increase\n      kind: percent\n" +
		"    - id: kr-3\n      metric: Three\n      direction: increase\n      kind: percent\n" +
		"    - id: kr-4\n      metric: Four\n      direction: increase\n      kind: percent\n"
	if _, err := e.Commit(ctx, "Goal", "g10", []byte(y), "p1", "seed"); err != nil {
		t.Fatal(err)
	}
	checks, err := e.GoalChecks(ctx, "g10")
	if err != nil {
		t.Fatal(err)
	}
	wantCheck(t, checks, "key-results-count", "warn")
}

// One measure with a baseline and a dated target, one with neither: not
// attainable and not time-bound until both are.
func TestGoalChecksPartlyMeasured(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	ctx := context.Background()
	y := "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: g11\n  name: Goal Eleven\nspec:\n  level: objective\n  parent: g1\n  objective: Partly specified\n  keyResults:\n" +
		"    - id: kr-1\n      metric: One\n      direction: increase\n      kind: percent\n      baseline: {value: 10, date: \"2025-09\"}\n      target: {value: 20, date: \"2026-06\"}\n" +
		"    - id: kr-2\n      metric: Two\n      direction: increase\n      kind: percent\n"
	if _, err := e.Commit(ctx, "Goal", "g11", []byte(y), "p1", "seed"); err != nil {
		t.Fatal(err)
	}
	checks, err := e.GoalChecks(ctx, "g11")
	if err != nil {
		t.Fatal(err)
	}
	wantCheck(t, checks, "smart-measurable", "ok")
	wantCheck(t, checks, "smart-attainable", "warn")
	wantCheck(t, checks, "smart-time-bound", "warn")
}

func TestGoalChecksNotFound(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	_, err := e.GoalChecks(context.Background(), "does-not-exist")
	if err == nil {
		t.Fatal("expected an error for a missing goal")
	}
}

func TestDeleteGoalNotFound(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	err := e.DeleteGoal(context.Background(), "does-not-exist", "p1", "edited on the tree")
	if !errors.Is(err, engine.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestDeleteGoalUnreferencedSucceeds covers the plain path: a goal nothing
// references can be excluded (validated via DeleteGoal, which checks the leaf rule).
// The file stays; exclusion from vault.yaml is handled separately by the vault layer.
func TestDeleteGoalUnreferencedSucceeds(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	ctx := context.Background()
	mustCommit(t, e, "Goal", "g-lonely", "p1", "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: g-lonely\n  name: Lonely Goal\nspec:\n  level: objective\n  parent: g1\n  objective: Nothing points at this yet\n")

	if err := e.DeleteGoal(ctx, "g-lonely", "p1", "edited on the tree"); err != nil {
		t.Fatalf("expected the delete to succeed, got %v", err)
	}

	// The goal is still in the file (exclusion from vault.yaml is separate),
	// so GoalChecks still returns it. This test just validates the leaf rule check.
	if _, err := e.GoalChecks(ctx, "g-lonely"); err != nil {
		t.Fatalf("expected GoalChecks to still work (file not deleted), got %v", err)
	}

	tree, err := e.GoalTree(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// The goal is still in the tree (file not deleted, vault.yaml not updated).
	// Exclusion from vault.yaml is handled separately at the API/CLI layer.
	found := false
	for _, n := range tree.Nodes {
		if n.ID == "g-lonely" {
			found = true
			break
		}
		for _, c := range n.Children {
			if c.ID == "g-lonely" {
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatalf("expected the goal still present in tree (file not deleted)")
	}

	// Deleting an unexcluded goal a second time still succeeds (no state change yet).
	// Once vault.yaml exclusion is implemented, the second call would see found=false.
	if err := e.DeleteGoal(ctx, "g-lonely", "p1", "edited on the tree"); err != nil {
		t.Fatalf("expected the second delete to succeed (goal still in file), got %v", err)
	}
}

// TestDeleteGoalReferencedRefused covers the refusal path: a goal still
// referenced (here, by a child goal's own parent) is refused with a
// ValidationError naming what references it, and the goal survives.
func TestDeleteGoalReferencedRefused(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	ctx := context.Background()
	mustCommit(t, e, "Goal", "g-parent-of", "p1", "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: g-parent-of\n  name: Parent Goal\nspec:\n  level: objective\n  parent: g1\n  objective: A strategic goal with a child\n")
	mustCommit(t, e, "Goal", "g-child-of", "p1", "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: g-child-of\n  name: Child Goal\nspec:\n  level: outcome\n  parent: g-parent-of\n  objective: Serves g-parent-of\n")

	err := e.DeleteGoal(ctx, "g-parent-of", "p1", "edited on the tree")
	var ve *engine.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected a *ValidationError, got %T: %v", err, err)
	}
	if len(ve.Problems) == 0 {
		t.Fatal("expected at least one problem naming what references the goal")
	}
	found := false
	for _, p := range ve.Problems {
		if strings.Contains(p.Message, "g-child-of") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a problem naming g-child-of, got %+v", ve.Problems)
	}

	// The goal survives the refused delete.
	if _, err := e.GoalChecks(ctx, "g-parent-of"); err != nil {
		t.Fatalf("expected the goal to survive a refused delete, got %v", err)
	}
}

// An outcome may be defined before the objective it serves: left unplaced
// with a placeholder holding its parent's place, listed apart in the tree,
// told where it should go, and placed later (TAXONOMY.md D35).
func TestAGoalStandsUnplacedUntilPlaced(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	ctx := context.Background()
	bare := "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: kept-cool\n  name: Produce is kept cool\nspec:\n  level: outcome\n"
	if _, err := e.Commit(ctx, "Goal", "kept-cool", []byte(bare), "local", "test"); err == nil || !strings.Contains(err.Error(), "requires an objective") {
		t.Fatalf("an outcome with nothing holding its parent's place: %v", err)
	}
	held := "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: kept-cool\n  name: Produce is kept cool\n  pending:\n    - {path: /spec/parent, kind: Goal, name: Not placed yet}\nspec:\n  level: outcome\n"
	mustCommit(t, e, "Goal", "kept-cool", "local", held)

	tree, err := e.GoalTree(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Unplaced) != 1 || tree.Unplaced[0].ID != "kept-cool" {
		t.Fatalf("unplaced: %+v", tree.Unplaced)
	}
	for _, n := range tree.Nodes {
		if n.ID == "kept-cool" {
			t.Fatal("an unplaced outcome was listed as a goal")
		}
	}
	checks, err := e.GoalChecks(ctx, "kept-cool")
	if err != nil {
		t.Fatal(err)
	}
	var placed *engine.GoalCheck
	for i := range checks {
		if checks[i].ID == "placed" {
			placed = &checks[i]
		}
		if checks[i].ID == "pending" {
			t.Fatalf("the parent's placeholder was listed twice: %+v", checks[i])
		}
	}
	if placed == nil || placed.State != "warn" || !strings.Contains(placed.Message, "under an objective") {
		t.Fatalf("placed: %+v", placed)
	}

	// Placed: under an objective, the placeholder gone.
	mustCommit(t, e, "Goal", "kept-cool", "local", "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: kept-cool\n  name: Produce is kept cool\nspec:\n  level: outcome\n  parent: g1-s\n")
	tree, _ = e.GoalTree(ctx)
	if len(tree.Unplaced) != 0 {
		t.Fatalf("still unplaced: %+v", tree.Unplaced)
	}
}
