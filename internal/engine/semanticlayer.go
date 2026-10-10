package engine

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/semantic"
)

// WithSemanticExporters sets the syntaxes the KPIs can be exported in as a
// semantic layer (TAXONOMY.md D57). Without one, an export answers
// semantic.ErrNoFormat.
func WithSemanticExporters(xs ...semantic.Exporter) Option {
	return func(e *Engine) {
		for _, x := range xs {
			if e.semantic == nil {
				e.semantic = map[string]semantic.Exporter{}
			}
			e.semantic[x.Format()] = x
		}
	}
}

// SemanticFormats lists the syntaxes the KPIs can be exported in.
func (e *Engine) SemanticFormats() []string {
	out := make([]string, 0, len(e.semantic))
	for f := range e.semantic {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// ExportSemantic writes the semantic layer in a syntax, with what the
// record leaves to defaults or out.
func (e *Engine) ExportSemantic(ctx context.Context, format string) ([]semantic.File, []string, error) {
	x, ok := e.semantic[format]
	if !ok {
		return nil, nil, fmt.Errorf("%w: %s", semantic.ErrNoFormat, format)
	}
	l, notes, err := e.SemanticLayer(ctx)
	if err != nil {
		return nil, nil, err
	}
	files, err := x.Export(l)
	return files, notes, err
}

// dbtName is a record's id as a warehouse name: hyphens as underscores.
func dbtName(id string) string { return strings.ReplaceAll(id, "-", "_") }

// SemanticLayer is the record as a semantic layer: each data source a KPI
// measures from as a semantic model, each KPI with a metric as a metric,
// in the semantic layer's own terms, read as ctx reads the workspace. The
// notes say what the record leaves to defaults or out.
func (e *Engine) SemanticLayer(ctx context.Context) (semantic.Layer, []string, error) {
	sources, err := e.allInPlay(ctx, "DataSource")
	if err != nil {
		return semantic.Layer{}, nil, err
	}
	kpis, err := e.allInPlay(ctx, "KPI")
	if err != nil {
		return semantic.Layer{}, nil, err
	}
	var l semantic.Layer
	var notes []string
	docs := map[string]map[string]any{}
	for _, d := range sources {
		docs[d.id] = d.doc
	}
	models := map[string]int{}
	modelOf := func(source string) (int, bool) {
		if i, ok := models[source]; ok {
			return i, true
		}
		doc, ok := docs[source]
		if !ok {
			return 0, false
		}
		m := semantic.Model{Name: dbtName(source), Source: source, Ref: dbtName(source), Entity: dbtName(source)}
		sm, _ := specOf(doc)["semanticModel"].(map[string]any)
		if sm == nil {
			notes = append(notes, fmt.Sprintf("DataSource/%s has no semantic model: written on ref('%s'), with no time dimension, for the engineer to set.", source, m.Ref))
		} else {
			m.Ref, _ = sm["model"].(string)
			m.Entity, _ = sm["entity"].(string)
			m.Key, _ = sm["key"].(string)
			if t, ok := sm["time"].(map[string]any); ok {
				m.TimeColumn, _ = t["column"].(string)
				m.Grain, _ = t["grain"].(string)
			}
			for _, dim := range listOf(sm["dimensions"]) {
				name, _ := dim["name"].(string)
				col, _ := dim["column"].(string)
				m.Dimensions = append(m.Dimensions, semantic.Dimension{Name: name, Column: col})
			}
		}
		m.Description = nameOf(doc, source)
		models[source] = len(l.Models)
		l.Models = append(l.Models, m)
		return models[source], true
	}
	measure := func(kpi, name string, v any) (string, bool) {
		ms, _ := v.(map[string]any)
		source, _ := ms["source"].(string)
		i, ok := modelOf(source)
		if !ok {
			notes = append(notes, fmt.Sprintf("KPI/%s: its measure is over %q, which is not a data source; the metric is left out.", kpi, source))
			return "", false
		}
		agg, _ := ms["agg"].(string)
		expr, _ := ms["expr"].(string)
		l.Models[i].Measures = append(l.Models[i].Measures, semantic.Measure{Name: name, Agg: agg, Expr: expr})
		return name, true
	}
	sort.Slice(kpis, func(i, j int) bool { return kpis[i].id < kpis[j].id })
	for _, d := range kpis {
		spec := specOf(d.doc)
		mt, _ := spec["metric"].(map[string]any)
		if mt == nil {
			continue
		}
		def, _ := spec["definition"].(string)
		filter, _ := mt["filter"].(string)
		name := dbtName(d.id)
		m := semantic.Metric{Name: name, KPI: d.id, Label: nameOf(d.doc, d.id), Description: withFormula(def, mt), Filter: filter}
		m.Type, _ = mt["type"].(string)
		ok := true
		switch m.Type {
		case semantic.Simple, semantic.Cumulative:
			m.Measure, ok = measure(d.id, name, mt["measure"])
			m.Window, _ = mt["window"].(string)
		case semantic.Ratio:
			// dbt divides one metric by another: each side is a measure
			// with a simple metric of its own.
			for _, side := range []string{"numerator", "denominator"} {
				ms, sok := measure(d.id, name+"_"+side, mt[side])
				if !sok {
					ok = false
					continue
				}
				l.Metrics = append(l.Metrics, semantic.Metric{Name: ms, KPI: d.id, Label: m.Label + " (" + side + ")", Type: semantic.Simple, Measure: ms})
				if side == "numerator" {
					m.Numerator = ms
				} else {
					m.Denominator = ms
				}
			}
		case semantic.Derived:
			m.Expr, _ = mt["expr"].(string)
			for _, u := range stringsOf(mt["uses"]) {
				m.Uses = append(m.Uses, dbtName(u))
			}
		}
		if ok {
			l.Metrics = append(l.Metrics, m)
		}
	}
	return l, notes, nil
}

// whereWords are the conditions' comparisons, as the description reads them.
var whereWords = map[string]string{"is": "is", "isNot": "is not", "above": "is above", "below": "is below", "atLeast": "is at least", "atMost": "is at most", "oneOf": "is one of"}

// withFormula adds to a metric's description what its definer said it
// counts and which rows count, in their words (TAXONOMY.md D63): what an
// analyst maps to columns and a filter, carried into the export.
func withFormula(def string, mt map[string]any) string {
	var parts []string
	for _, side := range []string{"measure", "numerator", "denominator"} {
		if m, ok := mt[side].(map[string]any); ok {
			if c, _ := m["counts"].(string); c != "" {
				label := map[string]string{"measure": "Counts", "numerator": "Counts", "denominator": "Out of"}[side]
				parts = append(parts, label+": "+c+".")
			}
		}
	}
	var conds []string
	for _, w := range anyList(mt["where"]) {
		wm, _ := w.(map[string]any)
		in, _ := wm["input"].(string)
		op, _ := wm["op"].(string)
		v, _ := wm["value"].(string)
		if in != "" && v != "" {
			conds = append(conds, in+" "+whereWords[op]+" "+v)
		}
	}
	if len(conds) > 0 {
		parts = append(parts, "Only where "+strings.Join(conds, "; ")+".")
	}
	if len(parts) == 0 {
		return def
	}
	return strings.TrimSpace(def + " " + strings.Join(parts, " "))
}
