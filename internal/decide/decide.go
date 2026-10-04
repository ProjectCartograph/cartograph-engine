// Package decide is the port through which the engine asks typed
// questions of a piece of text: which of these options it is, or whether
// a statement holds of it, each answer a calibrated probability
// (docs/adr/0023). It reads; it never writes text. The engine uses it to
// check what a person wrote against the discipline's examples and to
// recognise what a person means, and works without it: a deployment that
// chooses none gets every check and every answer it had before.
package decide

import (
	"context"
	"errors"
)

// Type is the kind of answer a question wants.
type Type string

const (
	// Choice picks one of the options.
	Choice Type = "choice"
	// YesNo says how likely a statement about the text is to hold.
	YesNo Type = "yesno"
)

// Option is one answer a Choice may pick: a key, and a description of
// what it means, which is what the decision is made against.
type Option struct {
	Key, Description string
}

// Question is one thing to decide about the text.
type Question struct {
	Type Type
	// Instructions say what to decide, in a sentence.
	Instructions string
	// Options are a Choice's answers, in the order they are put; a
	// decider keeps the order, which a model may read. A YesNo has none.
	Options []Option
}

// Answer is one question's answer.
type Answer struct {
	// Choice is the option picked, for a Choice, and Probabilities every
	// option's probability.
	Choice        string
	Probabilities map[string]float64
	// Yes is the probability the statement holds, for a YesNo.
	Yes float64
}

// Decider answers questions about a text.
type Decider interface {
	// Decide answers every question asked of state, by the question's
	// name, in one call.
	Decide(ctx context.Context, state string, questions map[string]Question) (map[string]Answer, error)
}

// ErrUnavailable is a decider that cannot answer now (its model not
// reachable). The engine answers as it would without one.
var ErrUnavailable = errors.New("decisions unavailable")
