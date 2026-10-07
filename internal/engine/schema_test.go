package engine_test

import (
	"context"
	"strings"
	"testing"

	codecyaml "github.com/ProjectCartograph/cartograph-engine/v2/internal/codec/yaml"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store/memory"
)

// seededEngine returns an engine with one Team, DataSource,
// ReportingCycle, a goal hierarchy (pillar g1, strategic g1-s, functional
// g1-f), and a KPI already committed, so every kind's "valid" fixture below
// has something real to reference (I3.4a). Validate never writes, so
// every subtest below shares this one instance safely.
func seededEngine(t *testing.T) *engine.Engine {
	t.Helper()
	return seededEngineOver(t, memory.NewManifestStore())
}

// seededEngineOver is seededEngine over a store the test keeps, to write
// into it behind the engine's back what an older engine allowed.
func seededEngineOver(t *testing.T, ms store.ManifestStore) *engine.Engine {
	t.Helper()
	e, err := engine.New(ms, memory.NewOperationalStore(), engine.WithCodec(codecyaml.New()))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	commit := func(kind, id, actor, y string) {
		t.Helper()
		if _, err := e.Commit(ctx, kind, id, []byte(y), actor, "seed"); err != nil {
			t.Fatalf("seed %s/%s: %v", kind, id, err)
		}
	}

	commit("Team", "t1", "local", "apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: t1\n  name: Team One\nspec:\n  name: Team One\n")
	commit("DataSource", "d1", "local", "apiVersion: cartograph/v1\nkind: DataSource\nmetadata:\n  id: d1\n  name: Data Source One\nspec:\n  name: Data Source One\n  category: database\n  team: t1\n")
	commit("ReportingCycle", "c1", "local", "apiVersion: cartograph/v1\nkind: ReportingCycle\nmetadata:\n  id: c1\n  name: Cycle One\nspec:\n  name: Cycle One\n  periodMonths: 3\n  startMonth: 1\n")
	// The Resources step picks a role out of this catalogue; the seed had
	// no entry of the kind, the way the example instance had none.
	commit("Resource", "r1", "local", "apiVersion: cartograph/v1\nkind: Resource\nmetadata:\n  id: r1\n  name: Resource One\nspec:\n  name: Resource One\n  category: personRole\n")
	// Segments: a dimension and three values, so a gap can name where it
	// was observed and a measure can name what it breaks down by.
	// The units a KPI can be counted in. Seeded here the way a vault is
	// seeded on open, so every fixture below can name one.
	for _, u := range [][3]string{{"percent", "Percent", "percent"}, {"count", "Count", "count"},
		{"hours", "Hours", "duration"}} {
		commit("Unit", u[0], "local", "apiVersion: cartograph/v1\nkind: Unit\nmetadata:\n  id: "+u[0]+
			"\n  name: "+u[1]+"\nspec:\n  name: "+u[1]+"\n  dimension: "+u[2]+"\n")
	}

	commit("Segment", "seg-region", "local", "apiVersion: cartograph/v1\nkind: Segment\nmetadata:\n  id: seg-region\n  name: Region\nspec:\n  name: Region\n")
	commit("Segment", "seg-gender", "local", "apiVersion: cartograph/v1\nkind: Segment\nmetadata:\n  id: seg-gender\n  name: Gender\nspec:\n  name: Gender\n")
	commit("Segment", "seg-age", "local", "apiVersion: cartograph/v1\nkind: Segment\nmetadata:\n  id: seg-age\n  name: Age\nspec:\n  name: Age\n")
	commit("Segment", "seg-north", "local", "apiVersion: cartograph/v1\nkind: Segment\nmetadata:\n  id: seg-north\n  name: North\nspec:\n  name: North\n  parent: seg-region\n")

	commit("Goal", "g1", "local", "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: g1\n  name: Goal One\nspec:\n  level: goal\n  objective: Improve outcomes\n")
	commit("Goal", "g1-s", "local", "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: g1-s\n  name: Goal One Strategic\nspec:\n  level: objective\n  parent: g1\n  objective: Achieve strategic outcome\n")
	commit("Goal", "g1-f", "local", "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: g1-f\n  name: Goal One Functional\nspec:\n  level: outcome\n  parent: g1-s\n  objective: Achieve functional outcome\n")
	commit("KPI", "k1", "local", "apiVersion: cartograph/v1\nkind: KPI\nmetadata:\n  id: k1\n  name: KPI One\nspec:\n  name: KPI One\n  definition: A measured thing\n  unit: percent\n  direction: increase\n  source: d1\n  goals: [g1-f]\n")
	commit("BeneficiaryGroup", "bg1", "local", "apiVersion: cartograph/v1\nkind: BeneficiaryGroup\nmetadata:\n  id: bg1\n  name: Group One\nspec:\n  name: Group One\n  source: d1\n")
	// A second group, so a project can name more than one: beneficiaries
	// are a list of who the project is for, and one entry never exercises
	// that.
	commit("BeneficiaryGroup", "bg2", "local", "apiVersion: cartograph/v1\nkind: BeneficiaryGroup\nmetadata:\n  id: bg2\n  name: Group Two\nspec:\n  name: Group Two\n")

	return e
}

