package engine

import (
	"context"
	"fmt"
	"sort"
	"strings"

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

// contrasts are the guidance's judgements (a field's good option against
// its poor one, asked good first) as measured: where good begins, and
// the score kept. A judgement the guidance has but this does not is not
// asked: it has not been measured.
var contrasts = map[string]struct {
	Threshold float64
	Measured  Measure
}{
	"statement-state": {Threshold: 0.76, Measured: Measure{Right: 21, Of: 24}},
	"gap-results":     {Threshold: 0.62, Measured: Measure{Right: 15, Of: 16}},
}

// contrast asks a guidance judgement of one text: Holds is the good
// option, Sure its probability. ok is false without a model, or for a
// judgement not measured.
func (e *Engine) contrast(ctx context.Context, id string, j GuideJudgement, text string) (Judged, bool) {
	m, measured := contrasts[id]
	if !measured {
		return Judged{}, false
	}
	// Good first: the model reads the order (docs/adr/0023).
	a, ok := e.ask(ctx, text, map[string]decide.Question{id: {Type: decide.Choice, Instructions: "Which describes this text?",
		Options: []decide.Option{{Key: j.Good.Key, Description: j.Good.Label}, {Key: j.Poor.Key, Description: j.Poor.Label}}}})
	if !ok {
		return Judged{}, false
	}
	p := a[id].Probabilities[j.Good.Key]
	return Judged{Holds: p >= m.Threshold, Sure: p}, true
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

// offWithoutModel is what is not judged while no model answers: every
// question above, and the matching that is the model's to do. A check
// that needs one is left out of a record's checks, not passed.
var offWithoutModel = []string{
	"whether an objective is a target with a number, rather than a change in words",
	"whether a text names a person without a title",
	"whether an outcome is written as a state rather than an action",
	"whether a gap names a result that falls short rather than a missing resource",
	"which existing record says what a text says, unless its name is the same or its initials",
	"what in the workspace is relevant to a piece of work, and which flow defines a text",
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

// personCheck asks whether any text the record holds names a person
// (names-person): an advisory check, the exact rule having refused a
// name with a title already (docs/adr/0029, 0030). asked is false
// without a model: the check is off.
func (e *Engine) personCheck(ctx context.Context, kind string, doc map[string]any) (Check, bool) {
	type text struct{ path, s string }
	var texts []text
	var walk func(v any, path string)
	walk = func(v any, path string) {
		switch t := v.(type) {
		case map[string]any:
			for k, x := range t {
				walk(x, path+"/"+k)
			}
		case []any:
			for i, x := range t {
				walk(x, fmt.Sprintf("%s/%d", path, i))
			}
		case string:
			// Words a person wrote: more than one, so not an id, a key or
			// a choice.
			if strings.Contains(strings.TrimSpace(t), " ") {
				texts = append(texts, text{path, t})
			}
		}
	}
	walk(doc["spec"], "/spec")
	if m, ok := doc["metadata"].(map[string]any); ok {
		if n, ok := m["name"].(string); ok {
			texts = append(texts, text{"/metadata/name", n})
		}
	}
	sort.Slice(texts, func(i, j int) bool { return texts[i].path < texts[j].path })
	// Each text on its own: asked joined, the model found a name in two
	// parts of nine (measured), so a joined screen would hide most. The
	// answers are cached by text, so a record pays for each once.
	asked := false
	for _, t := range texts {
		one, ok := e.judge(ctx, "names-person", t.s)
		if !ok {
			return Check{}, false
		}
		asked = true
		if one.Holds {
			return Check{ID: "names-person", State: checkWarn, Section: e.stepOfField(kind, t.path),
				Message: fmt.Sprintf("%s %s names a person. Cartograph names roles and bodies: name the role, such as the lead analyst, or the body.", t.path, sureWords(one.Sure))}, true
		}
	}
	if !asked {
		return Check{}, false
	}
	return Check{ID: "names-person", State: checkOK, Message: "No text names a person."}, true
}
