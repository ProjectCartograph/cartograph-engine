package engine

import (
	"context"
	"fmt"
	"sort"
)

// PortfolioChecks reads one portfolio, what names it, and its decisions
// (TAXONOMY.md D32). Every check advises: each answer is read from other
// manifests, so, as for a programme (D6), none can stop a save.
func (e *Engine) PortfolioChecks(ctx context.Context, id string) ([]ProgrammeCheck, error) {
	v, found, err := e.manifests.GetCurrent(ctx, "Portfolio", id)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("%w: Portfolio/%s", ErrNotFound, id)
	}
	var doc map[string]any
	if err := e.codec.DecodeInto(v.YAML, &doc); err != nil {
		return nil, fmt.Errorf("parse Portfolio/%s: %w", id, err)
	}
	return e.portfolioChecksOf(ctx, id, doc)
}

// held is one thing a portfolio holds.
type held struct{ kind, id string }

func (e *Engine) portfolioChecksOf(ctx context.Context, id string, doc map[string]any) ([]ProgrammeCheck, error) {
	spec, _ := doc["spec"].(map[string]any)
	var out []ProgrammeCheck
	add := func(checkID, section, state, message string) {
		out = append(out, ProgrammeCheck{ID: checkID, Section: section, State: state, Message: message})
	}

	// What it is prioritised against.
	if objectives, _ := spec["objectives"].([]any); len(objectives) > 0 {
		add("portfolio-objectives", "strategy", programmeCheckOK,
			fmt.Sprintf("Prioritised against %d strategic objective%s.", len(objectives), plural(len(objectives))))
	} else {
		add("portfolio-objectives", "strategy", programmeCheckWarn,
			"No strategic objective yet: nothing to select and prioritise against.")
	}

	// What it holds, read back from what names it.
	top := func(field string) func(map[string]any) []any {
		return func(s map[string]any) []any { out, _ := s[field].([]any); return out }
	}
	programmes, err := e.namedBy(ctx, "Programme", top("portfolios"), id)
	if err != nil {
		return nil, err
	}
	projects, err := e.namedBy(ctx, "Project", func(s map[string]any) []any {
		alignment, _ := s["alignment"].(map[string]any)
		out, _ := alignment["portfolios"].([]any)
		return out
	}, id)
	if err != nil {
		return nil, err
	}
	portfolios, err := e.namedBy(ctx, "Portfolio", top("portfolios"), id)
	if err != nil {
		return nil, err
	}
	if counted := countedList([]counted{{len(programmes), "programme"}, {len(projects), "project"}, {len(portfolios), "portfolio"}}); counted == "" {
		add("portfolio-holds", "holds", programmeCheckWarn,
			"Nothing names this portfolio yet, so it holds nothing to decide on.")
	} else {
		add("portfolio-holds", "holds", programmeCheckOK, counted+" named.")
	}

	// Each thing it holds has a decision, and each decision is about
	// something it holds.
	var holds []held
	for _, p := range programmes {
		holds = append(holds, held{"Programme", p})
	}
	for _, p := range projects {
		holds = append(holds, held{"Project", p})
	}
	for _, p := range portfolios {
		holds = append(holds, held{"Portfolio", p})
	}
	if len(holds) == 0 {
		return out, nil
	}
	l := &lookup{ctx: ctx, store: e.manifests, codec: e.codec}
	files, err := l.Documents("PortfolioDecisions")
	if err != nil {
		return nil, err
	}
	decided := map[held]bool{}
	for _, f := range files {
		fs, _ := f["spec"].(map[string]any)
		if p, _ := fs["portfolio"].(string); p != id {
			continue
		}
		list, _ := fs["decisions"].([]any)
		for _, raw := range list {
			d, _ := raw.(map[string]any)
			k, _ := d["kind"].(string)
			i, _ := d["id"].(string)
			decided[held{k, i}] = true
		}
	}
	var undecided, strangers []string
	isHeld := map[held]bool{}
	for _, h := range holds {
		isHeld[h] = true
		if !decided[h] {
			undecided = append(undecided, l.nameOf(h.kind, h.id))
		}
	}
	for h := range decided {
		if !isHeld[h] {
			strangers = append(strangers, l.nameOf(h.kind, h.id))
		}
	}
	sort.Strings(strangers)
	switch {
	case len(strangers) > 0:
		add("portfolio-decided", "holds", programmeCheckWarn, fmt.Sprintf(
			"Decided on %s, which %s not name this portfolio: name it from there, or remove the decision.",
			joinAnd(strangers), plural3(len(strangers), "does", "do")))
	case len(undecided) > 0:
		add("portfolio-decided", "holds", programmeCheckWarn, fmt.Sprintf(
			"No decision yet on %s: invest, hold or stop.", joinAnd(undecided)))
	default:
		add("portfolio-decided", "holds", programmeCheckOK, "Everything it holds has a decision.")
	}
	return out, nil
}

// nameOf is a manifest's name, or its id when it has none or is missing.
func (l *lookup) nameOf(kind, id string) string {
	docs, err := l.Documents(kind)
	if err != nil {
		return id
	}
	if doc, ok := docs[id]; ok {
		return fmt.Sprintf("%q", docName(doc, id))
	}
	return fmt.Sprintf("%q", id)
}

// joinAnd joins names as a list in a sentence, with "and".
func joinAnd(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	}
	if len(names) > 4 {
		return fmt.Sprintf("%s and %d others", joinComma(names[:3]), len(names)-3)
	}
	return joinComma(names[:len(names)-1]) + " and " + names[len(names)-1]
}

func joinComma(names []string) string {
	out := ""
	for i, n := range names {
		if i > 0 {
			out += ", "
		}
		out += n
	}
	return out
}
