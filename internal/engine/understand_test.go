package engine_test

import (
	"context"
	"strings"
	"testing"

	codecyaml "github.com/ProjectCartograph/cartograph-engine/v2/internal/codec/yaml"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/decide"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/decide/fake"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store/memory"
)

// model answers as a decision model might: a statement starting with a
// verb is an action, not a state; a choice picks what answer says.
type model struct {
	answer func(state string, q decide.Question) decide.Answer
	asked  int
}

func (m *model) Ready(context.Context) error { return nil }

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
	t.Parallel()
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
	if c := byID["statement-state"]; c.State != "warn" || !strings.Contains(c.Message, "something to do") {
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
// existing record on its own whether it says the same, and the three
// flows likeliest to define it are offered, one per kind; without a model,
// matches are by the words they share and no flow is offered.
func TestUnderstandingWhatAPersonTyped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := &model{answer: func(state string, q decide.Question) decide.Answer {
		switch q.Options[0].Key {
		case "same":
			same := 0.1
			if strings.Contains(q.Options[0].Description, "Faults are found before produce leaves the depot") {
				same = 0.93
			}
			return decide.Answer{Probabilities: map[string]float64{"same": same, "other": 1 - same}}
		case "yes":
			// The cues: a gap first, then a goal's level, then a project.
			// The goal's levels are asked by their definitions, and the
			// broadest ranks the flow, but the level question chooses
			// outcome, so the flow opens there.
			yes := map[string]float64{
				"a problem today compared with where it should be":                     0.9,
				"A broad direction your organisation keeps working towards.":           0.8,
				"A fact about people or things once that change is made.":              0.6,
				"a one-off job to build, set up, replace, train or roll out something": 0.7,
			}[q.Options[0].Description]
			return decide.Answer{Probabilities: map[string]float64{"yes": yes, "no": 1 - yes}}
		case "goal":
			if q.Instructions != "Which level of the strategy is this?" || len(q.Options) != 3 {
				t.Errorf("the level question: %+v", q)
			}
			return decide.Answer{Choice: "outcome", Probabilities: map[string]float64{"goal": 0.2, "objective": 0.2, "outcome": 0.6}}
		}
		t.Errorf("an unexpected question: %+v", q.Options)
		return decide.Answer{}
	}}
	u, err := engineWith(t, m).Understand(ctx, "Faults are caught before produce goes out", "en")
	if err != nil {
		t.Fatal(err)
	}
	if !u.Available || len(u.Matches) != 1 || u.Matches[0].ID != "faults-found-before-dispatch" || u.Matches[0].By != "model" {
		t.Fatalf("with a model, matches: %+v", u)
	}
	var got []string
	for _, r := range u.Routes {
		got = append(got, r.Key)
	}
	if strings.Join(got, " ") != "gap outcome project" || u.Routes[1].Kind != "Goal" || u.Routes[1].Level != "outcome" {
		t.Fatalf("with a model, routes: %+v", u.Routes)
	}

	u, err = engineWith(t, nil).Understand(ctx, "Faults are found before produce leaves the depot", "en")
	if err != nil {
		t.Fatal(err)
	}
	// Without a model only the exact rule matches: the record's own name.
	if u.Available || len(u.Routes) != 0 || len(u.Matches) != 1 || u.Matches[0].By != "name" || u.Matches[0].ID != "faults-found-before-dispatch" {
		t.Fatalf("without a model, the name: %+v", u)
	}
	u, err = engineWith(t, nil).Understand(ctx, "Faults found before produce leaves", "en")
	if err != nil {
		t.Fatal(err)
	}
	if u.Available || len(u.Matches) != 0 {
		t.Fatalf("without a model, words are not a match: %+v", u)
	}
}

