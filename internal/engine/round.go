package engine

import (
	"context"
	"strings"
)

// A round of decisions (docs/adr/0032). The open checks of a piece of work
// form a tree of decisions: what a record is comes before its numbers and
// its links, and a record comes after everything it names. The frontier
// is every decision whose prerequisites are settled, and a round asks the
// whole frontier at once, so the person answers several questions in one
// exchange and is never asked one that hangs on an answer not yet given.
// The engine computes the frontier from the order of work; an agent does
// not judge it.

// Round is the frontier of a piece of work's open checks.
type Round struct {
	// Settle are what the documents themselves state: the agent writes
	// them from the documents and asks only where a document is silent.
	Settle []Task `json:"settle"`
	// Ask are the decisions that are the person's, each with the records
	// that could settle it and the one recommended.
	Ask []Question `json:"ask"`
	// Write are links settled by writing a later record (a gap names the
	// outcome it closes): not questions, but the records to write next.
	Write []Task `json:"write"`
	// Waiting counts the open checks behind this round: they depend on
	// an answer in it, and come in a later round.
	Waiting int `json:"waiting"`
}

// Question is one decision in a round.
type Question struct {
	Task
	// Recommended is the choice the decision model ranks most relevant to
	// the record (docs/adr/0023). Empty without a model, or when none is
	// relevant: the agent then recommends from the documents.
	Recommended *Candidate `json:"recommended,omitempty"`
}

// Done reports whether nothing is left to settle or ask.
func (r Round) Done() bool {
	return len(r.Settle) == 0 && len(r.Ask) == 0 && len(r.Write) == 0 && r.Waiting == 0
}

// Round computes the next round of a piece of work in change set set.
func (e *Engine) Round(ctx context.Context, set string, work []Ref, locale string) (Round, error) {
	w, err := e.Work(ctx, work, locale)
	if err != nil {
		return Round{}, err
	}
	// What a document states is the agent's to write only when the work
	// has a document; otherwise every decision is the person's.
	sources, _ := e.Sources(ctx, set)
	r := frontier(w.Tasks, len(sources) > 0, e.namesIn(ctx, w.Tasks))
	e.recommend(ctx, r.Ask)
	r.Ask = append(r.Ask, e.unasked(ctx, set)...)
	return r, nil
}

// unasked are the checks the change set leaves for the person without
// having asked them ("not available", or nothing): a round is asked with
// the person there, so each is a question again until it carries their
// answer (eval run 007 left one so before asking, and never came back).
func (e *Engine) unasked(ctx context.Context, set string) []Question {
	cs, err := e.WorkingChangeSet(ctx, set)
	if err != nil {
		return nil
	}
	var out []Question
	for _, w := range cs.Waivers {
		if a := strings.TrimSpace(w.Asked); a != "" && !strings.EqualFold(a, "not available") {
			continue
		}
		kind, id, _ := strings.Cut(w.On, "/")
		out = append(out, Question{Task: Task{Phase: "define", Kind: kind, ID: id, Check: w.Check, State: checkWarn,
			Message: "Left for your person without asking them (" + w.Reason + "): ask them now, then leave it again with their answer as asked, or settle it."}})
	}
	return out
}

// recommend marks, on each question with choices, the one the decision
// model ranks most relevant to its record, asked once a record.
func (e *Engine) recommend(ctx context.Context, ask []Question) {
	ranked := map[string]Relevance{}
	for i := range ask {
		q := &ask[i]
		if len(q.Choices) == 0 || q.Name == "" {
			continue
		}
		rel, seen := ranked[q.Name]
		if !seen {
			rel, _ = e.Relevant(ctx, q.Name, nil, "", 10)
			ranked[q.Name] = rel
		}
		if !rel.Available {
			continue
		}
		best := -1.0
		for _, m := range rel.Matches {
			for _, c := range q.Choices {
				if c.ID == m.ID && m.Likelihood > best {
					best = m.Likelihood
					q.Recommended = &c
				}
			}
		}
	}
}

// frontier picks, from tasks in the order of work, the round to ask now.
//
// A question waits on what its own answer names: a field that names a
// record (an indicator's aims, a project's outcome) is asked once that
// record's definition no longer has a check open that blocks, so the
// person never chooses a record that is not yet settled. Nothing else
// in the work holds it back: a risk is asked beside an unsettled
// component, an indicator's target beside an aim still being drafted.
// Within a record, what it is comes first: its definition's checks are
// asked together, and its numbers and links wait while one of them that
// blocks is open. A warning holds nothing back.
// names are what each record of the work names, by field.
func frontier(tasks []Task, fromDocument bool, names map[Ref][]Named) Round {
	out := Round{Settle: []Task{}, Ask: []Question{}, Write: []Task{}}
	defining := map[Ref]bool{}
	for _, t := range tasks {
		if t.Phase == "define" && t.By == "" && t.State != checkWarn {
			defining[Ref{Kind: t.Kind, ID: t.ID}] = true
		}
	}
	// hangs reports whether the answer at field names a record whose
	// definition is open.
	hangs := func(r Ref, field string) bool {
		if field == "" {
			return false
		}
		for _, n := range names[r] {
			if n.Ref != r && defining[n.Ref] && (n.Path == field || strings.HasPrefix(n.Path, field+"/")) {
				return true
			}
		}
		return false
	}
	for _, t := range tasks {
		r := Ref{Kind: t.Kind, ID: t.ID}
		if t.By != "" {
			// Settled by writing a later record that names this one: once
			// this one is defined.
			if defining[r] {
				out.Waiting++
			} else {
				out.Write = append(out.Write, t)
			}
			continue
		}
		if (t.Phase != "define" && defining[r]) || hangs(r, t.Field) {
			out.Waiting++
			continue
		}
		if fromDocument && WrittenFromTheDocument(t.Check) {
			out.Settle = append(out.Settle, t)
			continue
		}
		out.Ask = append(out.Ask, Question{Task: t})
	}
	return out
}

// Named is a record a record names, and the field that names it.
type Named struct {
	Ref
	Path string
}

// namesIn are the records each record with an open check names, by
// field, as the change set on ctx has them.
func (e *Engine) namesIn(ctx context.Context, tasks []Task) map[Ref][]Named {
	out := map[Ref][]Named{}
	for _, t := range tasks {
		r := Ref{Kind: t.Kind, ID: t.ID}
		if _, done := out[r]; done {
			continue
		}
		out[r] = nil
		text := e.workText(ctx, r)
		if text == nil {
			continue
		}
		var doc map[string]any
		if e.codec.DecodeInto(text, &doc) != nil {
			continue
		}
		for _, f := range extractRefs(doc, e.refRules[r.Kind]) {
			out[r] = append(out[r], Named{Ref: Ref{Kind: f.kind, ID: f.id}, Path: f.path})
		}
	}
	return out
}
