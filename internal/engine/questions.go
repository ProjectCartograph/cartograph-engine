package engine

import (
	"context"
	"fmt"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/decide"
)

// The questions the engine asks of text (docs/adr/0030). Each is a
// judgement of meaning no pattern can make: put to the decision model,
// its wording kept only as it measured against examples whose answer is
// known (testdata/decide, `just decide-measure`), on the model the flake
// pins. An answer advises, with how sure it is; it never refuses.
// Without a model, a question is not asked, and the check that needs it
// says it is off.

// judgement is one question: one or more wordings of a yes or no, whose
// probabilities are averaged (a model reads every wording in the one
// call), and the probability at or above which the answer is yes.
type judgement struct {
	// Name keys the question in a call and in the measured examples.
	Name string
	// Wordings are the statements a yes or no is asked of; each is
	// phrased so that yes means the judgement holds.
	Wordings []string
	// Threshold is where yes begins, as measured: a calibrated model's
	// probabilities need not split at one half for a given wording.
	Threshold float64
	// Measured is what the wording scored on the pinned model, for the
	// record and for decide-measure to hold it to.
	Measured Measure
}

// Measure is a question's score on its examples: how many it answered as
// known, of how many.
type Measure struct {
	Right, Of int
}

// judgements are every question the engine asks, by name.
var judgements = map[string]judgement{
	// An objective says the change the work makes, in words; a target is
	// a figure to reach and belongs in a key result. A number that names
	// a group ("2-year-olds", "Form 1 students", "the 3 depots") is not a
	// target, which is what a pattern could not tell (#3).
	"objective-target": {
		Name: "objective-target",
		Wordings: []string{
			"This text is a measurable target with a number to achieve.",
			"This is a numeric goal, such as reaching a percentage, a count or doubling something.",
		},
		Threshold: 0.43,
		Measured:  Measure{Right: 25, Of: 28},
	},
	// A record names roles and bodies, never people (AGENTS.md). A title
	// before a name is caught exactly, and refused; a name without one, or
	// in another language, only a model can see (#4).
	"names-person": {
		Name:      "names-person",
		Wordings:  []string{"The text contains a person's personal name."},
		Threshold: 0.21,
		Measured:  Measure{Right: 17, Of: 20},
	},
}

// Judged is a judgement's answer about one text: whether it holds, and
// how sure the model is, the averaged probability of yes.
type Judged struct {
	Holds bool
	Sure  float64
}

// judge asks the model whether a judgement holds of text. ok is false
// when there is no model or it cannot answer now: the judgement is off.
func (e *Engine) judge(ctx context.Context, name, text string) (Judged, bool) {
	j, known := judgements[name]
	if !known {
		panic(fmt.Sprintf("engine: no judgement %q", name))
	}
	questions := make(map[string]decide.Question, len(j.Wordings))
	for i, w := range j.Wordings {
		questions[fmt.Sprintf("%s-%d", j.Name, i)] = decide.Question{Type: decide.YesNo, Instructions: w}
	}
	answers, ok := e.ask(ctx, text, questions)
	if !ok {
		return Judged{}, false
	}
	sum := 0.0
	for i := range j.Wordings {
		sum += answers[fmt.Sprintf("%s-%d", j.Name, i)].Yes
	}
	p := sum / float64(len(j.Wordings))
	return Judged{Holds: p >= j.Threshold, Sure: p}, true
}

// sureWords says how sure an answer is, for a check's message.
func sureWords(p float64) string {
	switch {
	case p >= 0.8:
		return "very likely"
	case p >= 0.6:
		return "likely"
	default:
		return "possibly"
	}
}
