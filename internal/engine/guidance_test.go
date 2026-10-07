package engine_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
)

// The guide for an outcome: the outcome's own words and examples, a
// parent chosen among objectives only, and the gap it closes asked for.
func TestTheGuideForAnOutcome(t *testing.T) {
	t.Parallel()
	e := seedReadings(t)
	g, err := e.Guide(context.Background(), "Goal", "outcome", "fr")
	if err != nil {
		t.Fatal(err)
	}
	if g.Locale != "en" || !strings.Contains(g.LevelIs, "people or things") || g.Definition == "" {
		t.Fatalf("the guide's head: %+v", g)
	}
	fields := map[string]bool{}
	var link bool
	for _, st := range g.Steps {
		for _, f := range st.Fields {
			fields[f.Path] = true
			switch f.Path {
			case "/spec/objective":
				if !strings.Contains(f.Guide, "true") || len(f.Poor) == 0 || !strings.Contains(f.Poor[0].Text, "Reduce delivery time") {
					t.Errorf("the outcome statement's words: %+v", f)
				}
			case "/spec/parent":
				if f.References != "Goal" || len(f.Candidates) == 0 {
					t.Errorf("the parent's candidates: %+v", f)
				}
				for _, c := range f.Candidates {
					if c.Detail != "objective" {
						t.Errorf("an outcome's parent offered at level %q: %s", c.Detail, c.ID)
					}
				}
			}
		}
		for _, l := range st.Links {
			if l.Kind == "Gap" && l.Path == "/spec/outcomes" && l.Ask != "" && l.IfNone != "" {
				link = true
			}
		}
	}
	if !fields["/spec/keyResults/-/metric"] || !link {
		t.Fatalf("fields %v, gap link %v", fields, link)
	}
	if goal, _ := e.Guide(context.Background(), "Goal", "goal", "en"); len(goal.Steps) == 0 {
		t.Fatal("no guide for a goal")
	} else {
		// A goal is asked for the objectives under it, never for a gap.
		for _, st := range goal.Steps {
			for _, l := range st.Links {
				if l.Kind != "Goal" || l.Check != "has-objectives" {
					t.Errorf("a goal's link: %+v", l)
				}
				for _, c := range l.Candidates {
					if c.Detail != "objective" {
						t.Errorf("a goal offered %s at level %q to place under it", c.ID, c.Detail)
					}
				}
			}
		}
		if goal.Template["apiVersion"] != "cartograph/v1" || goal.Template["spec"].(map[string]any)["level"] != "goal" {
			t.Errorf("the goal's template: %v", goal.Template)
		}
		for _, c := range goal.Existing {
			if c.Detail != "goal" {
				t.Errorf("an existing record at another level: %+v", c)
			}
		}
	}
}

// Every check a flow or a guidance bundle names is one the engine
// reports: read from the engine's own source, where each check is added
// by id.
func TestGuidanceNamesOnlyRealChecks(t *testing.T) {
	t.Parallel()
	ids := map[string]bool{}
	src, _ := filepath.Glob("*.go")
	re := regexp.MustCompile(`(?:\badd(?:Fix)?\(|ID:\s*)"([a-z0-9-]+)"`)
	for _, f := range src {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			ids[m[1]] = true
		}
	}
	// A judgement is a check the engine reports from the guidance itself,
	// asked of a decision model (judgedChecks).
	judged, _ := filepath.Glob("../contract/guidance/*/*.guidance.json")
	for _, f := range judged {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var bundle struct {
			Judgements map[string]json.RawMessage `json:"judgements"`
		}
		if err := json.Unmarshal(b, &bundle); err != nil {
			t.Fatal(err)
		}
		for id := range bundle.Judgements {
			ids[id] = true
		}
	}
	if !ids["smart-specific"] || !ids["closes-gap"] || !ids["gap-states"] {
		t.Fatalf("the engine's check ids were not found: %d", len(ids))
	}
	flows, _ := filepath.Glob("../contract/flows/*.flow.json")
	bundles, _ := filepath.Glob("../contract/guidance/*/*.guidance.json")
	named := regexp.MustCompile(`"checks":\s*\[([^\]]*)\]|"check":\s*"([a-z0-9-]+)"`)
	for _, f := range append(flows, bundles...) {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range named.FindAllStringSubmatch(string(b), -1) {
			for _, id := range regexp.MustCompile(`"([a-z0-9-]+)"`).FindAllStringSubmatch(m[1], -1) {
				if !ids[id[1]] {
					t.Errorf("%s names check %q, which the engine never reports", filepath.Base(f), id[1])
				}
			}
			if m[2] != "" && !ids[m[2]] {
				t.Errorf("%s names check %q, which the engine never reports", filepath.Base(f), m[2])
			}
		}
		if strings.HasSuffix(f, ".guidance.json") {
			var g struct {
				Checks map[string]string `json:"checks"`
			}
			if err := json.Unmarshal(b, &g); err != nil {
				t.Fatal(err)
			}
			for id := range g.Checks {
				if !ids[id] {
					t.Errorf("%s says how to meet %q, which the engine never reports", filepath.Base(f), id)
				}
			}
		}
	}
}

