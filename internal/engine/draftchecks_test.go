package engine_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	codecyaml "github.com/ProjectCartograph/cartograph-engine/v2/internal/codec/yaml"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/identity"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store/memory"
)

// An agent defines several manifests in one change set, each naming the
// one before it. Their checks read one another's drafts, not the stored
// record, where none of them exists yet: a project aligned to a drafted
// outcome is aligned, a drafted programme has the drafted project as a
// component, and a drafted planned service is set up by it.
func TestChecksReadTheDraftsInTheirChangeSet(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	agent := actingAs(identity.Principal{Subject: "ada@example.org", Email: "ada@example.org", Name: "Ada", Agent: "Claude", Grant: "g1"})
	save := func(kind, id, text string) {
		t.Helper()
		if err := e.SaveInChangeSet(agent, "", kind, id, []byte(text)); err != nil {
			t.Fatalf("save %s/%s: %v", kind, id, err)
		}
	}
	save("Goal", "outcome-new", "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: outcome-new\n  name: Faults found at intake\nspec:\n  level: outcome\n  objective: Faults are found at intake\n")
	save("Programme", "programme-new", programmeYAML("programme-new", ""))
	save("Operation", "service-new", "apiVersion: cartograph/v1\nkind: Operation\nmetadata:\n  id: service-new\n  name: Checks\nspec:\n  purpose: Check deliveries\n  team: t1\n  status: planned\n")
	save("Segment", "rural", "apiVersion: cartograph/v1\nkind: Segment\nmetadata:\n  id: rural\n  name: Rural\nspec:\n  name: Rural\n  description: Depots outside the towns.\n")
	save("Gap", "gap-new", "apiVersion: cartograph/v1\nkind: Gap\nmetadata:\n  id: gap-new\n  name: Faults reach buyers\nspec:\n  outcomes: [outcome-new]\n  segments: [rural]\n")
	project := strings.Replace(projectYAML("project-new", "  alignment:\n    goals: [outcome-new]\n    programmes: [programme-new]\n  operation: service-new\n"),
		"        change: {what: No more gap}\n", "        change: {what: No more gap}\n        gaps: [{gap: gap-new, segments: [rural]}]\n", 1)
	save("Project", "project-new", project)

	cs, err := e.WorkingChangeSet(agent, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := e.InChangeSet(agent, cs.ID)
	if err != nil {
		t.Fatal(err)
	}
	state := func(kind, id, check string) engine.Check {
		t.Helper()
		checks, err := e.DraftChecks(ctx, kind, id)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range checks {
			if c.ID == check {
				return c
			}
		}
		t.Fatalf("%s/%s has no %s", kind, id, check)
		return engine.Check{}
	}
	if c := state("Project", "project-new", "goals-functional-level"); c.State != "ok" {
		t.Errorf("a project aligned to a drafted outcome: %+v", c)
	}
	if c := state("Programme", "programme-new", "components-present"); c.State != "ok" {
		t.Errorf("a drafted programme with a drafted member: %+v", c)
	}
	if c := state("Operation", "service-new", "service-status"); c.State != "ok" {
		t.Errorf("a drafted planned service a drafted project sets up: %+v", c)
	}
	if c := state("Gap", "gap-new", "gap-covered"); c.State != "ok" {
		t.Errorf("a drafted gap a drafted project works on: %+v", c)
	}
}

// A goal drafted beside the purpose is judged relevant against it, not
// told there is no vision or mission to judge it by.
func TestAGoalIsJudgedAgainstADraftedPurpose(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	agent := actingAs(identity.Principal{Subject: "ada@example.org", Email: "ada@example.org", Name: "Ada", Agent: "Claude", Grant: "g1"})
	for _, d := range []struct{ kind, id, text string }{
		{"Purpose", "default", "apiVersion: cartograph/v1\nkind: Purpose\nmetadata:\n  id: default\n  name: Purpose\nspec:\n  vision: Every member's produce reaches a buyer sound\n"},
		{"Goal", "goal-new", "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: goal-new\n  name: Sound produce\nspec:\n  level: goal\n  objective: Sound produce\n"},
	} {
		if err := e.SaveInChangeSet(agent, "", d.kind, d.id, []byte(d.text)); err != nil {
			t.Fatal(err)
		}
	}
	cs, _ := e.WorkingChangeSet(agent, "")
	ctx, err := e.InChangeSet(agent, cs.ID)
	if err != nil {
		t.Fatal(err)
	}
	checks, err := e.DraftChecks(ctx, "Goal", "goal-new")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range checks {
		if c.ID == "smart-relevant" && strings.Contains(c.Message, "no vision or mission") {
			t.Fatalf("the drafted purpose was not read: %+v", c)
		}
	}
}

// An agent's draft is held to the strict profile (docs/adr/0027): a
// second objective, or a person's name on a role, is never saved; a draft
// that is only not finished is.
func TestAnAgentCannotSaveAnImproperShape(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	agent := actingAs(identity.Principal{Subject: "ada@example.org", Email: "ada@example.org", Name: "Ada", Agent: "Claude", Grant: "g1"})
	two := "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: p-two\n  name: Two aims\nspec:\n  objectives:\n    - objective: Faults are found at intake\n    - objective: Buyers are paid on time\n"
	if err := e.SaveInChangeSet(agent, "", "Project", "p-two", []byte(two)); err == nil || !strings.Contains(err.Error(), "/spec/objectives") {
		t.Errorf("a second objective: %v", err)
	}
	person := "apiVersion: cartograph/v1\nkind: Resource\nmetadata:\n  id: r-lead\n  name: Dr. Ada Mensah\nspec:\n  category: personRole\n"
	if err := e.SaveInChangeSet(agent, "", "Resource", "r-lead", []byte(person)); err == nil {
		t.Error("a person's name on a role was saved")
	}
	if _, err := e.EditInChangeSet(agent, "", "Project", "p-one", map[string]any{"/metadata/name": "One aim"}, nil); err != nil {
		t.Errorf("an unfinished draft: %v", err)
	}
	// A person is not held to it here: their drafts reach the record
	// through a version save and the blocking checks, as before.
	if err := e.SaveInChangeSet(actingAs(identity.Principal{Subject: "ada@example.org", Email: "ada@example.org"}), "", "Project", "p-two", []byte(two)); err != nil {
		t.Errorf("a person's draft: %v", err)
	}
}

// A register drafted from a name gets what it needs to be valid, read
// from the name; a near enough name is the same record.
func TestNamedRegistersAreReadFromTheirNames(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	agent := actingAs(identity.Principal{Subject: "ada@example.org", Email: "ada@example.org", Name: "Ada", Agent: "Claude", Grant: "g1"})
	if _, err := e.EditInChangeSet(agent, "", "Project", "p-reg", map[string]any{"/metadata/name": "Rollout"}, nil); err != nil {
		t.Fatal(err)
	}
	sets, err := e.ChangeSets(agent, "", false)
	if err != nil || len(sets) == 0 {
		t.Fatalf("change sets: %v", err)
	}
	set := sets[0].ID
	fields, created, err := e.NamedRefs(agent, set, "Project", map[string]any{
		"/spec/resources": []any{
			map[string]any{"role": "sponsor", "resource": "Steering Committee"},
			map[string]any{"role": "manager", "resource": "Education Health Services Unit"},
			map[string]any{"role": "teamMember", "resource": "Education Health Services Unit (EHSU)"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(created) != 2 {
		t.Fatalf("drafted %v from %v", created, fields)
	}
	for _, r := range created {
		text, _, _ := e.ChangeSetText(agent, set, "Resource", strings.TrimPrefix(r, "Resource/"))
		if !strings.Contains(string(text), "category: governanceBody") && !strings.Contains(string(text), "category: orgUnit") {
			t.Errorf("%s: %s", r, text)
		}
	}
}

// A team is a body, never a person: an agent cannot draft one named so.
func TestAnAgentCannotNameATeamAfterAPerson(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	agent := actingAs(identity.Principal{Subject: "ada@example.org", Email: "ada@example.org", Name: "Ada", Agent: "Claude", Grant: "g1"})
	team := "apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: t-person\n  name: Dr. Ada Mensah\nspec: {}\n"
	if err := e.SaveInChangeSet(agent, "", "Team", "t-person", []byte(team)); err == nil {
		t.Error("a team named after a person was saved")
	}
}

// A second objective is refused by the engine for everyone, at every
// version save and every proposal: the rule rests on no writer, person
// or model, getting it right. A project stored with two before the rule
// keeps them and may drop one, never add a third.
func TestNoVersionBringsInASecondObjective(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := seededEngine(t)
	two := func(id string, n int) []byte {
		objs := ""
		for i := 0; i < n; i++ {
			objs += fmt.Sprintf("    - objective: A change %s happens\n", strings.Repeat("again ", i))
		}
		return []byte("apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: " + id + "\n  name: Rollout " + id + "\nspec:\n  objectives:\n" + objs)
	}
	onObjectives := func(err error) bool {
		var invalid *engine.ValidationError
		if !errors.As(err, &invalid) {
			return false
		}
		for _, p := range invalid.Problems {
			if strings.HasSuffix(p.Path, "/spec/objectives") {
				return true
			}
		}
		return false
	}
	if _, err := e.Commit(ctx, "Project", "p-two", two("p-two", 2), "ada@example.org", "two"); !onObjectives(err) {
		t.Fatalf("a person's save of two objectives: %v", err)
	}
	agent := actingAs(identity.Principal{Subject: "ada@example.org", Email: "ada@example.org", Name: "Ada", Agent: "Claude", Grant: "g1"})
	if _, err := e.ProposeSave(agent, "Project", "p-two", two("p-two", 2), "two", map[string]string{"goals-objective": "we want two"}); !onObjectives(err) {
		t.Fatalf("a waived proposal of two objectives: %v", err)
	}
}

// A deployed vault that already holds a project with two objectives still
// imports and saves: the project may keep or drop one, never gain a third.
func TestAProjectStoredWithTwoObjectivesMayOnlyShed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(exampleDir(t))); err != nil {
		t.Fatal(err)
	}
	e, err := engine.New(memory.NewManifestStore(), memory.NewOperationalStore(), engine.WithCodec(codecyaml.New()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.ImportDir(ctx, dir, "alice-nkemah", "seed"); err != nil {
		t.Fatal(err)
	}
	v, err := e.Get(ctx, "Project", "quality-check-rollout")
	if err != nil {
		t.Fatal(err)
	}
	one := string(v.YAML)
	at := strings.Index(one, "  objectives:\n")
	if at < 0 {
		t.Fatalf("no objectives in %s", one)
	}
	at += len("  objectives:\n")
	with := func(n int) []byte {
		extra := ""
		for i := 2; i <= n; i++ {
			extra += fmt.Sprintf("    - id: objective-%d\n      objective: Another change %s happens at every depot\n", i, strings.Repeat("again ", i))
		}
		return []byte(one[:at] + extra + one[at:])
	}
	// A vault written before the rule, with two: it still imports.
	if err := os.WriteFile(filepath.Join(dir, "Project", "quality-check-rollout.yaml"), with(2), 0o644); err != nil {
		t.Fatal(err)
	}
	e, err = engine.New(memory.NewManifestStore(), memory.NewOperationalStore(), engine.WithCodec(codecyaml.New()))
	if err != nil {
		t.Fatal(err)
	}
	if rep, err := e.ImportDir(ctx, dir, "alice-nkemah", "restore"); err != nil || len(rep.Problems) > 0 {
		t.Fatalf("a vault with two objectives no longer imports: %v %+v", err, rep.Problems)
	}
	if _, err := e.Commit(ctx, "Project", "quality-check-rollout", with(2), "alice-nkemah", "keep"); err != nil {
		t.Errorf("keeping the two it had: %v", err)
	}
	if _, err := e.Commit(ctx, "Project", "quality-check-rollout", with(3), "alice-nkemah", "grow"); err == nil {
		t.Error("a third objective was saved")
	}
	if _, err := e.Commit(ctx, "Project", "quality-check-rollout", []byte(one), "alice-nkemah", "shed"); err != nil {
		var iv *engine.ValidationError
		errors.As(err, &iv)
		t.Errorf("dropping to one: %+v", iv.Problems[:3])
	}
}

// No person appears in Cartograph, in any text: a note naming someone by
// their title is refused, an agent's draft as much as a person's save.
func TestAPersonIsNamedInNoText(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	agent := actingAs(identity.Principal{Subject: "ada@example.org", Email: "ada@example.org", Name: "Ada", Agent: "Claude", Grant: "g1"})
	cs, err := e.StartChangeSet(agent, "Roles", "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.EditInChangeSet(agent, cs.ID, "Project", "p-note", map[string]any{
		"/metadata/name":  "Rollout",
		"/spec/resources": []any{map[string]any{"role": "manager", "note": "Dr. M. Mensah with the quality team"}},
	}, nil)
	var invalid *engine.ValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("a titled name in a note: %v", err)
	}
	named := false
	for _, p := range invalid.Problems {
		named = named || (p.Path == "/spec/resources/0/note" && strings.Contains(p.Message, "never people"))
	}
	if !named {
		t.Fatalf("problems %+v", invalid.Problems)
	}
	if _, err := e.EditInChangeSet(agent, cs.ID, "Project", "p-note", map[string]any{
		"/metadata/name":  "Rollout",
		"/spec/resources": []any{map[string]any{"role": "manager", "note": "The lead analyst, with the quality team"}},
	}, nil); err != nil {
		t.Fatalf("a role named instead: %v", err)
	}
}
