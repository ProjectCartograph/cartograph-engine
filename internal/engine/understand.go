package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/decide"
)

// Understanding what a person wrote (docs/adr/0023). A decision model,
// where a deployment chooses one, answers typed questions about text: the
// engine asks it whether a statement is written as the discipline asks
// (the guidance's judgements), what kind of thing a sentence describes,
// and which existing record says the same thing. Without one, the checks
// it would add are not asked, and matching falls back to the words two
// texts share. Nothing it answers is stored: it advises, as every check
// does.

// WithDecider sets the decision model the engine asks. Without one, the
// engine answers as it always has.
func WithDecider(d decide.Decider) Option {
	return func(e *Engine) {
		e.decider = d
		e.decisions = &decisionCache{entries: map[string]map[string]decide.Answer{}}
	}
}

// decisionCache keeps answers by the text and questions asked, so a check
// run asks the model once per distinct statement. A cache, not a fact:
// losing it costs only time.
type decisionCache struct {
	mu      sync.Mutex
	entries map[string]map[string]decide.Answer
}

const decisionCacheSize = 4096

// ask asks the model, or answers from the cache. Without a model, or when
// it cannot answer now, ok is false and the caller answers without it.
func (e *Engine) ask(ctx context.Context, state string, questions map[string]decide.Question) (map[string]decide.Answer, bool) {
	if e.decider == nil || strings.TrimSpace(state) == "" || len(questions) == 0 {
		return nil, false
	}
	h := sha256.New()
	h.Write([]byte(state))
	names := make([]string, 0, len(questions))
	for n := range questions {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		q := questions[n]
		fmt.Fprintf(h, "\x00%s\x00%s\x00%s", n, q.Type, q.Instructions)
		for _, o := range q.Options {
			fmt.Fprintf(h, "\x00%s=%s", o.Key, o.Description)
		}
	}
	key := hex.EncodeToString(h.Sum(nil))
	e.decisions.mu.Lock()
	if a, ok := e.decisions.entries[key]; ok {
		e.decisions.mu.Unlock()
		return a, true
	}
	e.decisions.mu.Unlock()
	answers, err := e.decider.Decide(ctx, state, questions)
	if err != nil {
		return nil, false
	}
	e.decisions.mu.Lock()
	if len(e.decisions.entries) >= decisionCacheSize {
		e.decisions.entries = map[string]map[string]decide.Answer{}
	}
	e.decisions.entries[key] = answers
	e.decisions.mu.Unlock()
	return answers, true
}

// GuideJudgement is a check a decision model answers about one field: a
// contrast between a good option and a poor one, asked good first.
type GuideJudgement struct {
	Path   string      `json:"path"`
	Levels []string    `json:"levels,omitempty"`
	Good   GuideOption `json:"good"`
	Poor   GuideOption `json:"poor"`
	Pass   string      `json:"pass"`
	Fail   string      `json:"fail"`
}

// GuideOption is one side of a judgement's contrast.
type GuideOption struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// valuesAt reads the strings at a JSON pointer, "-" standing for every
// item of a list, each with its item number where a list was crossed.
func valuesAt(doc any, path string) []struct {
	n    int
	text string
} {
	type found = struct {
		n    int
		text string
	}
	var out []found
	var walk func(node any, parts []string, n int)
	walk = func(node any, parts []string, n int) {
		if len(parts) == 0 {
			if s, ok := node.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, found{n, s})
			}
			return
		}
		if parts[0] == "-" {
			list, _ := node.([]any)
			for i, item := range list {
				walk(item, parts[1:], i+1)
			}
			return
		}
		m, _ := node.(map[string]any)
		if m == nil {
			return
		}
		walk(m[parts[0]], parts[1:], n)
	}
	walk(doc, strings.Split(strings.TrimPrefix(path, "/"), "/"), 0)
	return out
}

