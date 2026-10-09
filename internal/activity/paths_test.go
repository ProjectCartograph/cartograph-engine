package activity

import (
	"testing"
)

// A screen gets harder to use only on purpose: a change that lengthens a
// task's shortest path fails here until paths.json says the new length,
// in the same change (just ux-budget writes it).
func TestNoTaskOutgrowsItsBudget(t *testing.T) {
	t.Parallel()
	flows, err := Flows()
	if err != nil {
		t.Fatal(err)
	}
	paths, err := Paths(flows)
	if err != nil {
		t.Fatal(err)
	}
	budget, err := Budget()
	if err != nil {
		t.Fatal(err)
	}
	for task, n := range paths {
		b, ok := budget[task]
		switch {
		case !ok:
			t.Errorf("%s has no budget: run just ux-budget and commit paths.json (its shortest path is %d)", task, n)
		case n > b:
			t.Errorf("%s now takes %d presses and answers, %d more than its budget of %d: shorten it, or run just ux-budget and say why in the pull request", task, n, n-b, b)
		}
	}
	for task := range budget {
		if _, ok := paths[task]; !ok {
			t.Errorf("paths.json budgets %s, which no flow has: run just ux-budget", task)
		}
	}
}

func TestAGoalsShortestPathFollowsItsSchema(t *testing.T) {
	t.Parallel()
	flows, err := Flows()
	if err != nil {
		t.Fatal(err)
	}
	paths, err := Paths(flows)
	if err != nil {
		t.Fatal(err)
	}
	// Open, Next past four of five steps, the level and the name the
	// schema requires, and save.
	if got := paths["define/Goal"]; got != 1+4+2+1 {
		t.Fatalf("define/Goal is %d", got)
	}
}
