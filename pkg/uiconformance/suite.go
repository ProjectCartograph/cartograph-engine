// Package uiconformance is the one suite every interface to Cartograph must
// pass: the web interface, the terminal interface, and whatever an
// organisation builds. It is public so an interface in its own
// repository can import it and run it in its own CI.
//
// The suite drives an interface through the Driver protocol, a small
// vocabulary of what a person does (open a flow, go to a step, set a
// field, act), and reads the vault back through a client.Client. It
// asserts on two things only: what the vault holds afterwards, and
// which problems the interface showed at which fields. It never looks
// at a widget, a pixel or a key binding, so two interfaces that pass
// it behave the same and a person can move between them.
//
// The scenarios are data (scenarios/*.json), so a runner in another
// language can execute the same ones against an interface written in
// that language; this package is the Go runner and the reference.
package uiconformance

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/pkg/client"
)

//go:embed scenarios/*.json
var scenarioFS embed.FS

// Field is a JSON pointer into the manifest being edited, for example
// "/spec/objective" or "/spec/keyResults/{kr-1}/target" (a keyed list
// element by its key).
type Field string

// Action is something a person does that is not filling a field.
type Action string

// The actions every flow offers.
const (
	ActSave        Action = "save"        // keep the working copy (never refused for incompleteness)
	ActSaveVersion Action = "saveVersion" // save a version; refused with problems when invalid
	ActDiscard     Action = "discard"     // drop the working copy
	ActNext        Action = "next"
	ActBack        Action = "back"
	ActApply       Action = "apply" // include an unapplied manifest in the live state
)

// Problem is one thing the interface shows as wrong: at a field, with a
// message. The same shape the engine answers, because that is where it
// comes from.
type Problem struct {
	Field   Field
	Message string
}

// Location is where the interface is: which flow (a kind), on which
// manifest, at which step.
type Location struct {
	Kind string
	ID   string
	Step string
}

// Driver is what an interface implements to be driven. A method returns
// an error only when the interface could not do what was asked (the
// control is missing, the step does not exist); a refusal shown to the
// person is not an error, it is a Problem.
type Driver interface {
	Open(ctx context.Context, kind, id string) (string, error)
	Step(ctx context.Context, key string) error
	Set(ctx context.Context, field Field, value any) error
	Act(ctx context.Context, action Action, reason string) error
	Problems(ctx context.Context) ([]Problem, error)
	Where(ctx context.Context) (Location, error)
	Close(ctx context.Context) error
}

// Scenario is one behaviour every interface must have, as steps.
type Scenario struct {
	Key   string `json:"key"`
	Name  string `json:"name"`
	Rule  string `json:"rule"` // the DESIGN_RULES line it holds
	Steps []Step `json:"steps"`
}

// Step is one instruction to the driver, or one expectation. Exactly
// one of the fields below is set.
type Step struct {
	Open *struct{ Kind, ID string } `json:"open,omitempty"`
	Step string                     `json:"step,omitempty"`
	Set  *struct {
		Field Field
		Value any
	} `json:"set,omitempty"`
	Act *struct {
		Action Action
		Reason string
	} `json:"act,omitempty"`
	Expect *Expect `json:"expect,omitempty"`
}

// Expect is an expectation checked against the interface or the vault.
type Expect struct {
	NoProblems bool `json:"noProblems,omitempty"`
	// Problem: a problem at Field whose message contains Contains.
	Problem *struct {
		Field    Field  `json:"field"`
		Contains string `json:"contains"`
	} `json:"problem,omitempty"`
	// Where: the interface is at this step.
	Where string `json:"where,omitempty"`
	// Current: the committed version's document has Field equal to
	// Value (Field "" with Version checks only the number).
	Current *struct {
		Field   Field `json:"field"`
		Value   any   `json:"value"`
		Version int   `json:"version"`
		Absent  bool  `json:"absent"`
	} `json:"current,omitempty"`
	// Working: the working copy has Field equal to Value.
	Working *struct {
		Field Field `json:"field"`
		Value any   `json:"value"`
	} `json:"working,omitempty"`
	// IDIsIdentifier: the id the interface is editing has no spaces.
	IDIsIdentifier bool `json:"idIsIdentifier,omitempty"`
}

