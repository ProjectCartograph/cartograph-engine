package engine_test

import (
	"context"
	"strings"
	"testing"
)

// Cartograph points at things in one shape everywhere: kind+id for another
// manifest, local+id for an item inside this manifest, external for a
// target the vault will never hold. The three joins that used to store a
// copy of a role's title — an acceptance criterion's verifier and a
// success criterion's owner and confirmer — became references on
// 2026-09-28, so renaming a role can no longer leave stale copies behind.
func TestProjectReferenceForms(t *testing.T) {
	e := seededEngine(t)
	// A project with two named roles to point at.
	base := "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: proj-ref\n  name: Ref\nspec:\n  team: t1\n" +
		"  resources:\n    - {id: quality-reviewer, role: teamMember}\n" +
		"    - {id: sponsor, role: sponsor}\n" +
		"  summary:\n    problems:\n      - problem: {situation: A gap}\n        change: {what: No more gap}\n"

	runSchemaCases(t, e, []schemaCase{
		{
			name: "a local reference to a role this project has", kind: "Project",
			yaml: base + "  successCriteria:\n    - id: sc-1\n      statement: Coverage is close to complete\n      metric: compliance\n      owner: {local: resources, id: quality-reviewer}\n      confirmedBy: {local: resources, id: sponsor}\n      when: atClosing\n",
		},
		{
			// A governance body the vault has no manifest for is named as
			// written rather than invented as a manifest.
			name: "an external reference needs no id", kind: "Project",
			yaml: base + "  successCriteria:\n    - id: sc-1\n      statement: Coverage is close to complete\n      metric: compliance\n      confirmedBy: {external: The the board}\n      when: atClosing\n",
		},
		{
			name: "a local reference to a role this project does not have", kind: "Project",
			wantProblem: true, wantSubstr: "does not exist in this project",
			yaml: base + "  successCriteria:\n    - id: sc-1\n      statement: Coverage is close to complete\n      metric: compliance\n      confirmedBy: {local: resources, id: nobody}\n      when: atClosing\n",
		},
		{
			name: "a local reference to a list nothing can point at", kind: "Project",
			wantProblem: true, wantSubstr: "not a list references can point at",
			yaml: base + "  successCriteria:\n    - id: sc-1\n      statement: Coverage is close to complete\n      metric: compliance\n      confirmedBy: {local: funding, id: sponsor}\n      when: atClosing\n",
		},
		{
			// The three variants are exclusive: a reference that is both a
			// manifest and an item inside one names nothing coherent.
			name: "kind and local together", kind: "Project", wantProblem: true,
			yaml: base + "  successCriteria:\n    - id: sc-1\n      statement: Coverage is close to complete\n      metric: compliance\n      confirmedBy: {kind: Team, local: resources, id: sponsor}\n      when: atClosing\n",
		},
		{
			name: "a cross-manifest reference that does not resolve", kind: "Project",
			wantProblem: true, wantSubstr: "does not exist",
			yaml: base + "  successCriteria:\n    - id: sc-1\n      statement: Coverage is close to complete\n      metric: compliance\n      confirmedBy: {kind: Team, id: no-such-team}\n      when: atClosing\n",
		},
		{
			// The whole point of the change: a bare title is no longer a
			// reference and is refused at the write path.
			name: "the old title string is refused", kind: "Project", wantProblem: true,
			yaml: base + "  successCriteria:\n    - id: sc-1\n      statement: Coverage is close to complete\n      metric: compliance\n      confirmedBy: Sponsor\n      when: atClosing\n",
		},
	})
}

