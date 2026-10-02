package engine_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A resource and the power it holds are logically distinct: the resource
// is declared once and used across many projects, while the power is
// particular to each, so it is stored on the link. The map is that link.
func TestStakeholderMapRules(t *testing.T) {
	e := seededEngine(t)
	ctx := context.Background()
	// A map is about a piece of work, so there has to be some.
	if _, err := e.Commit(ctx, "Project", "p1", []byte(
		"apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: p1\n  name: P\nspec:\n  team: t1\n"+
			"  summary:\n    problems:\n      - problem: {situation: A gap}\n        change: {what: No more gap}\n"), "local", "seed"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Commit(ctx, "Programme", "prog1", []byte(
		"apiVersion: cartograph/v1\nkind: Programme\nmetadata:\n  id: prog1\n  name: Prog\nspec:\n  aim: {change: Make things better}\n  leadTeam: t1\n"+
			"  problems:\n    - problem: {situation: A gap}\n      change: {what: No more gap}\n"), "local", "seed"); err != nil {
		t.Fatal(err)
	}
	base := "apiVersion: cartograph/v1\nkind: StakeholderMap\nmetadata:\n  id: sm-1\n  name: Map\nspec:\n"

	runSchemaCases(t, e, []schemaCase{
		{
			name: "a map scoped to a project, scoring one resource", kind: "StakeholderMap",
			yaml: base + "  scope: {kind: Project, id: p1}\n  entries:\n    - {resource: r1, influence: 3, interest: 2, tier: primary}\n",
		},
		{
			// The whole reason scoring is optional: naming a stakeholder
			// and assessing their power are separate acts, and the first
			// must be recordable without the second.
			name: "an entry with no score at all", kind: "StakeholderMap",
			yaml: base + "  scope: {kind: Project, id: p1}\n  entries:\n    - {resource: r1}\n",
		},
		{
			name: "a map with no entries yet", kind: "StakeholderMap",
			yaml: base + "  scope: {kind: Project, id: p1}\n",
		},
		{
			name: "scoped to a programme", kind: "StakeholderMap",
			yaml: base + "  scope: {kind: Programme, id: prog1}\n  entries:\n    - {resource: r1}\n",
		},
		{
			name: "scoped to something that is not work", kind: "StakeholderMap",
			wantProblem: true, wantSubstr: "scoped to work, not to a Team",
			yaml: base + "  scope: {kind: Team, id: t1}\n  entries:\n    - {resource: r1}\n",
		},
		{
			name: "scoped to nothing Cartograph holds", kind: "StakeholderMap",
			wantProblem: true, wantSubstr: "scoped to a project, programme or operation",
			yaml: base + "  scope: {external: Somewhere}\n",
		},
		{
			name: "a resource that does not exist", kind: "StakeholderMap",
			wantProblem: true, wantSubstr: "does not exist",
			yaml: base + "  scope: {kind: Project, id: p1}\n  entries:\n    - {resource: no-such-resource}\n",
		},
		{
			name: "the same stakeholder scored twice", kind: "StakeholderMap",
			wantProblem: true, wantSubstr: "one score per stakeholder",
			yaml: base + "  scope: {kind: Project, id: p1}\n  entries:\n    - {resource: r1, influence: 1}\n    - {resource: r1, influence: 3}\n",
		},
		{
			name: "a score outside the scale", kind: "StakeholderMap", wantProblem: true,
			yaml: base + "  scope: {kind: Project, id: p1}\n  entries:\n    - {resource: r1, influence: 4}\n",
		},
		{
			// The power left the project's own rows; a project that still
			// carries it is refused rather than quietly keeping two homes
			// for one fact.
			name: "a project may not score its own resources", kind: "Project", wantProblem: true,
			yaml: "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: proj-score\n  name: P\nspec:\n  team: t1\n" +
				"  summary:\n    problems:\n      - problem: {situation: A gap}\n        change: {what: No more gap}\n" +
				"  resources:\n    - {role: teamMember, resource: r1, influence: 2}\n",
		},
		{
			// A stakeholder is a resource the project references like any
			// other; only the score moved.
			name: "a project still names its stakeholders", kind: "Project",
			yaml: "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: proj-sh\n  name: P\nspec:\n  team: t1\n" +
				"  summary:\n    problems:\n      - problem: {situation: A gap}\n        change: {what: No more gap}\n" +
				"  resources:\n    - {role: teamMember, resource: r1}\n",
		},
	})
}

// A file used to be one manifest, and yaml.Unmarshal quietly kept only the
// first document, so a file written the way every Kubernetes example is
// written lost all but one of its manifests and said nothing.
func TestImportReadsEveryDocumentInAFile(t *testing.T) {
	e := newTestEngine(t)
	ctx := context.Background()
	dir := t.TempDir()

	mustWriteFile(t, filepath.Join(dir, "everything.yaml"),
		"apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: t1\n  name: Team One\nspec: {}\n"+
			"---\n"+
			"apiVersion: cartograph/v1\nkind: Resource\nmetadata:\n  id: depot-managers\n  name: Depot managers\nspec:\n  category: externalParty\n"+
			"---\n"+
			"apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: p1\n  name: P\nspec:\n  team: t1\n"+
			"  summary:\n    problems:\n      - problem: {situation: A gap}\n        change: {what: No more gap}\n"+
			"  resources:\n    - {role: teamMember, resource: depot-managers}\n"+
			"---\n"+
			"apiVersion: cartograph/v1\nkind: StakeholderMap\nmetadata:\n  id: p1-stakeholders\n  name: P stakeholders\nspec:\n"+
			"  scope: {kind: Project, id: p1}\n  entries:\n    - {resource: depot-managers, influence: 3, interest: 2, tier: primary}\n")

	report, err := e.ImportDir(ctx, dir, "bootstrap", "one file, four manifests")
	if err != nil {
		t.Fatalf("ImportDir returned an error: %v", err)
	}
	if len(report.Problems) != 0 {
		t.Fatalf("expected a clean import, got problems: %+v", report.Problems)
	}
	if len(report.Imported) != 4 {
		t.Fatalf("expected all four manifests, got %d: %+v", len(report.Imported), report.Imported)
	}
	got := map[string]bool{}
	for _, item := range report.Imported {
		got[item.Kind] = true
	}
	for _, kind := range []string{"Team", "Resource", "Project", "StakeholderMap"} {
		if !got[kind] {
			t.Fatalf("expected %s to survive the import, got %+v", kind, report.Imported)
		}
	}
	m, err := e.Get(ctx, "StakeholderMap", "p1-stakeholders")
	if err != nil {
		t.Fatalf("the map did not survive the import: %v", err)
	}
	if !strings.Contains(string(m.YAML), "depot-managers") {
		t.Fatalf("expected the map's own document, got:\n%s", m.YAML)
	}
}

// A file holding exactly one manifest keeps the bytes it was written with,
// so committing it does not reformat somebody's hand-written YAML.
func TestSingleDocumentFileKeepsItsBytes(t *testing.T) {
	e := newTestEngine(t)
	ctx := context.Background()
	dir := t.TempDir()

	original := "apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: t1\n  name: Team One\nspec:\n  # a comment nobody should lose\n  description: Team One\n"
	mustWriteFile(t, filepath.Join(dir, "team.yaml"), original)

	if _, err := e.ImportDir(ctx, dir, "bootstrap", "single doc"); err != nil {
		t.Fatalf("ImportDir returned an error: %v", err)
	}
	v, err := e.Get(ctx, "Team", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if string(v.YAML) != original {
		t.Fatalf("expected the original bytes, got:\n%s", v.YAML)
	}
	_ = os.Remove(filepath.Join(dir, "team.yaml"))
}

// A programme holds risks too, and the dependencies between programmes are
// the graph worth drawing: a strategic plan states many linkages between
// its programmes, and they need somewhere to go. The same shape a
// project's risks have, minus the phase: a programme schedules nothing, so
// a dependency on one lands by no phase of its own.
func TestProgrammeRisks(t *testing.T) {
	e := seededEngine(t)
	base := "apiVersion: cartograph/v1\nkind: Programme\nmetadata:\n  id: prog-risk\n  name: Prog\nspec:\n" +
		"  name: Prog\n  aim: {change: Make something better}\n  leadTeam: t1\n"

	runSchemaCases(t, e, []schemaCase{
		{
			name: "a programme depending on another programme", kind: "Programme",
			yaml: base + "  risks:\n    - description: The identifiers must be stable first\n      type: dependency\n" +
				"      depends: {direction: needs, on: {kind: Team, id: t1}}\n",
		},
		{
			name: "a programme carrying an ordinary risk", kind: "Programme",
			yaml: base + "  risks:\n    - description: Capacity is not costed\n      type: issue\n      impact: high\n      likelihood: medium\n",
		},
		{
			name: "an edge on a row that is not a dependency", kind: "Programme",
			wantProblem: true, wantSubstr: "only a dependency carries an edge",
			yaml: base + "  risks:\n    - description: Something may go wrong\n      type: risk\n" +
				"      depends: {direction: needs, on: {kind: Team, id: t1}}\n",
		},
		{
			// A programme has no timeline, so a phase it cannot have is
			// refused rather than quietly ignored.
			name: "a programme dependency naming a phase", kind: "Programme",
			wantProblem: true, wantSubstr: "no timeline to name one from",
			yaml: base + "  risks:\n    - description: The identifiers must be stable first\n      type: dependency\n" +
				"      depends: {direction: needs, on: {kind: Team, id: t1}, needBy: design}\n",
		},
		{
			name: "escalation still states its reason", kind: "Programme", wantProblem: true,
			wantSubstr: "reason is required",
			yaml:       base + "  risks:\n    - description: Capacity is not costed\n      type: issue\n      escalate: {flag: true}\n",
		},
	})
}
