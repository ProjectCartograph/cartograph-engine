// Package reporting is the port for reports: tables over the record for
// people and BI tools. It is not the core. The engine knows nothing of
// it, and a deployment chooses an adapter, or none
// (docs/adr/0014): computed (from the engine's reads, on any store),
// postgres (views in the deployment's own database), or a distribution's
// own, such as a columnar warehouse fed from the event log.
package reporting

import (
	"context"
	"errors"
)

// Table is a report: named columns and rows of values in their order.
type Table struct {
	Name    string
	Columns []string
	Rows    [][]any
}

// Reporter answers reports by name.
type Reporter interface {
	// Names lists the reports it answers.
	Names() []string
	// Run computes one. An unknown name is ErrNoReport.
	Run(ctx context.Context, name string) (Table, error)
}

// ErrNoReport is a report the reporter does not answer.
var ErrNoReport = errors.New("no such report")

// Standard names the reports every built-in reporter answers, with the
// same columns: the contract's list.
var Standard = []string{"projects", "kpi-readings", "alignment", "teams"}