type schemaCase struct {
	name        string
	kind        string
	yaml        string
	wantProblem bool
	wantSubstr  string // substring expected in some problem's message or path, when wantProblem
}

func runSchemaCases(t *testing.T, e *engine.Engine, cases []schemaCase) {
	t.Helper()
	ctx := context.Background()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			problems, err := e.Validate(ctx, c.kind, []byte(c.yaml))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !c.wantProblem {
				if len(problems) != 0 {
					t.Fatalf("expected no problems, got %+v", problems)
				}
				return
			}
			if len(problems) == 0 {
				t.Fatal("expected at least one problem, got none")
			}
			if c.wantSubstr != "" {
				found := false
				for _, p := range problems {
					if strings.Contains(p.Path, c.wantSubstr) || strings.Contains(p.Message, c.wantSubstr) {
						found = true
					}
				}
				if !found {
					t.Fatalf("expected a problem mentioning %q, got %+v", c.wantSubstr, problems)
				}
			}
		})
	}
}

func TestTeamSchema(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	runSchemaCases(t, e, []schemaCase{
		{name: "valid", kind: "Team", yaml: "apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: t2\n  name: Team Two\nspec:\n  name: Team Two\n  parent: t1\n"},
		// The name is metadata.name alone (2.6.0); a team without one is refused.
		{name: "named in metadata alone", kind: "Team", yaml: "apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: t2\n  name: Team Two\nspec:\n  description: named once\n"},
		{name: "missing name", kind: "Team", wantProblem: true, yaml: "apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: t2\nspec:\n  description: no name here\n"},
		{name: "dangling parent ref", kind: "Team", wantProblem: true, wantSubstr: "does not exist", yaml: "apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: t2\n  name: Team Two\nspec:\n  name: Team Two\n  parent: does-not-exist\n"},
	})
}

