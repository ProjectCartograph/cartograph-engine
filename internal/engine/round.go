package engine

import "context"

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
	r := frontier(w.Tasks, len(sources) > 0)
	e.recommend(ctx, r.Ask)
	return r, nil
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
// A stage of the graph opens once no earlier stage still has a record
// whose definition is open: a record names only what comes before it, and
// what it names must be settled, not merely written, before the person
// is asked about the record that names it. Within an open stage, each
// record offers its first open step, since its later steps (its numbers,
// its links) hang on what it is. Records apart from each other are asked
// side by side.
func frontier(tasks []Task, fromDocument bool) Round {
	out := Round{Settle: []Task{}, Ask: []Question{}, Write: []Task{}}
	openDefinition := -1
	for _, t := range tasks {
		if t.Phase == "define" && t.By == "" && (openDefinition < 0 || t.Stage < openDefinition) {
			openDefinition = t.Stage
		}
	}
	type step struct {
		phase, step string
	}
	first := map[Ref]step{}
	for _, t := range tasks {
		r := Ref{Kind: t.Kind, ID: t.ID}
		if t.By != "" {
			// Settled by writing a later record: once every record up to
			// and at its own stage is defined.
			if openDefinition < 0 || t.Stage < openDefinition {
				out.Write = append(out.Write, t)
			} else {
				out.Waiting++
			}
			continue
		}
		if openDefinition >= 0 && t.Stage > openDefinition {
			out.Waiting++
			continue
		}
		s, seen := first[r]
		if !seen {
			s = step{t.Phase, t.Step}
			first[r] = s
		}
		if s != (step{t.Phase, t.Step}) {
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
