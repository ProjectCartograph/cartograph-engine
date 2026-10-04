package engine_test

import (
	"context"
	"strings"
	"testing"

	codecyaml "github.com/ProjectCartograph/cartograph-engine/v2/internal/codec/yaml"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/decide"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store/memory"
)

// model answers as a decision model might: a statement starting with a
// verb is an action, not a state; a choice picks what answer says.
type model struct {
	answer func(state string, q decide.Question) decide.Answer
	asked  int
}

func (m *model) Decide(_ context.Context, state string, qs map[string]decide.Question) (map[string]decide.Answer, error) {
	m.asked++
	out := map[string]decide.Answer{}
	for name, q := range qs {
		out[name] = m.answer(state, q)
	}
	return out, nil
}

func engineWith(t *testing.T, d decide.Decider) *engine.Engine {
	t.Helper()
	e, err := engine.New(memory.NewManifestStore(), memory.NewOperationalStore(), engine.WithCodec(codecyaml.New()), engine.WithDecider(d))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.ImportDir(context.Background(), exampleDir(t), "alice-nkemah", "seed"); err != nil {
		t.Fatal(err)
	}
	return e
}

// With a decision model, a statement is judged by what it says, as a
// contrast between the guidance's good and poor options, good first;
// without one, the check is not asked and nothing else changes
// (docs/adr/0023).
func TestAStatementIsJudgedByWhatItSays(t *testing.T) {
	ctx := context.Background()
	var order []string
	m := &model{answer: func(state string, q decide.Question) decide.Answer {
		order = nil
		for _, o := range q.Options {
			order = append(order, o.Key)
		}
		action := strings.HasPrefix(strings.ToLower(state), "reduce")
		p := map[string]float64{"state": 0.9, "action": 0.1}
		if action {
			p = map[string]float64{"state": 0.1, "action": 0.9}
		}
		return decide.Answer{Probabilities: p}
	}}
	e := engineWith(t, m)
	outcome := "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: o-fast\n  name: Faster delivery\nspec:\n  level: outcome\n  parent: depots-stay-open-through-the-season\n  objective: Reduce delivery time to customers\n"
	mustCommit(t, e, "Goal", "o-fast", "local", outcome)
	checks, err := e.GoalChecks(ctx, "o-fast")
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]engine.GoalCheck{}
	for _, c := range checks {
		byID[c.ID] = c
	}
	if c := byID["statement-state"]; c.State != "warn" || !strings.Contains(c.Message, "action") {
		t.Fatalf("an outcome written as an action: %+v", c)
	}
	if strings.Join(order, " ") != "state action" {
		t.Fatalf("the good option was not asked first: %v", order)
	}
	before := m.asked
	if _, err := e.GoalChecks(ctx, "o-fast"); err != nil {
		t.Fatal(err)
	}
	if m.asked != before {
		t.Fatalf("the same statement was asked again: %d then %d", before, m.asked)
	}

	plain := engineWith(t, nil)
	mustCommit(t, plain, "Goal", "o-fast", "local", outcome)
	checks, err = plain.GoalChecks(ctx, "o-fast")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range checks {
		if c.ID == "statement-state" {
			t.Fatal("without a model, a judgement was asked")
		}
	}
}

// What a person types is matched against the record, asking of each
// existing record on its own whether it says the same; without a model,
// by the words they share.
func TestUnderstandingWhatAPersonTyped(t *testing.T) {
	ctx := context.Background()
	m := &model{answer: func(state string, q decide.Question) decide.Answer {
		same := 0.1
		if strings.Contains(q.Options[0].Description, "Faults are found before produce leaves the depot") {
			same = 0.93
		}
		if q.Options[0].Key != "same" {
			t.Errorf("same was not asked first: %+v", q.Options)
		}
		return decide.Answer{Probabilities: map[string]float64{"same": same, "other": 1 - same}}
	}}
	u, err := engineWith(t, m).Understand(ctx, "Faults are caught before produce goes out", "en")
	if err != nil {
		t.Fatal(err)
	}
	if !u.Available || len(u.Matches) != 1 || u.Matches[0].ID != "faults-found-before-dispatch" || u.Matches[0].By != "model" {
		t.Fatalf("with a model: %+v", u)
	}

	u, err = engineWith(t, nil).Understand(ctx, "Faults are found before produce leaves the depot", "en")
	if err != nil {
		t.Fatal(err)
	}
	if u.Available || len(u.Matches) == 0 || u.Matches[0].By != "words" || u.Matches[0].ID != "faults-found-before-dispatch" {
		t.Fatalf("without a model: %+v", u)
	}
}