func TestBeneficiaryGroupSchema(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	runSchemaCases(t, e, []schemaCase{
		{name: "valid, with source", kind: "BeneficiaryGroup", yaml: "apiVersion: cartograph/v1\nkind: BeneficiaryGroup\nmetadata:\n  id: bg2\n  name: Group Two\nspec:\n  name: Group Two\n  description: A second beneficiary group\n  source: d1\n"},
		{name: "valid, minimal", kind: "BeneficiaryGroup", yaml: "apiVersion: cartograph/v1\nkind: BeneficiaryGroup\nmetadata:\n  id: bg2\n  name: Group Two\nspec:\n  name: Group Two\n"},
		{name: "named in metadata alone", kind: "BeneficiaryGroup", yaml: "apiVersion: cartograph/v1\nkind: BeneficiaryGroup\nmetadata:\n  id: bg2\n  name: Group Two\nspec:\n  description: named once\n"},
		{name: "missing name", kind: "BeneficiaryGroup", wantProblem: true, yaml: "apiVersion: cartograph/v1\nkind: BeneficiaryGroup\nmetadata:\n  id: bg2\nspec:\n  description: no name\n"},
		{name: "name exceeds maxLength 80", kind: "BeneficiaryGroup", wantProblem: true, wantSubstr: "maxLength", yaml: "apiVersion: cartograph/v1\nkind: BeneficiaryGroup\nmetadata:\n  id: bg2\n  name: Group Two\nspec:\n  name: " + strings.Repeat("x", 81) + "\n"},
		{name: "description exceeds maxLength 240", kind: "BeneficiaryGroup", wantProblem: true, wantSubstr: "maxLength", yaml: "apiVersion: cartograph/v1\nkind: BeneficiaryGroup\nmetadata:\n  id: bg2\n  name: Group Two\nspec:\n  name: Group Two\n  description: " + strings.Repeat("x", 241) + "\n"},
		// No size on a group either: the register identifies who a group is,
		// it does not count them (DESIGN_RULES.md).
		{name: "typicalSize is refused", kind: "BeneficiaryGroup", wantProblem: true, wantSubstr: "typicalSize", yaml: "apiVersion: cartograph/v1\nkind: BeneficiaryGroup\nmetadata:\n  id: bg2\n  name: Group Two\nspec:\n  name: Group Two\n  typicalSize: 400\n"},
		{name: "dangling source ref", kind: "BeneficiaryGroup", wantProblem: true, wantSubstr: "does not exist", yaml: "apiVersion: cartograph/v1\nkind: BeneficiaryGroup\nmetadata:\n  id: bg2\n  name: Group Two\nspec:\n  name: Group Two\n  source: does-not-exist\n"},
	})
}

func TestReportingCycleSchema(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	runSchemaCases(t, e, []schemaCase{
		{name: "valid", kind: "ReportingCycle", yaml: "apiVersion: cartograph/v1\nkind: ReportingCycle\nmetadata:\n  id: c2\n  name: Cycle Two\nspec:\n  name: Cycle Two\n  periodMonths: 12\n  startMonth: 1\n"},
		{name: "missing periodMonths", kind: "ReportingCycle", wantProblem: true, yaml: "apiVersion: cartograph/v1\nkind: ReportingCycle\nmetadata:\n  id: c2\n  name: Cycle Two\nspec:\n  name: Cycle Two\n  startMonth: 1\n"},
		{name: "bad enum periodMonths", kind: "ReportingCycle", wantProblem: true, yaml: "apiVersion: cartograph/v1\nkind: ReportingCycle\nmetadata:\n  id: c2\n  name: Cycle Two\nspec:\n  name: Cycle Two\n  periodMonths: 5\n  startMonth: 1\n"},
		{name: "startMonth out of range", kind: "ReportingCycle", wantProblem: true, yaml: "apiVersion: cartograph/v1\nkind: ReportingCycle\nmetadata:\n  id: c2\n  name: Cycle Two\nspec:\n  name: Cycle Two\n  periodMonths: 12\n  startMonth: 13\n"},
	})
}

func TestDataSourceSchema(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	runSchemaCases(t, e, []schemaCase{
		{name: "valid", kind: "DataSource", yaml: "apiVersion: cartograph/v1\nkind: DataSource\nmetadata:\n  id: d2\n  name: Data Source Two\nspec:\n  name: Data Source Two\n  category: spreadsheet\n  team: t1\n"},
		{name: "missing category is allowed (provenance split)", kind: "DataSource", wantProblem: false, yaml: "apiVersion: cartograph/v1\nkind: DataSource\nmetadata:\n  id: d2\n  name: Data Source Two\nspec:\n  name: Data Source Two\n  team: t1\n"},
		{name: "bad enum category", kind: "DataSource", wantProblem: true, yaml: "apiVersion: cartograph/v1\nkind: DataSource\nmetadata:\n  id: d2\n  name: Data Source Two\nspec:\n  name: Data Source Two\n  category: filing shelf\n  team: t1\n"},
		{name: "dangling team ref", kind: "DataSource", wantProblem: true, wantSubstr: "does not exist", yaml: "apiVersion: cartograph/v1\nkind: DataSource\nmetadata:\n  id: d2\n  name: Data Source Two\nspec:\n  name: Data Source Two\n  category: spreadsheet\n  team: does-not-exist\n"},
	})
}