// judgedChecks asks the model the kind's judgements about what the
// manifest says: one check per judgement, met when every text it reads is
// answered as the discipline asks. Nothing when no model is set or it
// cannot answer now.
func (e *Engine) judgedChecks(ctx context.Context, kind string, doc map[string]any) []ProgrammeCheck {
	if e.decider == nil {
		return nil
	}
	words, ok, _ := GuideBundleFor(kind, DefaultLocale)
	if !ok || len(words.Judgements) == 0 {
		return nil
	}
	spec, _ := doc["spec"].(map[string]any)
	level, _ := spec["level"].(string)
	ids := make([]string, 0, len(words.Judgements))
	for id := range words.Judgements {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var out []ProgrammeCheck
	for _, id := range ids {
		j := words.Judgements[id]
		if len(j.Levels) > 0 && !contains(j.Levels, level) {
			continue
		}
		values := valuesAt(doc, j.Path)
		if len(values) == 0 {
			continue
		}
		failed, asked := 0, 0
		first := 0
		for _, v := range values {
			// Good first: the model reads the order (docs/adr/0023).
			a, ok := e.ask(ctx, v.text, map[string]decide.Question{id: {Type: decide.Choice, Instructions: "Which describes this text?",
				Options: []decide.Option{{Key: j.Good.Key, Description: j.Good.Label}, {Key: j.Poor.Key, Description: j.Poor.Label}}}})
			if !ok {
				continue
			}
			asked++
			if a[id].Probabilities[j.Good.Key] < 0.5 {
				failed++
				if first == 0 {
					first = v.n
				}
			}
		}
		if asked == 0 {
			continue
		}
		if failed == 0 {
			out = append(out, ProgrammeCheck{ID: id, State: programmeCheckOK, Message: j.Pass, Section: e.stepOfField(kind, j.Path)})
		} else {
			out = append(out, ProgrammeCheck{ID: id, State: programmeCheckWarn, Message: strings.ReplaceAll(j.Fail, "{n}", fmt.Sprint(first)), Section: e.stepOfField(kind, j.Path)})
		}
	}
	return out
}

// Match is an existing record that says what a text says.
type Match struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Name   string `json:"name"`
	Level  string `json:"level,omitempty"`
	Detail string `json:"detail,omitempty"`
	// Likelihood is how likely it is to be the same thing, from 0 to 1;
	// By says whether the model judged it or the words the two share did.
	Likelihood float64 `json:"likelihood"`
	By         string  `json:"by"`
}

// statementOf is what a record says, beside its name, for matching.
func statementOf(doc map[string]any) string {
	spec, _ := doc["spec"].(map[string]any)
	for _, k := range []string{"objective", "statement", "definition", "purpose", "description"} {
		if s, ok := spec[k].(string); ok && strings.TrimSpace(s) != "" {
			return s
		}
	}
	if aim, ok := spec["aim"].(map[string]any); ok {
		if s, ok := aim["change"].(string); ok {
			return s
		}
	}
	return ""
}

// wordsOf is a text's distinct words of three letters or more.
func wordsOf(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if len([]rune(w)) >= 3 {
			out[w] = true
		}
	}
	return out
}

// overlap scores how many words two texts share, from 0 to 1.
func overlap(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	n := 0
	for w := range a {
		if b[w] {
			n++
		}
	}
	return float64(n) / math.Sqrt(float64(len(a)*len(b)))
}

