// Package semantic is the port through which the KPIs Cartograph records
// leave for, and later come back from, a semantic layer: the warehouse's
// own definitions of what each number is (TAXONOMY.md D57, docs/adr/0026).
// The engine builds a Layer from the record in the semantic layer's own
// terms (semantic models over warehouse models, measures, metrics) and an
// adapter writes it in one tool's syntax. The dbt adapter beside this
// package is the one implementation today; a distribution may add another.
package semantic

import "errors"

// Layer is a whole semantic layer: the models the data sources are and
// the metrics the KPIs are.
type Layer struct {
	Models  []Model
	Metrics []Metric
}

// Model is a semantic model: a data source as it sits in the warehouse.
type Model struct {
	// Name is the semantic model's name; Source the data source it is.
	Name, Source string
	Description  string
	// Ref is the warehouse model it is built on.
	Ref string
	// Entity is what one row is, Key the column that names a row.
	Entity, Key string
	// TimeColumn is the time each row is counted at, Grain its finest.
	TimeColumn, Grain string
	Dimensions        []Dimension
	Measures          []Measure
}

// Dimension is a categorical column a metric may be split by.
type Dimension struct {
	Name, Column string
}

// Measure is an aggregation over a model's rows.
type Measure struct {
	Name, Agg, Expr string
}

// Metric types, as the semantic layer names them.
const (
	Simple     = "simple"
	Ratio      = "ratio"
	Cumulative = "cumulative"
	Derived    = "derived"
)

// Metric is one number, as the semantic layer defines it.
type Metric struct {
	// Name is the metric's name; KPI the KPI it is.
	Name, KPI          string
	Label, Description string
	Type               string
	// Measure is the measure of a simple or cumulative metric, by name.
	Measure string
	// Numerator and Denominator are, for a ratio, metrics by name.
	Numerator, Denominator string
	// Window is how far back a cumulative metric sums; empty for all time.
	Window string
	// Expr and Uses are a derived metric's expression and the metrics,
	// by name, it uses.
	Expr   string
	Uses   []string
	Filter string
}

// File is one file an exporter writes.
type File struct {
	Path    string
	Content []byte
}

// Exporter writes a layer in one tool's syntax. The same layer always
// gives the same files.
type Exporter interface {
	// Format names the syntax, such as dbt.
	Format() string
	Export(l Layer) ([]File, error)
}

// Importer reads a layer back from one tool's files, so metrics defined
// in the warehouse can be recorded as KPIs. No adapter implements it yet.
type Importer interface {
	Format() string
	Import(files []File) (Layer, error)
}

// ErrNoFormat is an export asked for in a syntax no exporter writes.
var ErrNoFormat = errors.New("no semantic layer exporter for that format")
