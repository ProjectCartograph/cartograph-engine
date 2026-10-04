// Package conformance is what every decide.Decider must do: answer every
// question asked, pick a Choice from its own options with probabilities
// that are probabilities, give a YesNo a probability, and stop when its
// context is cancelled.
package conformance

import (
	"context"
	"math"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/decide"
)

// Run checks d.
func Run(t *testing.T, d decide.Decider) {
	t.Helper()
	questions := map[string]decide.Question{
		"kind": {Type: decide.Choice, Instructions: "What is this text?", Options: []decide.Option{
			{Key: "aim", Description: "Something to set out to do."},
			{Key: "state", Description: "Something that will be true."},
		}},
		"state": {Type: decide.YesNo, Instructions: "Is this written as a state that will be true, rather than an action?"},
	}
	t.Run("answers every question, each a probability", func(t *testing.T) {
		got, err := d.Decide(context.Background(), "Fruit arrives sound at every depot.", questions)
		if err != nil {
			t.Fatal(err)
		}
		kind, ok := got["kind"]
		if !ok || (kind.Choice != "aim" && kind.Choice != "state") {
			t.Fatalf("the choice: %+v", kind)
		}
		sum := 0.0
		for key, p := range kind.Probabilities {
			if key != "aim" && key != "state" || p < 0 || p > 1 {
				t.Fatalf("a probability: %s %v", key, p)
			}
			sum += p
		}
		if math.Abs(sum-1) > 0.02 {
			t.Fatalf("the choice's probabilities add up to %v", sum)
		}
		if y := got["state"].Yes; y < 0 || y > 1 {
			t.Fatalf("the yes or no: %v", y)
		}
	})
	t.Run("stops when its context is cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := d.Decide(ctx, "x", questions); err == nil {
			t.Fatal("answered a cancelled call")
		}
	})
}