// A dependency carries an edge, and the edge is the whole reason the type
// exists: before it, a row typed dependency was usually a risk sentence
// filed under the wrong word.
func TestProjectDependencyEdge(t *testing.T) {
	e := seededEngine(t)
	base := "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: proj-dep\n  name: Dep\nspec:\n  team: t1\n" +
		"  timeline:\n    start: \"2026-01\"\n    phases:\n      - {id: design, name: Design, months: 3}\n" +
		"  summary:\n    problems:\n      - problem: {situation: A gap}\n        change: {what: No more gap}\n"

	runSchemaCases(t, e, []schemaCase{
		{
			name: "a dependency on another manifest, landing by a phase", kind: "Project",
			yaml: base + "  risks:\n    - description: The feed must carry the new field\n      type: dependency\n      depends: {direction: needs, on: {kind: DataSource, id: d1}, needBy: design}\n",
		},
		{
			name: "a dependency on something outside Cartograph", kind: "Project",
			yaml: base + "  risks:\n    - description: Approval must be granted\n      type: dependency\n      depends: {direction: needs, on: {external: The the board}}\n",
		},
		{
			// Direction is what makes the edge a tree rather than a cloud,
			// so it is required on the edge itself.
			name: "an edge with no direction", kind: "Project", wantProblem: true,
			yaml: base + "  risks:\n    - description: The feed must carry the new field\n      type: dependency\n      depends: {on: {kind: DataSource, id: d1}}\n",
		},
		{
			name: "an edge on a row that is not a dependency", kind: "Project",
			wantProblem: true, wantSubstr: "only a dependency carries an edge",
			yaml: base + "  risks:\n    - description: The vendor may be late\n      type: risk\n      depends: {direction: needs, on: {kind: DataSource, id: d1}}\n",
		},
		{
			name: "needBy names a phase the timeline does not have", kind: "Project",
			wantProblem: true, wantSubstr: "which this timeline does not have",
			yaml: base + "  risks:\n    - description: The feed must carry the new field\n      type: dependency\n      depends: {direction: needs, on: {kind: DataSource, id: d1}, needBy: rollout}\n",
		},
	})
}

// The check half: a dependency with no edge is reported, and edges that
// leave Cartograph are counted rather than faulted.
func TestProjectDependencyChecks(t *testing.T) {
	e := seededEngine(t)
	base := "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: proj-depchk\n  name: DepChk\nspec:\n  team: t1\n" +
		"  summary:\n    problems:\n      - problem: {situation: A gap}\n        change: {what: No more gap}\n" +
		"    scopeIn: [Something]\n" +
		"  timeline:\n    start: \"2026-01\"\n    phases:\n      - {id: design, name: Design, months: 3}\n"

	commit := func(t *testing.T, id, risks string) map[string]string {
		t.Helper()
		yaml := strings.Replace(base, "id: proj-depchk", "id: "+id, 1) + risks
		if _, err := e.Commit(context.Background(), "Project", id, []byte(yaml), "tester", ""); err != nil {
			t.Fatalf("commit %s: %v", id, err)
		}
		checks, err := e.ProjectChecks(context.Background(), id, false)
		if err != nil {
			t.Fatalf("checks %s: %v", id, err)
		}
		out := map[string]string{}
		for _, it := range checks.Items {
			out[it.ID] = it.State + ": " + it.Message
		}
		return out
	}

	t.Run("every dependency says what it waits on", func(t *testing.T) {
		got := commit(t, "dep-ok",
			"  risks:\n    - description: The feed must carry the new field\n      type: dependency\n      depends: {direction: needs, on: {kind: DataSource, id: d1}}\n"+
				"    - description: Approval must be granted\n      type: dependency\n      depends: {direction: needs, on: {external: The the board}}\n")
		want := "ok: Every dependency says what it waits on (1 outside Cartograph)."
		if got["risks-dependency-edges"] != want {
			t.Fatalf("risks-dependency-edges = %q, want %q", got["risks-dependency-edges"], want)
		}
	})

	t.Run("a dependency with no edge is reported", func(t *testing.T) {
		got := commit(t, "dep-bare",
			"  risks:\n    - description: Records cannot be joined across systems\n      type: dependency\n")
		if !strings.HasPrefix(got["risks-dependency-edges"], "warn:") {
			t.Fatalf("expected a warning, got %q", got["risks-dependency-edges"])
		}
		if !strings.Contains(got["risks-dependency-edges"], "1 of 1") {
			t.Fatalf("expected the count, got %q", got["risks-dependency-edges"])
		}
	})

	t.Run("a project with no dependencies gets no check", func(t *testing.T) {
		got := commit(t, "dep-none", "  risks:\n    - description: The vendor may be late\n      type: risk\n")
		if _, present := got["risks-dependency-edges"]; present {
			t.Fatalf("expected no dependency check, got %q", got["risks-dependency-edges"])
		}
	})
}