func TestGoalSchema(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	runSchemaCases(t, e, []schemaCase{
		{name: "valid strategic goal", kind: "Goal", yaml: "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: g2\n  name: Goal Two\nspec:\n  level: objective\n  parent: g1\n  objective: Raise attainment\n"},
		{name: "objective may be omitted", kind: "Goal", yaml: "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: g2\n  name: Goal Two\nspec:\n  level: goal\n"},
		{name: "bad enum level", kind: "Goal", wantProblem: true, yaml: "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: g2\n  name: Goal Two\nspec:\n  level: worldwide\n  objective: Raise attainment\n"},
		{name: "dangling parent ref", kind: "Goal", wantProblem: true, wantSubstr: "does not exist", yaml: "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: g2\n  name: Goal Two\nspec:\n  level: objective\n  parent: does-not-exist\n  objective: Raise attainment\n"},
		{name: "team is no longer a valid field", kind: "Goal", wantProblem: true, yaml: "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: g2\n  name: Goal Two\nspec:\n  level: objective\n  parent: g1\n  team: t1\n  objective: Raise attainment\n"},
		{name: "key result unique ids ok", kind: "Goal", yaml: "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: g2\n  name: Goal Two\nspec:\n  level: objective\n  parent: g1\n  objective: Raise attainment\n  keyResults:\n    - id: kr-1\n      metric: Screenings\n      direction: increase\n      kind: count\n      unit: deliveries\n    - id: kr-2\n      metric: Coverage\n      direction: increase\n      kind: percent\n"},
		{name: "key result duplicate ids", kind: "Goal", wantProblem: true, wantSubstr: "duplicates", yaml: "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: g2\n  name: Goal Two\nspec:\n  level: objective\n  parent: g1\n  objective: Raise attainment\n  keyResults:\n    - id: kr-1\n      metric: Screenings\n      direction: increase\n      kind: count\n      unit: deliveries\n    - id: kr-1\n      metric: Coverage\n      direction: increase\n      kind: percent\n"},
		{name: "key result source is no longer valid on a goal", kind: "Goal", wantProblem: true, yaml: "apiVersion: cartograph/v1\nkind: Goal\nmetadata:\n  id: g2\n  name: Goal Two\nspec:\n  level: objective\n  parent: g1\n  objective: Raise attainment\n  keyResults:\n    - id: kr-1\n      metric: Screenings\n      direction: increase\n      kind: count\n      unit: deliveries\n      source: d1\n"},
	})
}

func TestKPISchema(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	runSchemaCases(t, e, []schemaCase{
		{name: "valid", kind: "KPI", yaml: "apiVersion: cartograph/v1\nkind: KPI\nmetadata:\n  id: k2\n  name: KPI Two\nspec:\n  name: KPI Two\n  definition: Another measure\n  unit: count\n  direction: decrease\n  source: d1\n  goals: [g1-f]\n"},
		{name: "missing source", kind: "KPI", wantProblem: true, yaml: "apiVersion: cartograph/v1\nkind: KPI\nmetadata:\n  id: k2\n  name: KPI Two\nspec:\n  name: KPI Two\n  definition: Another measure\n  unit: count\n  direction: decrease\n"},
		{name: "bad enum direction", kind: "KPI", wantProblem: true, yaml: "apiVersion: cartograph/v1\nkind: KPI\nmetadata:\n  id: k2\n  name: KPI Two\nspec:\n  name: KPI Two\n  definition: Another measure\n  unit: count\n  direction: sideways\n  source: d1\n"},
		{name: "dangling source ref", kind: "KPI", wantProblem: true, wantSubstr: "does not exist", yaml: "apiVersion: cartograph/v1\nkind: KPI\nmetadata:\n  id: k2\n  name: KPI Two\nspec:\n  name: KPI Two\n  definition: Another measure\n  unit: count\n  direction: decrease\n  source: does-not-exist\n"},
		{name: "dangling goal ref", kind: "KPI", wantProblem: true, wantSubstr: "does not exist", yaml: "apiVersion: cartograph/v1\nkind: KPI\nmetadata:\n  id: k2\n  name: KPI Two\nspec:\n  name: KPI Two\n  definition: Another measure\n  unit: count\n  direction: decrease\n  source: d1\n  goals: [does-not-exist]\n"},
	})
}