// All loads every scenario from the embedded files, sorted by key.
func All() ([]Scenario, error) {
	entries, err := fs.ReadDir(scenarioFS, "scenarios")
	if err != nil {
		return nil, err
	}
	var out []Scenario
	for _, e := range entries {
		b, err := scenarioFS.ReadFile("scenarios/" + e.Name())
		if err != nil {
			return nil, err
		}
		var sc Scenario
		if err := json.Unmarshal(b, &sc); err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		out = append(out, sc)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// Run executes every scenario against a fresh driver and client. The
// factory is called once per scenario so scenarios share no state.
func Run(t *testing.T, factory func(t *testing.T) (Driver, client.Client)) {
	t.Helper()
	scenarios, err := All()
	if err != nil {
		t.Fatal(err)
	}
	for _, sc := range scenarios {
		t.Run(sc.Key, func(t *testing.T) {
			ctx := context.Background()
			d, c := factory(t)
			t.Cleanup(func() { _ = d.Close(ctx) })
			if err := Execute(ctx, sc, d, c); err != nil {
				t.Fatalf("%s: %v", sc.Name, err)
			}
		})
	}
}

// Execute runs one scenario and returns the first failed step as an
// error naming the step number and what was expected.
func Execute(ctx context.Context, sc Scenario, d Driver, c client.Client) error {
	var id string
	for i, st := range sc.Steps {
		fail := func(format string, args ...any) error {
			return fmt.Errorf("step %d: %s", i+1, fmt.Sprintf(format, args...))
		}
		switch {
		case st.Open != nil:
			got, err := d.Open(ctx, st.Open.Kind, st.Open.ID)
			if err != nil {
				return fail("open %s: %v", st.Open.Kind, err)
			}
			id = got
		case st.Step != "":
			if err := d.Step(ctx, st.Step); err != nil {
				return fail("step %q: %v", st.Step, err)
			}
		case st.Set != nil:
			if err := d.Set(ctx, st.Set.Field, st.Set.Value); err != nil {
				return fail("set %s: %v", st.Set.Field, err)
			}
		case st.Act != nil:
			if err := d.Act(ctx, st.Act.Action, st.Act.Reason); err != nil {
				return fail("act %s: %v", st.Act.Action, err)
			}
		case st.Expect != nil:
			if err := check(ctx, st.Expect, d, c, id); err != nil {
				return fail("%v", err)
			}
		default:
			return fail("empty step")
		}
	}
	return nil
}

func check(ctx context.Context, ex *Expect, d Driver, c client.Client, id string) error {
	loc, err := d.Where(ctx)
	if err != nil {
		return err
	}
	if ex.NoProblems {
		ps, err := d.Problems(ctx)
		if err != nil {
			return err
		}
		if len(ps) != 0 {
			return fmt.Errorf("expected no problems, the interface shows %+v", ps)
		}
	}
	if ex.Problem != nil {
		ps, err := d.Problems(ctx)
		if err != nil {
			return err
		}
		hit := false
		for _, p := range ps {
			if p.Field == ex.Problem.Field && strings.Contains(p.Message, ex.Problem.Contains) {
				hit = true
			}
		}
		if !hit {
			return fmt.Errorf("expected a problem at %s containing %q, the interface shows %+v", ex.Problem.Field, ex.Problem.Contains, ps)
		}
	}
	if ex.Where != "" && loc.Step != ex.Where {
		return fmt.Errorf("expected to be at step %q, the interface is at %q", ex.Where, loc.Step)
	}
	if ex.Current != nil {
		m, err := c.Get(ctx, loc.Kind, id)
		if ex.Current.Absent {
			if err == nil && m.Version != 0 {
				return fmt.Errorf("expected no version, the vault holds version %d", m.Version)
			}
		} else {
			if err != nil {
				return fmt.Errorf("expected a version in the vault: %v", err)
			}
			if ex.Current.Version != 0 && m.Version != ex.Current.Version {
				return fmt.Errorf("expected version %d, the vault holds %d", ex.Current.Version, m.Version)
			}
			if ex.Current.Field != "" {
				if got := lookup(m.Doc, ex.Current.Field); !equal(got, ex.Current.Value) {
					return fmt.Errorf("expected %s = %v in the version, the vault holds %v", ex.Current.Field, ex.Current.Value, got)
				}
			}
		}
	}
	if ex.Working != nil {
		m, err := c.Working(ctx, loc.Kind, id)
		if err != nil {
			return fmt.Errorf("expected a working copy in the vault: %v", err)
		}
		if got := lookup(m.Doc, ex.Working.Field); !equal(got, ex.Working.Value) {
			return fmt.Errorf("expected %s = %v in the working copy, the vault holds %v", ex.Working.Field, ex.Working.Value, got)
		}
	}
	if ex.IDIsIdentifier && (id == "" || strings.ContainsAny(id, " \t")) {
		return fmt.Errorf("an id is an identifier, never prose: %q", id)
	}
	return nil
}

// lookup reads a plain JSON pointer out of a document (keyed-list
// segments are not resolved here: expectations name scalar fields).
func lookup(doc map[string]any, field Field) any {
	var cur any = doc
	for _, seg := range strings.Split(strings.TrimPrefix(string(field), "/"), "/") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[seg]
	}
	return cur
}

func equal(a, b any) bool {
	switch x := a.(type) {
	case int:
		if y, ok := b.(float64); ok {
			return float64(x) == y
		}
	case float64:
		if y, ok := b.(int); ok {
			return x == float64(y)
		}
	}
	return fmt.Sprint(a) == fmt.Sprint(b)
}
