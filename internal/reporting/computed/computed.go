// Package computed is the reporting adapter that works out each report
// when asked, from the engine's reads: no storage of its own, the same on
// every store (docs/adr/0014). Each report costs a fixed number of
// reads, whatever the size of the record.
package computed

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/reporting"
)

// Reporter computes reports from an engine.
type Reporter struct{ E *engine.Engine }

var _ reporting.Reporter = Reporter{}

// Names lists the standard reports.
func (Reporter) Names() []string { return reporting.Standard }

// Run computes a report by name.
func (r Reporter) Run(ctx context.Context, name string) (reporting.Table, error) {
	switch name {
	case "projects":
		return r.projects(ctx)
	case "kpi-readings":
		return r.readings(ctx)
	case "alignment":
		return r.alignment(ctx)
	case "teams":
		return r.teams(ctx)
	}
	return reporting.Table{}, fmt.Errorf("%w: %s", reporting.ErrNoReport, name)
}

// doc decodes a version: its name and spec.
func (r Reporter) doc(v engine.Version) (string, map[string]any) {
	doc, err := r.E.Codec().Decode(v.YAML)
	if err != nil {
		return "", map[string]any{}
	}
	meta, _ := doc["metadata"].(map[string]any)
	name, _ := meta["name"].(string)
	spec, _ := doc["spec"].(map[string]any)
	if spec == nil {
		spec = map[string]any{}
	}
	return name, spec
}

// team is one team: its name and the team above it.
type team struct{ name, parent string }

func (r Reporter) teamTree(ctx context.Context) (map[string]team, error) {
	vs, err := r.E.CurrentOfKind(ctx, "Team")
	if err != nil {
		return nil, err
	}
	out := make(map[string]team, len(vs))
	for _, v := range vs {
		name, spec := r.doc(v)
		if n, _ := spec["name"].(string); name == "" && n != "" {
			// Saved before 2.6.0, named in spec.name only.
			name = n
		}
		parent, _ := spec["parent"].(string)
		out[v.ID] = team{name: name, parent: parent}
	}
	return out, nil
}

// above is every team above id, nearest first.
func above(tree map[string]team, id string) []string {
	var out []string
	seen := map[string]bool{id: true}
	for p := tree[id].parent; p != "" && !seen[p] && len(out) < 64; p = tree[p].parent {
		out = append(out, p)
		seen[p] = true
	}
	return out
}

func (r Reporter) projects(ctx context.Context) (reporting.Table, error) {
	t := reporting.Table{Name: "projects", Columns: []string{"id", "name", "team", "team_name", "state", "version", "updated_on", "goals"}, Rows: [][]any{}}
	vs, err := r.E.CurrentOfKind(ctx, "Project")
	if err != nil {
		return t, err
	}
	ids := make([]string, len(vs))
	for i, v := range vs {
		ids[i] = v.ID
	}
	states, err := r.E.ProjectStates(ctx, ids)
	if err != nil {
		return t, err
	}
	tree, err := r.teamTree(ctx)
	if err != nil {
		return t, err
	}
	byGoal, err := r.E.ReferencingKind(ctx, "Goal")
	if err != nil {
		return t, err
	}
	goals := map[string][]string{}
	for goal, refs := range byGoal {
		for _, s := range refs {
			if s.Kind == "Project" {
				goals[s.ID] = append(goals[s.ID], goal)
			}
		}
	}
	for _, v := range vs {
		name, spec := r.doc(v)
		teamID, _ := spec["team"].(string)
		g := goals[v.ID]
		sort.Strings(g)
		t.Rows = append(t.Rows, []any{v.ID, name, teamID, tree[teamID].name, states[v.ID], v.Number, v.On.UTC().Format(time.RFC3339), strings.Join(g, " ")})
	}
	return t, nil
}

func (r Reporter) readings(ctx context.Context) (reporting.Table, error) {
	t := reporting.Table{Name: "kpi-readings", Columns: []string{"kpi", "kpi_name", "readings", "period", "value", "provisional", "note", "recorded_at", "recorded_by"}, Rows: [][]any{}}
	series, err := r.E.CurrentOfKind(ctx, "KPIReadings")
	if err != nil {
		return t, err
	}
	kpiOf := map[string]string{}
	for _, v := range series {
		_, spec := r.doc(v)
		kpiOf[v.ID], _ = spec["kpi"].(string)
	}
	kpis, err := r.E.List(ctx, "KPI", engine.Filter{}, false)
	if err != nil {
		return t, err
	}
	kpiName := map[string]string{}
	for _, s := range kpis {
		kpiName[s.ID] = s.Name
	}
	items, err := r.E.SeriesAsOf(ctx, "KPIReadings", "readings", nil, time.Time{})
	if err != nil {
		return t, err
	}
	ids := make([]string, 0, len(items))
	for id := range items {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		for _, it := range items[id] {
			doc, err := r.E.Codec().Decode(it.Item)
			if err != nil {
				continue
			}
			provisional, _ := doc["provisional"].(bool)
			note, _ := doc["note"].(string)
			t.Rows = append(t.Rows, []any{kpiOf[id], kpiName[kpiOf[id]], id, it.Key, doc["value"], provisional, note,
				it.RecordedAt.UTC().Format(time.RFC3339), it.RecordedBy})
		}
	}
	return t, nil
}

func (r Reporter) alignment(ctx context.Context) (reporting.Table, error) {
	t := reporting.Table{Name: "alignment", Columns: []string{"goal", "goal_name", "kind", "id", "name"}, Rows: [][]any{}}
	goals, err := r.E.List(ctx, "Goal", engine.Filter{}, false)
	if err != nil {
		return t, err
	}
	byGoal, err := r.E.ReferencingKind(ctx, "Goal")
	if err != nil {
		return t, err
	}
	for _, g := range goals {
		for _, s := range byGoal[g.ID] {
			t.Rows = append(t.Rows, []any{g.ID, g.Name, s.Kind, s.ID, s.Name})
		}
	}
	return t, nil
}

func (r Reporter) teams(ctx context.Context) (reporting.Table, error) {
	t := reporting.Table{Name: "teams", Columns: []string{"team", "name", "parent", "above"}, Rows: [][]any{}}
	tree, err := r.teamTree(ctx)
	if err != nil {
		return t, err
	}
	ids := make([]string, 0, len(tree))
	for id := range tree {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		t.Rows = append(t.Rows, []any{id, tree[id].name, tree[id].parent, strings.Join(above(tree, id), " ")})
	}
	return t, nil
}