func TestProgrammeSchema(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	runSchemaCases(t, e, []schemaCase{
		{name: "valid", kind: "Programme", yaml: "apiVersion: cartograph/v1\nkind: Programme\nmetadata:\n  id: prog1\n  name: Programme One\nspec:\n  name: Programme One\n  aim: {change: Raise attainment across teams}\n  leadTeam: t1\n  goals: [g1-f]\n  kpis: [k1]\n"},
		{name: "missing aim", kind: "Programme", wantProblem: true, yaml: "apiVersion: cartograph/v1\nkind: Programme\nmetadata:\n  id: prog1\n  name: Programme One\nspec:\n  name: Programme One\n  leadTeam: t1\n"},
		{name: "dangling leadTeam ref", kind: "Programme", wantProblem: true, wantSubstr: "does not exist", yaml: "apiVersion: cartograph/v1\nkind: Programme\nmetadata:\n  id: prog1\n  name: Programme One\nspec:\n  name: Programme One\n  aim: {change: Raise attainment}\n  leadTeam: does-not-exist\n"},
	})
}

func TestOperationSchema(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	runSchemaCases(t, e, []schemaCase{
		{name: "valid", kind: "Operation", yaml: "apiVersion: cartograph/v1\nkind: Operation\nmetadata:\n  id: op1\n  name: Operation One\nspec:\n  name: Operation One\n  purpose: Keep the lights on\n  team: t1\n  kpis: [k1]\n"},
		{name: "missing purpose", kind: "Operation", wantProblem: true, yaml: "apiVersion: cartograph/v1\nkind: Operation\nmetadata:\n  id: op1\n  name: Operation One\nspec:\n  name: Operation One\n  team: t1\n"},
		{name: "dangling team ref", kind: "Operation", wantProblem: true, wantSubstr: "does not exist", yaml: "apiVersion: cartograph/v1\nkind: Operation\nmetadata:\n  id: op1\n  name: Operation One\nspec:\n  name: Operation One\n  purpose: Keep the lights on\n  team: does-not-exist\n"},
		{name: "bad enum data handoff", kind: "Operation", wantProblem: true, yaml: "apiVersion: cartograph/v1\nkind: Operation\nmetadata:\n  id: op1\n  name: Operation One\nspec:\n  name: Operation One\n  purpose: Keep the lights on\n  team: t1\n  data:\n    consumes:\n      - source: d1\n        purpose: reconciliation\n        handoff: telepathy\n"},
	})
}

