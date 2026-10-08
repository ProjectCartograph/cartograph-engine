// Package dbt writes a semantic layer as dbt's: one YAML file of
// semantic_models and metrics, in the shape dbt's semantic layer reads
// (dbt 1.6 and later), for an analytics engineer to put in a dbt project
// beside the models it refers to.
package dbt

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/semantic"
)

// Path is where the file goes in a dbt project.
const Path = "models/semantic/cartograph.yml"

// Exporter is the dbt exporter.
type Exporter struct{}

// New returns the dbt exporter.
func New() Exporter { return Exporter{} }

// Format is dbt.
func (Exporter) Format() string { return "dbt" }

// Export writes the layer as one dbt YAML file.
func (Exporter) Export(l semantic.Layer) ([]semantic.File, error) {
	doc := map[string]any{"version": 2}
	var models []any
	for _, m := range l.Models {
		models = append(models, model(m))
	}
	var metrics []any
	for _, m := range l.Metrics {
		metrics = append(metrics, metric(m))
	}
	if len(models) > 0 {
		doc["semantic_models"] = models
	}
	if len(metrics) > 0 {
		doc["metrics"] = metrics
	}
	var b bytes.Buffer
	b.WriteString("# Written by Cartograph from its KPIs and data sources. Edit them there and export again.\n")
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(ordered(doc, "version", "semantic_models", "metrics")); err != nil {
		return nil, fmt.Errorf("write dbt yaml: %w", err)
	}
	return []semantic.File{{Path: Path, Content: b.Bytes()}}, nil
}

func model(m semantic.Model) *yaml.Node {
	key := m.Key
	if key == "" {
		key = m.Entity
	}
	entity := ordered(map[string]any{"name": m.Entity, "type": "primary", "expr": key}, "name", "type", "expr")
	var dims []any
	if m.TimeColumn != "" {
		dims = append(dims, ordered(map[string]any{"name": m.TimeColumn, "type": "time",
			"type_params": map[string]any{"time_granularity": m.Grain}}, "name", "type", "type_params"))
	}
	for _, d := range m.Dimensions {
		dim := map[string]any{"name": d.Name, "type": "categorical"}
		if d.Column != "" && d.Column != d.Name {
			dim["expr"] = d.Column
		}
		dims = append(dims, ordered(dim, "name", "type", "expr"))
	}
	var measures []any
	for _, ms := range m.Measures {
		me := map[string]any{"name": ms.Name, "agg": ms.Agg}
		switch {
		case ms.Expr != "":
			me["expr"] = ms.Expr
		case ms.Agg == "count" || ms.Agg == "sum":
			// A count of rows: dbt sums a 1 per row.
			me["agg"], me["expr"] = "sum", "1"
		}
		measures = append(measures, ordered(me, "name", "agg", "expr"))
	}
	out := map[string]any{"name": m.Name, "model": fmt.Sprintf("ref('%s')", m.Ref), "entities": []any{entity}}
	if m.Description != "" {
		out["description"] = m.Description
	}
	if m.TimeColumn != "" {
		out["defaults"] = map[string]any{"agg_time_dimension": m.TimeColumn}
	}
	if len(dims) > 0 {
		out["dimensions"] = dims
	}
	if len(measures) > 0 {
		out["measures"] = measures
	}
	return ordered(out, "name", "description", "model", "defaults", "entities", "dimensions", "measures")
}

func metric(m semantic.Metric) *yaml.Node {
	params := map[string]any{}
	switch m.Type {
	case semantic.Simple:
		params["measure"] = m.Measure
	case semantic.Cumulative:
		params["measure"] = m.Measure
		if m.Window != "" {
			params["window"] = m.Window
		}
	case semantic.Ratio:
		params["numerator"], params["denominator"] = m.Numerator, m.Denominator
	case semantic.Derived:
		params["expr"] = m.Expr
		var uses []any
		for _, u := range m.Uses {
			uses = append(uses, map[string]any{"name": u})
		}
		params["metrics"] = uses
	}
	out := map[string]any{"name": m.Name, "label": m.Label, "type": m.Type,
		"type_params": ordered(params, "measure", "numerator", "denominator", "window", "expr", "metrics")}
	if m.Description != "" {
		out["description"] = strings.TrimSpace(m.Description)
	}
	if m.Filter != "" {
		out["filter"] = m.Filter
	}
	return ordered(out, "name", "label", "description", "type", "type_params", "filter")
}

// ordered is a mapping with its keys in the order given, then any others,
// so the file reads as dbt's own examples do and never changes order.
func ordered(m map[string]any, keys ...string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.MappingNode}
	seen := map[string]bool{}
	put := func(k string) {
		v, ok := m[k]
		if !ok || seen[k] {
			return
		}
		seen[k] = true
		var val yaml.Node
		if node, isNode := v.(*yaml.Node); isNode {
			val = *node
		} else if err := val.Encode(v); err != nil {
			return
		}
		n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: k}, &val)
	}
	for _, k := range keys {
		put(k)
	}
	var rest []string
	for k := range m {
		if !seen[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	for _, k := range rest {
		put(k)
	}
	return n
}