// The guide marks what a definition cannot leave out: what the schema
// requires where it sits, and what a handoff waits on, and nothing else.
func TestTheGuideMarksRequiredFields(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	g, err := e.Guide(context.Background(), "Project", "", "en")
	if err != nil {
		t.Fatal(err)
	}
	required := map[string]bool{}
	seen := map[string]bool{}
	for _, st := range g.Steps {
		for _, f := range st.Fields {
			seen[f.Path] = true
			required[f.Path] = required[f.Path] || f.Required
		}
	}
	for path, want := range map[string]bool{
		"/metadata/name":                   true,  // the schema requires a name
		"/spec/deliverables":               true,  // a handoff waits on a deliverable
		"/spec/deliverables/-/name":        true,  // every deliverable has a name
		"/spec/deliverables/-/tasks":       false, // tasks are optional
		"/spec/deliverables/-/description": false,
		"/spec/alignment/partOf":           false, // only a component names a parent
		"/spec/alignment/goals":            false, // and a component names no goal
	} {
		if !seen[path] {
			t.Errorf("the guide has no %s", path)
		} else if required[path] != want {
			t.Errorf("%s required = %v, want %v", path, required[path], want)
		}
	}
}

// A guide's field says what the schema holds it to, so an agent writes a
// value the save will take without reading the schema.
func TestTheGuideGivesEachFieldItsLimitsChoicesAndShape(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	g, err := e.Guide(context.Background(), "Project", "", "en")
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]engine.GuideField{}
	for _, st := range g.Steps {
		for _, f := range st.Fields {
			byPath[f.Path] = f
		}
	}
	if f := byPath["/spec/deliverables/-/name"]; f.MaxLength != 60 {
		t.Errorf("a deliverable's name: %+v", f)
	}
	if f := byPath["/spec/mandate/-/kind"]; len(f.Values) != 6 {
		t.Errorf("a mandate's kind: %+v", f)
	}
	if f := byPath["/spec/timeline/start"]; f.Format != "YYYY-MM" {
		t.Errorf("the start: %+v", f)
	}
	if f := byPath["/spec/mandate/-/issuer"]; !strings.Contains(f.Shape, `{"local":"resources","id":"<role id>"}`) {
		t.Errorf("a reference's shape: %+v", f)
	}
	if f := byPath["/spec/objectives/-/keyResults/-/baseline"]; !strings.Contains(f.Shape, `"unknownReason"`) {
		t.Errorf("a baseline's shape: %+v", f)
	}
}

// A kind's schema comes with the shared definitions it points to, so
// metadata.alias and a reference's forms are readable without resolving.
func TestASchemaComesWithItsSharedDefinitions(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	defs, err := e.SchemaDefs("Project")
	if err != nil {
		t.Fatal(err)
	}
	meta, _ := defs["Metadata"].(map[string]any)
	props, _ := meta["properties"].(map[string]any)
	if _, ok := props["alias"]; !ok || defs["Ref"] == nil || defs["KeyResult"] == nil {
		t.Fatalf("the shared definitions: %v", func() []string {
			var names []string
			for k := range defs {
				names = append(names, k)
			}
			return names
		}())
	}
}
