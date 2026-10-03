// Package kinds is the registry of every manifest kind: its schema file and
// its rules function, if it has one beyond schema and generic reference
// validation. Adding a kind means adding one entry here and one schema
// file; the engine itself never changes.
package kinds

import (
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/kinds/goal"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/kinds/kit"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/kinds/kpireadings"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/kinds/programme"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/kinds/project"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/kinds/purpose"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/kinds/segment"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/kinds/settings"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/kinds/stakeholdermap"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/kinds/team"
)

// Spec describes one kind.
type Spec struct {
	// Name is the kind as it appears in a manifest's `kind` field, for
	// example "Project".
	Name string
	// SchemaFile is the file name under contract/schemas.
	SchemaFile string
	// Rules validates the kind's document beyond its JSON Schema and
	// generic reference checks. Nil means there are none.
	Rules kit.RulesFunc
}

// All is every registered kind, in the fixed order used for listing.
var All = []Spec{
	{Name: "Team", SchemaFile: "team.schema.json", Rules: team.Rules},
	{Name: "ReportingCycle", SchemaFile: "reportingcycle.schema.json"},
	{Name: "DataSource", SchemaFile: "datasource.schema.json"},
	{Name: "BeneficiaryGroup", SchemaFile: "beneficiarygroup.schema.json"},
	{Name: "Resource", SchemaFile: "resource.schema.json"},
	{Name: "FundingSource", SchemaFile: "fundingsource.schema.json"},
	{Name: "Segment", SchemaFile: "segment.schema.json", Rules: segment.Rules},
	{Name: "Gap", SchemaFile: "gap.schema.json"},
	{Name: "Assumption", SchemaFile: "assumption.schema.json"},
	{Name: "Purpose", SchemaFile: "purpose.schema.json", Rules: purpose.Rules},
	{Name: "Goal", SchemaFile: "goal.schema.json", Rules: goal.Rules},
	{Name: "Unit", SchemaFile: "unit.schema.json"},
	{Name: "KPI", SchemaFile: "kpi.schema.json"},
	{Name: "KPIReadings", SchemaFile: "kpireadings.schema.json", Rules: kpireadings.Rules},
	{Name: "Programme", SchemaFile: "programme.schema.json", Rules: programme.Rules},
	{Name: "Operation", SchemaFile: "operation.schema.json"},
	{Name: "Project", SchemaFile: "project.schema.json", Rules: project.Rules},
	{Name: "StakeholderMap", SchemaFile: "stakeholdermap.schema.json", Rules: stakeholdermap.Rules},
	{Name: "Settings", SchemaFile: "settings.schema.json", Rules: settings.Rules},
}

// ByName finds a kind by its exact name, or reports found=false.
func ByName(name string) (Spec, bool) {
	for _, s := range All {
		if s.Name == name {
			return s, true
		}
	}
	return Spec{}, false
}

// Names returns every registered kind name, in registry order.
func Names() []string {
	names := make([]string, len(All))
	for i, s := range All {
		names[i] = s.Name
	}
	return names
}
