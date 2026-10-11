package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/aggregate"
)

// A data source gives metrics, each an aggregation over its entries
// written as pseudocode SQL (TAXONOMY.md D64); an indicator's metric is
// one of them, or one out of another.

// sourceMetric is one metric a data source gives.
type sourceMetric struct {
	ID, Name, Description, Aggregation string
}

func sourceMetricsOf(doc map[string]any) []sourceMetric {
	var out []sourceMetric
	for _, m := range listOf(specOf(doc)["metrics"]) {
		sm := sourceMetric{}
		sm.ID, _ = m["id"].(string)
		sm.Name, _ = m["name"].(string)
		sm.Description, _ = m["description"].(string)
		sm.Aggregation, _ = m["aggregation"].(string)
		out = append(out, sm)
	}
	return out
}

// AggregationReading is what an aggregation reads as: in words, the
// fields it names, and the measure a semantic layer builds from it; or
// why it does not read as one, and where.
type AggregationReading struct {
	Reading string   `json:"reading,omitempty"`
	Names   []string `json:"names"`
	Agg     string   `json:"agg,omitempty"`
	Expr    string   `json:"expr,omitempty"`
	Problem string   `json:"problem,omitempty"`
	At      int      `json:"at,omitempty"`
}

// ReadAggregation reads a metric's aggregation, as a data source's
// editor shows it while it is written: what it reads, or why it does not.
func ReadAggregation(text string) AggregationReading {
	a, err := aggregate.Parse(text)
	if err != nil {
		var p aggregate.Problem
		if errors.As(err, &p) {
			return AggregationReading{Names: []string{}, Problem: p.Message, At: p.At}
		}
		return AggregationReading{Names: []string{}, Problem: err.Error()}
	}
	agg, expr := a.Measure()
	return AggregationReading{Reading: capital(a.Reading()) + ".", Names: a.Names(), Agg: agg, Expr: expr}
}

func capital(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// dataSourceChecksOf checks each metric a data source gives reads as an
// aggregation, saying what it reads when it does and why not when it
// does not, on the metric's own field.
func (e *Engine) dataSourceChecksOf(_ context.Context, _ string, doc map[string]any) ([]ProgrammeCheck, error) {
	var out []ProgrammeCheck
	for _, m := range sourceMetricsOf(doc) {
		label := m.Name
		if label == "" {
			label = m.ID
		}
		r := ReadAggregation(m.Aggregation)
		if r.Problem != "" {
			out = append(out, ProgrammeCheck{ID: "source-metric-" + m.ID, Section: "metrics", State: programmeCheckWarn,
				Message: fmt.Sprintf("%q does not read as an aggregation yet: %s", label, r.Problem)})
			continue
		}
		out = append(out, ProgrammeCheck{ID: "source-metric-" + m.ID, Section: "metrics", State: programmeCheckOK,
			Message: fmt.Sprintf("%q reads as %s", label, lowerFirst(r.Reading))})
	}
	return out, nil
}

// resolvedMetric is a data source metric an indicator names, found.
type resolvedMetric struct {
	Source, SourceName string
	Metric             sourceMetric
	Aggregation        aggregate.Aggregation
}

// resolveSourceMetric finds the data source metric a reference names.
// problem says why it cannot be used: it names nothing, a source that is
// not one of the indicator's own or does not exist, a metric the source
// does not give, or one that does not read as an aggregation.
func (e *Engine) resolveSourceMetric(ctx context.Context, ref any, own map[string]bool) (resolvedMetric, string, error) {
	rm, _ := ref.(map[string]any)
	source, _ := rm["dataSource"].(string)
	id, _ := rm["metric"].(string)
	if source == "" || id == "" {
		return resolvedMetric{}, "it names no data source metric", nil
	}
	if !own[source] {
		return resolvedMetric{}, fmt.Sprintf("its data source %q is not one of this indicator's sources: add it to the sources, or use a metric of one of them", source), nil
	}
	doc, found, err := e.currentDoc(ctx, "DataSource", source)
	if err != nil {
		return resolvedMetric{}, "", err
	}
	if !found {
		return resolvedMetric{}, fmt.Sprintf("the data source %q does not exist", source), nil
	}
	name := nameOf(doc, source)
	for _, m := range sourceMetricsOf(doc) {
		if m.ID != id {
			continue
		}
		a, err := aggregate.Parse(m.Aggregation)
		if err != nil {
			return resolvedMetric{}, fmt.Sprintf("%q of %s does not read as an aggregation yet", m.Name, name), nil
		}
		return resolvedMetric{Source: source, SourceName: name, Metric: m, Aggregation: a}, "", nil
	}
	return resolvedMetric{}, fmt.Sprintf("%s gives no metric %q: add it on the data source, or choose one it gives", name, id), nil
}

// sourceMetricCheck checks an indicator's metric written as data source
// metrics (TAXONOMY.md D64), saying what it is computed as, or why it
// cannot be.
func (e *Engine) sourceMetricCheck(ctx context.Context, mt map[string]any, sources []string) (ok bool, message string, err error) {
	own := map[string]bool{}
	for _, s := range sources {
		own[s] = true
	}
	t, _ := mt["type"].(string)
	of, problem, err := e.resolveSourceMetric(ctx, mt["of"], own)
	if err != nil || problem != "" {
		return false, "Its number: " + problem + ".", err
	}
	says := fmt.Sprintf("%q of %s", of.Metric.Name, of.SourceName)
	if t != "ratio" {
		if w, _ := mt["window"].(string); t == "cumulative" && w != "" {
			says += ", summed over " + w
		}
		return true, "Computed as " + says + ": " + of.Aggregation.Reading() + ".", nil
	}
	out, problem, err := e.resolveSourceMetric(ctx, mt["outOf"], own)
	if err != nil || problem != "" {
		if problem == "it names no data source metric" {
			problem = "a share needs what it is out of"
		}
		return false, "What it is out of: " + problem + ".", err
	}
	return true, fmt.Sprintf("Computed as %s out of %q of %s.", says, out.Metric.Name, out.SourceName), nil
}
