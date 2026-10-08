// Package fake is a decide.Decider for the gate's tests: it answers each
// question as a test scripts it, by the question's name, and any question
// not scripted as a model unsure of it would (every option alike, a yes
// or no at one half). Tests of what the engine does with an answer use
// it in place of a model, which runs only in the measured, local tests
// (docs/adr/0030). It passes the port's conformance suite.
package fake

import (
	"context"
	"sync"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/decide"
)

// Decider answers from a script, and remembers what it was asked.
type Decider struct {
	mu     sync.Mutex
	script map[string]func(state string, q decide.Question) decide.Answer
	asked  []Asked
}

// Asked is one question asked, with the text it was asked of.
type Asked struct {
	Question, State string
}

var _ decide.Decider = (*Decider)(nil)

// New returns a decider with nothing scripted.
func New() *Decider {
	return &Decider{script: map[string]func(string, decide.Question) decide.Answer{}}
}

// On scripts the answer to the question named name.
func (d *Decider) On(name string, answer func(state string, q decide.Question) decide.Answer) *Decider {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.script[name] = answer
	return d
}

// Asked is every question asked so far, in order.
func (d *Decider) Asked() []Asked {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]Asked(nil), d.asked...)
}

// Ready is always ready.
func (d *Decider) Ready(context.Context) error { return nil }

// Decide answers every question.
func (d *Decider) Decide(ctx context.Context, state string, questions map[string]decide.Question) (map[string]decide.Answer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make(map[string]decide.Answer, len(questions))
	for name, q := range questions {
		d.asked = append(d.asked, Asked{Question: name, State: state})
		if f, ok := d.script[name]; ok {
			out[name] = f(state, q)
			continue
		}
		out[name] = Unsure(q)
	}
	return out, nil
}

// Unsure is how a model answers what it cannot tell: every option alike,
// the first picked; a yes or no at one half.
func Unsure(q decide.Question) decide.Answer {
	if q.Type != decide.Choice || len(q.Options) == 0 {
		return decide.Answer{Yes: 0.5}
	}
	p := map[string]float64{}
	for _, o := range q.Options {
		p[o.Key] = 1 / float64(len(q.Options))
	}
	return decide.Answer{Choice: q.Options[0].Key, Probabilities: p}
}

// Pick is a choice of key, at probability p, the rest shared among the
// other options.
func Pick(q decide.Question, key string, p float64) decide.Answer {
	probs := map[string]float64{key: p}
	others := len(q.Options) - 1
	for _, o := range q.Options {
		if o.Key != key && others > 0 {
			probs[o.Key] = (1 - p) / float64(others)
		}
	}
	return decide.Answer{Choice: key, Probabilities: probs}
}

// Yes is a yes or no at probability p.
func Yes(p float64) decide.Answer { return decide.Answer{Yes: p} }