// matchCandidates are the records of kind (at level, for a Goal) closest
// to text by the words they share, at most limit.
func (e *Engine) matchCandidates(ctx context.Context, kind, level, text string, limit int) []Match {
	l := &lookup{ctx: ctx, store: e.manifests, codec: e.codec}
	docs, err := l.Documents(kind)
	if err != nil {
		return nil
	}
	want := wordsOf(text)
	var out []Match
	for id, doc := range docs {
		spec, _ := doc["spec"].(map[string]any)
		lv, _ := spec["level"].(string)
		if level != "" && lv != level {
			continue
		}
		name, detail := docName(doc, id), statementOf(doc)
		out = append(out, Match{Kind: kind, ID: id, Name: name, Level: lv, Detail: detail,
			Likelihood: overlap(want, wordsOf(name+" "+detail)), By: "words"})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Likelihood != out[j].Likelihood {
			return out[i].Likelihood > out[j].Likelihood
		}
		return out[i].ID < out[j].ID
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// maxMatches bounds what a match by words returns, and matchPool how many
// records the model is asked about, one question each, in one call.
const (
	maxMatches = 3
	matchPool  = 40
)

// The model's answer is a match only when it is sure and clear of the
// next record: measured (docs/adr/0023), this named no wrong record, where
// a lower bar named one in four.
const (
	sameAtLeast = 0.85
	sameClearBy = 0.05
)

// sameAs asks the model, of each candidate's name on its own, whether the
// text says the same as it: one two-way question each, "same" first, which
// is what was measured to work (docs/adr/0023). It returns the one record
// that says the same, or none.
func (e *Engine) sameAs(ctx context.Context, text string, pool []Match) ([]Match, bool) {
	qs := make(map[string]decide.Question, len(pool))
	for i, m := range pool {
		qs[fmt.Sprintf("r%d", i)] = decide.Question{Type: decide.Choice, Instructions: "Does this text say the same thing as the record?",
			Options: []decide.Option{{Key: "same", Description: "says the same as: " + m.Name}, {Key: "other", Description: "says something else"}}}
	}
	answers, ok := e.ask(ctx, text, qs)
	if !ok {
		return nil, false
	}
	best, first, second := -1, 0.0, 0.0
	for i := range pool {
		switch p := answers[fmt.Sprintf("r%d", i)].Probabilities["same"]; {
		case p > first:
			best, first, second = i, p, first
		case p > second:
			second = p
		}
	}
	if best < 0 || first < sameAtLeast || first-second < sameClearBy {
		return []Match{}, true
	}
	m := pool[best]
	m.Likelihood, m.By = first, "model"
	return []Match{m}, true
}

// byWords keeps the candidates that share enough words with the text,
// best first: the answer without a model.
func byWords(pool []Match) []Match {
	out := []Match{}
	for _, m := range pool {
		if m.Likelihood >= 0.2 {
			out = append(out, m)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Likelihood > out[j].Likelihood })
	if len(out) > maxMatches {
		out = out[:maxMatches]
	}
	return out
}

// MatchExisting returns the existing records of kind (and level) that say
// what text says, most likely first: judged by the model where there is
// one, else by the words they share.
func (e *Engine) MatchExisting(ctx context.Context, kind, level, text string) []Match {
	pool := e.matchCandidates(ctx, kind, level, text, matchPool)
	if len(pool) == 0 || strings.TrimSpace(text) == "" {
		return []Match{}
	}
	if out, ok := e.sameAs(ctx, text, pool); ok {
		return out
	}
	return byWords(pool)
}

// Understanding is what Cartograph makes of a text a person typed: the
// existing record that says the same, and the flows likeliest to define
// it.
type Understanding struct {
	// Available is false when no decision model answered: Matches are
	// then the records that share the text's words, and there are no
	// Routes.
	Available bool    `json:"available"`
	Matches   []Match `json:"matches"`
	Routes    []Route `json:"routes"`
}

// Route is a flow that may define what a person typed: a stage of the
// order of work, and how likely the model found it.
type Route struct {
	Key        string  `json:"key"`
	Kind       string  `json:"kind"`
	Level      string  `json:"level,omitempty"`
	Likelihood float64 `json:"likelihood"`
}

// maxRoutes is how many flows are offered. Measured (docs/adr/0023), the
// model's first flow was right half the time, and the right one was among
// its three likeliest nine times in ten, at any confidence: so three are
// offered, and the person chooses.
const maxRoutes = 3

// levelQuestion is the key of the question that picks a kind's level.
const levelQuestion = "level"

// routesFor asks the model, of each stage on its own, whether the text is
// what the stage's cue describes, "yes" first, and returns the likeliest
// flows, one per kind. A stage with levels is asked by its level's own
// definition, the words a person reads, and the level a flow opens at is
// chosen among those definitions in one question: measured
// (docs/adr/0023), definitions that tell the levels apart for the model
// are the ones that tell them apart for a person.
func (e *Engine) routesFor(ctx context.Context, text, locale string) ([]Route, bool) {
	qs := map[string]decide.Question{}
	at := map[string]Stage{}
	var levels []decide.Option
	for _, st := range stages {
		words, ok, _ := GuideBundleFor(st.Kind, locale)
		if !ok {
			continue
		}
		cue := words.Cues[st.Key]
		if cue == "" && st.Level != "" {
			cue = words.Levels[st.Level]
			if cue != "" {
				levels = append(levels, decide.Option{Key: st.Level, Description: cue})
			}
		}
		if cue == "" {
			continue
		}
		qs[st.Key] = decide.Question{Type: decide.Choice, Instructions: "Which describes this text?",
			Options: []decide.Option{{Key: "yes", Description: cue}, {Key: "no", Description: "something else"}}}
		at[st.Key] = st
	}
	if len(qs) == 0 {
		return nil, false
	}
	if len(levels) > 1 {
		qs[levelQuestion] = decide.Question{Type: decide.Choice, Instructions: "Which level of the strategy is this?", Options: levels}
	}
	answers, ok := e.ask(ctx, text, qs)
	if !ok {
		return nil, false
	}
	best := map[string]Route{}
	for key, st := range at {
		p := answers[key].Probabilities["yes"]
		if r, seen := best[st.Kind]; !seen || p > r.Likelihood || (p == r.Likelihood && key < r.Key) {
			best[st.Kind] = Route{Key: key, Kind: st.Kind, Level: st.Level, Likelihood: p}
		}
	}
	out := make([]Route, 0, len(best))
	for _, r := range best {
		// The level is the one its definitions choose, not the stage
		// that happened to rank the flow.
		if lv := answers[levelQuestion].Choice; r.Level != "" && lv != "" {
			r.Key, r.Level = lv, lv
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Likelihood != out[j].Likelihood {
			return out[i].Likelihood > out[j].Likelihood
		}
		return rank(out[i].Kind, out[i].Level) < rank(out[j].Kind, out[j].Level)
	})
	if len(out) > maxRoutes {
		out = out[:maxRoutes]
	}
	return out, true
}

// Understand reads a text a person typed against the record and the order
// of work: which existing record says the same, and which three flows are
// likeliest to define it. It never picks a flow for the person: measured,
// the first was right only half the time.
func (e *Engine) Understand(ctx context.Context, text, locale string) (Understanding, error) {
	out := Understanding{Matches: []Match{}, Routes: []Route{}}
	if strings.TrimSpace(text) == "" {
		return out, nil
	}
	var pool []Match
	seen := map[string]bool{}
	for _, s := range stages {
		if seen[s.Kind] {
			continue
		}
		seen[s.Kind] = true
		pool = append(pool, e.matchCandidates(ctx, s.Kind, "", text, matchPool)...)
	}
	sort.SliceStable(pool, func(i, j int) bool { return pool[i].Likelihood > pool[j].Likelihood })
	if len(pool) > matchPool {
		pool = pool[:matchPool]
	}
	found, ok := e.sameAs(ctx, text, pool)
	if !ok {
		out.Matches = byWords(pool)
		return out, nil
	}
	routes, ok := e.routesFor(ctx, text, locale)
	if !ok {
		out.Matches = byWords(pool)
		return out, nil
	}
	out.Available, out.Matches, out.Routes = true, found, routes
	return out, nil
}

// DecisionModel is whether a decision model is configured, and whether it
// answers now: what an agent asks first, to know whether to lean on it.
type DecisionModel struct {
	Configured bool `json:"configured"`
	Ready      bool `json:"ready"`
}

// DecisionModel reports the decision port's state, whatever is behind it.
func (e *Engine) DecisionModel(ctx context.Context) DecisionModel {
	if e.decider == nil {
		return DecisionModel{}
	}
	return DecisionModel{Configured: true, Ready: e.decider.Ready(ctx) == nil}
}

// How relevance is judged, as measured (docs/adr/0023): of each candidate
// on its own, whether the work is about the same thing; per kind, the
// likeliest few at or above an even chance. A ranking to put first in a
// picker, never a filter: everything else stays where it was.
const (
	relevantFloor   = 0.5
	relevantPerKind = 3
	relevantPool    = 120
)

// RelevantKinds are the kinds relevance is asked across when none are
// named: the work and strategy a piece of work names, and the registers
// it draws on.
var RelevantKinds = []string{"Goal", "KPI", "Gap", "Assumption", "Portfolio", "Programme", "Operation", "Project", "BeneficiaryGroup", "DataSource", "FundingSource", "Resource"}

// Relevance is what in the workspace is relevant to a text.
type Relevance struct {
	// Available is false when no decision model answered: Matches are
	// then by shared words.
	Available bool    `json:"available"`
	Matches   []Match `json:"matches"`
}

// Relevant ranks the records of kinds (a goal's level, when level is
// set) by how relevant they are to text, and returns up to limit of each,
// likeliest first.
func (e *Engine) Relevant(ctx context.Context, text string, kinds []string, level string, limit int) (Relevance, error) {
	out := Relevance{Matches: []Match{}}
	if strings.TrimSpace(text) == "" {
		return out, nil
	}
	if len(kinds) == 0 {
		kinds = RelevantKinds
	}
	if limit <= 0 || limit > 10 {
		limit = relevantPerKind
	}
	perKind := relevantPool / len(kinds)
	if perKind < 8 {
		perKind = 8
	}
	var pool []Match
	for _, k := range kinds {
		lv := ""
		if k == "Goal" {
			lv = level
		}
		pool = append(pool, e.matchCandidates(ctx, k, lv, text, perKind)...)
	}
	if len(pool) == 0 {
		return out, nil
	}
	qs := make(map[string]decide.Question, len(pool))
	for i, m := range pool {
		qs[fmt.Sprintf("r%d", i)] = decide.Question{Type: decide.Choice, Instructions: "Is the work about the same thing as the record?",
			Options: []decide.Option{{Key: "relevant", Description: "about the same thing as: " + m.Name}, {Key: "other", Description: "about something else"}}}
	}
	answers, ok := e.ask(ctx, text, qs)
	if ok {
		out.Available = true
		for i := range pool {
			pool[i].Likelihood = answers[fmt.Sprintf("r%d", i)].Probabilities["relevant"]
			pool[i].By = "model"
		}
	}
	floor := relevantFloor
	if !ok {
		floor = 0.2 // shared words, as a match falls back to them
	}
	byKind := map[string][]Match{}
	for _, m := range pool {
		if m.Likelihood >= floor {
			byKind[m.Kind] = append(byKind[m.Kind], m)
		}
	}
	for _, k := range kinds {
		list := byKind[k]
		sort.SliceStable(list, func(i, j int) bool { return list[i].Likelihood > list[j].Likelihood })
		if len(list) > limit {
			list = list[:limit]
		}
		out.Matches = append(out.Matches, list...)
	}
	return out, nil
}