// What in the workspace is relevant to a piece of work: each record asked
// on its own whether the work is about the same thing, that answer and
// the words it shares with the work weighed evenly, the likeliest few of
// each kind at an even chance or more, and nothing below it; without a
// model, nothing is judged relevant (docs/adr/0030).
func TestRelevantRanksTheWorkspace(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := &model{answer: func(state string, q decide.Question) decide.Answer {
		if q.Instructions != "Is the work about the same thing as the record?" || q.Options[0].Key != "relevant" {
			t.Errorf("an unexpected question: %+v", q)
		}
		p := 0.2
		switch q.Options[0].Description {
		case "about the same thing as: Faults are found before produce leaves the depot":
			p = 0.9
		case "about the same thing as: Quality pass rate":
			p = 0.7
		}
		return decide.Answer{Probabilities: map[string]float64{"relevant": p, "other": 1 - p}}
	}}
	e := engineWith(t, m)
	r, err := e.Relevant(ctx, "Check every delivery at intake", []string{"Goal", "KPI"}, "outcome", 0)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, x := range r.Matches {
		got = append(got, x.Kind+"/"+x.ID)
	}
	if !r.Available || strings.Join(got, " ") != "Goal/faults-found-before-dispatch KPI/quality-pass-rate" {
		t.Fatalf("with a model: %+v", r)
	}
	if s := e.DecisionModel(ctx); !s.Configured || !s.Ready || len(s.Off) != 0 {
		t.Fatalf("status with a model: %+v", s)
	}

	plain := engineWith(t, nil)
	r, err = plain.Relevant(ctx, "Faults are found before produce leaves the depot", []string{"Goal"}, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if r.Available || len(r.Matches) != 0 {
		t.Fatalf("without a model: %+v", r)
	}
	// Without a model, the status says what is not judged meanwhile.
	if s := plain.DecisionModel(ctx); s.Configured || s.Ready || len(s.Off) == 0 {
		t.Fatalf("status without a model: %+v", s)
	}
}

// A rough idea is read sentence by sentence for the questions a walk asks,
// and a sentence is offered only when the model is sure of it and it is
// clear of the next (docs/adr/0023); without a model, none is.
func TestAnIdeaAnswersTheQuestionsItCan(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := &model{answer: func(state string, q decide.Question) decide.Answer {
		p := 0.2
		switch {
		case q.Options[0].Description == "says who the work is for or who benefits" && strings.HasPrefix(state, "It is for"):
			p = 0.9
		case q.Options[0].Description == "says what is wrong today" && strings.HasPrefix(state, "Today"):
			p = 0.65 // sure, but not clear of the next
		case q.Options[0].Description == "says what is wrong today" && strings.HasPrefix(state, "It is for"):
			p = 0.6
		}
		return decide.Answer{Probabilities: map[string]float64{"yes": p, "other": 1 - p}}
	}}
	idea := "It is for the members who deliver to the northern depots. Today half the produce waits a day in the field!\nOk."
	got, ok := engineWith(t, m).FromIdea(ctx, "Project", idea)
	if !ok || len(got) != 1 || got[0].Key != "for" || got[0].Sentence != "It is for the members who deliver to the northern depots" || got[0].Field != "/spec/summary/beneficiaries" {
		t.Fatalf("with a model: %+v", got)
	}
	if got, ok := engineWith(t, nil).FromIdea(ctx, "Project", idea); ok || len(got) != 0 {
		t.Fatalf("without a model: %+v", got)
	}
}

// A name typed otherwise (another case, spacing or punctuation) or by its
// initials is the record for certain, with a model or without one, and
// the model is not asked: it is unsure of both.
func TestANameSpeltOtherwiseOrByInitialsIsTheRecord(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	unsure := &model{answer: func(string, decide.Question) decide.Answer {
		return decide.Answer{Probabilities: map[string]float64{"same": 0.3, "other": 0.7}}
	}}
	for _, d := range []decide.Decider{unsure, nil} {
		e := engineWith(t, d)
		for _, c := range []struct{ kind, text, want string }{
			{"BeneficiaryGroup", "depot STAFF", "depot-staff"},
			{"BeneficiaryGroup", "Depot-staff.", "depot-staff"},
			{"DataSource", "QCT", "quality-check-tool"},
			{"DataSource", "qct", "quality-check-tool"},
		} {
			got := e.MatchExisting(ctx, c.kind, "", c.text)
			if len(got) != 1 || got[0].ID != c.want || got[0].Likelihood != 1 {
				t.Errorf("%q (model %v): %+v", c.text, d != nil, got)
			}
		}
		// Not a name's initials: no certain match.
		if got := e.MatchExisting(ctx, "DataSource", "", "QX"); len(got) == 1 && got[0].Likelihood == 1 {
			t.Errorf("QX matched: %+v", got)
		}
	}
}

// A contrast is judged at the threshold it was measured at, not at one
// half: the model's probability of the good option must reach it. No
// list of words decides beforehand (docs/adr/0030).
func TestAContrastIsJudgedAtItsMeasuredThreshold(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	outcome := "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: o-gone\n  name: Bruised fruit gone\nspec:\n  level: outcome\n  parent: depots-stay-open-through-the-season\n  objective: Bruised fruit is no longer sold to buyers\n"
	for _, c := range []struct {
		good float64
		want string
	}{{0.9, "ok"}, {0.6, "warn"}} {
		m := &model{answer: func(string, decide.Question) decide.Answer {
			return decide.Answer{Probabilities: map[string]float64{"state": c.good, "action": 1 - c.good}}
		}}
		e := engineWith(t, m)
		mustCommit(t, e, "Goal", "o-gone", "local", outcome)
		checks, err := e.GoalChecks(ctx, "o-gone")
		if err != nil {
			t.Fatal(err)
		}
		got := ""
		for _, ch := range checks {
			if ch.ID == "statement-state" {
				got = ch.State
			}
		}
		if got != c.want {
			t.Errorf("good at %.1f: %q, want %q", c.good, got, c.want)
		}
	}
}

// A text that names a person is a warning on any kind, pinned to its
// field; with no model the check is off, and absent.
func TestATextNamingAPersonIsAWarning(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	model := fake.New().On("names-person-0", func(state string, _ decide.Question) decide.Answer {
		if strings.Contains(state, "Mensah") {
			return fake.Yes(0.8)
		}
		return fake.Yes(0.05)
	})
	y := []byte("apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: t-named\n  name: Grading team\nspec:\n  purpose: Ada Mensah grades the produce at intake\n")
	for _, c := range []struct {
		d    decide.Decider
		want string
	}{{model, "warn"}, {nil, ""}} {
		opts := []engine.Option{engine.WithCodec(codecyaml.New())}
		if c.d != nil {
			opts = append(opts, engine.WithDecider(c.d))
		}
		e, err := engine.New(memory.NewManifestStore(), memory.NewOperationalStore(), opts...)
		if err != nil {
			t.Fatal(err)
		}
		checks, err := e.ChecksOf(ctx, "Team", "t-named", y)
		if err != nil {
			t.Fatal(err)
		}
		got := ""
		for _, ch := range checks {
			if ch.ID == "names-person" {
				got = ch.State
				if !strings.Contains(ch.Message, "/spec/purpose") {
					t.Errorf("not pinned to its field: %q", ch.Message)
				}
			}
		}
		if got != c.want {
			t.Errorf("model %v: %q, want %q", c.d != nil, got, c.want)
		}
	}
}