func TestProjectSchema(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	runSchemaCases(t, e, []schemaCase{
		{name: "valid", kind: "Project", yaml: "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: proj1\n  name: Project One\nspec:\n  team: t1\n  summary:\n    problems:\n      - problem: {situation: Too many gaps}\n        change: {what: Fewer gaps}\n  alignment:\n    goals: [g1-f]\n  operation: new\n  objectives:\n    - objective: Deliver every order\n      keyResults:\n        - id: kr-1\n          metric: Orders delivered\n          direction: increase\n          kind: count\n          unit: deliveries\n"},
		{name: "missing summary.problems[].change", kind: "Project", wantProblem: true, yaml: "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: proj1\n  name: Project One\nspec:\n  team: t1\n  summary:\n    problems:\n      - problem: {situation: Too many gaps}\n"},
		{name: "empty summary.problems", kind: "Project", wantProblem: true, yaml: "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: proj1\n  name: Project One\nspec:\n  team: t1\n  summary:\n    problems: []\n"},
		{name: "bad enum risk impact", kind: "Project", wantProblem: true, yaml: "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: proj1\n  name: Project One\nspec:\n  team: t1\n  summary:\n    problems:\n      - problem: {situation: Too many gaps}\n        change: {what: Fewer gaps}\n  risks:\n    - description: Vendor delay\n      impact: catastrophic\n"},
		{name: "dangling team ref", kind: "Project", wantProblem: true, wantSubstr: "does not exist", yaml: "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: proj1\n  name: Project One\nspec:\n  team: does-not-exist\n  summary:\n    problems:\n      - problem: {situation: Too many gaps}\n        change: {what: Fewer gaps}\n"},
		{name: "operation literal new is not a dangling ref", kind: "Project", yaml: "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: proj1\n  name: Project One\nspec:\n  team: t1\n  summary:\n    problems:\n      - problem: {situation: Too many gaps}\n        change: {what: Fewer gaps}\n  operation: new\n"},
		{name: "dangling operation ref", kind: "Project", wantProblem: true, wantSubstr: "does not exist", yaml: "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: proj1\n  name: Project One\nspec:\n  team: t1\n  summary:\n    problems:\n      - problem: {situation: Too many gaps}\n        change: {what: Fewer gaps}\n  operation: does-not-exist\n"},
		{name: "duplicate key result ids within one objective", kind: "Project", wantProblem: true, wantSubstr: "duplicates", yaml: "apiVersion: cartograph/v1\nkind: Project\nmetadata:\n  id: proj1\n  name: Project One\nspec:\n  team: t1\n  summary:\n    problems:\n      - problem: {situation: Too many gaps}\n        change: {what: Fewer gaps}\n  objectives:\n    - objective: Deliver every order\n      keyResults:\n        - id: kr-1\n          metric: Orders delivered\n          direction: increase\n          kind: count\n          unit: deliveries\n        - id: kr-1\n          metric: Coverage\n          direction: increase\n          kind: percent\n"},
	})
}

// A team cannot sit beneath itself: a contributor's reach walks the teams
// beneath theirs, and a loop has no bottom (TAXONOMY.md D27).
func TestATeamCannotBeItsOwnAncestor(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	mustCommit(t, e, "Team", "t2", "local", "apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: t2\n  name: Team Two\nspec:\n  parent: t1\n")
	runSchemaCases(t, e, []schemaCase{
		{name: "beneath a team beneath it", kind: "Team", wantProblem: true, wantSubstr: "its own ancestor", yaml: "apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: t1\n  name: Team One\nspec:\n  parent: t2\n"},
		{name: "its own parent", kind: "Team", wantProblem: true, wantSubstr: "its own parent", yaml: "apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: t1\n  name: Team One\nspec:\n  parent: t1\n"},
	})
}

func TestPurposeLogoSchema(t *testing.T) {
	t.Parallel()
	e := seededEngine(t)
	purpose := func(logo string) string {
		return "apiVersion: cartograph/v1\nkind: Purpose\nmetadata:\n  id: default\n  name: Purpose\nspec:\n  organisation: Northwind\n  logo: \"" + logo + "\"\n"
	}
	runSchemaCases(t, e, []schemaCase{
		{name: "png data url", kind: "Purpose", yaml: purpose("data:image/png;base64,iVBORw0KGgo=")},
		{name: "svg data url", kind: "Purpose", yaml: purpose("data:image/svg+xml;base64,PHN2Zy8+")},
		{name: "a link elsewhere", kind: "Purpose", yaml: purpose("https://example.org/logo.png"), wantProblem: true, wantSubstr: "logo"},
		{name: "not an image", kind: "Purpose", yaml: purpose("data:text/html;base64,PGI+"), wantProblem: true, wantSubstr: "logo"},
	})
}
