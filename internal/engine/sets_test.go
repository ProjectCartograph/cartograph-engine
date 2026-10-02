package engine_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	codecyaml "github.com/ProjectCartograph/cartograph-engine/v2/internal/codec/yaml"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store/memory"
)

// countingStore counts the reads that cost a round trip in a database.
type countingStore struct {
	*memory.ManifestStore
	mu    sync.Mutex
	calls int
}

func (c *countingStore) count() {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
}

func (c *countingStore) GetCurrent(ctx context.Context, kind, id string) (store.Version, bool, error) {
	c.count()
	return c.ManifestStore.GetCurrent(ctx, kind, id)
}

func (c *countingStore) ListReferencing(ctx context.Context, toKind, toID string) ([]store.Summary, error) {
	c.count()
	return c.ManifestStore.ListReferencing(ctx, toKind, toID)
}

func (c *countingStore) ListIDs(ctx context.Context, kind string) ([]string, error) {
	c.count()
	return c.ManifestStore.ListIDs(ctx, kind)
}

func (c *countingStore) CurrentOfKind(ctx context.Context, kind string) ([]store.Version, error) {
	c.count()
	return c.ManifestStore.CurrentOfKind(ctx, kind)
}

func (c *countingStore) ReferencingKind(ctx context.Context, toKind string) (map[string][]store.Summary, error) {
	c.count()
	return c.ManifestStore.ReferencingKind(ctx, toKind)
}

// oneAtATime is a store that answers no sets, so the engine loops.
type oneAtATime struct{ store.ManifestStore }

// seedGoals commits a goal, n objectives under it, and an indicator
// aligned to each objective.
func seedGoals(t *testing.T, e *engine.Engine, n int) {
	t.Helper()
	ctx := context.Background()
	commit := func(kind, id, y string) {
		t.Helper()
		if _, err := e.Commit(ctx, kind, id, []byte(y), "p1", "seed"); err != nil {
			t.Fatalf("seed %s/%s: %v", kind, id, err)
		}
	}
	if _, err := e.SeedStandardUnits(ctx); err != nil {
		t.Fatal(err)
	}
	commit("Team", "t1", "apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: t1\n  name: Team One\nspec:\n  name: Team One\n")
	commit("DataSource", "d1", "apiVersion: cartograph/v1\nkind: DataSource\nmetadata:\n  id: d1\n  name: Source\nspec:\n  name: Source\n  category: database\n  team: t1\n")
	commit("Goal", "top", "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: top\n  name: Top\nspec:\n  level: goal\n  objective: Improve outcomes broadly\n  horizon: {start: \"2025\", end: \"2030\"}\n")
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("o%02d", i)
		commit("Goal", id, fmt.Sprintf("apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: %s\n  name: Objective %d\nspec:\n  level: objective\n  parent: top\n  objective: Improve the outcome for area %c\n  whyItMatters: Because\n", id, i, rune('a'+i)))
		commit("KPI", "k"+id, fmt.Sprintf("apiVersion: cartograph/v1\nkind: KPI\nmetadata:\n  id: k%s\n  name: Indicator %d\nspec:\n  name: Indicator %d\n  definition: A measured thing\n  unit: percent\n  direction: increase\n  source: d1\n  goals: [%s]\n", id, i, i, id))
	}
}

// The goal tree reads the store a fixed number of times, however many
// goals there are (docs/adr/0012): no read per goal.
func TestGoalTreeReadsAreFixed(t *testing.T) {
	calls := func(n int) int {
		s := &countingStore{ManifestStore: memory.NewManifestStore()}
		e, err := engine.New(s, memory.NewOperationalStore(), engine.WithCodec(codecyaml.New()))
		if err != nil {
			t.Fatal(err)
		}
		seedGoals(t, e, n)
		s.calls = 0
		if _, err := e.GoalTree(context.Background()); err != nil {
			t.Fatal(err)
		}
		return s.calls
	}
	few, many := calls(2), calls(20)
	if few != many {
		t.Fatalf("the goal tree read the store %d times for 3 goals and %d for 21", few, many)
	}
}

// Read in sets or one at a time, the tree is the same.
func TestGoalTreeSameEitherWay(t *testing.T) {
	tree := func(s store.ManifestStore) string {
		e, err := engine.New(s, memory.NewOperationalStore(), engine.WithCodec(codecyaml.New()))
		if err != nil {
			t.Fatal(err)
		}
		seedGoals(t, e, 5)
		got, err := e.GoalTree(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(got)
		return string(b)
	}
	sets := tree(memory.NewManifestStore())
	loops := tree(oneAtATime{memory.NewManifestStore()})
	if sets != loops {
		t.Fatalf("in sets:\n%s\none at a time:\n%s", sets, loops)
	}
	if want := `"kpis":1`; !contains(sets, want) {
		t.Fatalf("no objective counted its aligned indicator: %s", sets)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// Saving a project that has a working copy numbers the version after the
// highest saved one; the working copy has no number of its own.
func TestCommitProjectOverAWorkingCopy(t *testing.T) {
	e := seededEngine(t)
	ctx := context.Background()
	text := "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: p9\n  name: Project Nine\nspec:\n  team: t1\n  summary:\n    problems:\n      - problem: {situation: Too many gaps}\n        change: {what: Fewer gaps}\n"
	if _, err := e.CommitProject(ctx, "p9", []byte(text), "p1", "first"); err != nil {
		t.Fatal(err)
	}
	if err := e.PutWorking(ctx, "Project", "p9", []byte(text)); err != nil {
		t.Fatal(err)
	}
	v, err := e.CommitProject(ctx, "p9", []byte(text), "p1", "second")
	if err != nil {
		t.Fatalf("second save over a working copy: %v", err)
	}
	if v.Number != 2 {
		t.Fatalf("second save is version %d, want 2", v.Number)
	}
}
