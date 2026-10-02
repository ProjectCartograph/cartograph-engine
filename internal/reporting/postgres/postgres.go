// Package postgres is the reporting adapter that answers reports from
// views in the deployment's own Postgres (docs/adr/0014), so SQL and BI
// tools read the same rows the API serves. It reads the tables the
// Postgres store keeps, and is chosen only beside that store.
package postgres

import (
	"context"
	_ "embed"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/reporting"
)

//go:embed views.sql
var views string

// viewsLock is the advisory lock replicas take while replacing the views.
const viewsLock = 0x636172746f727074

// Reporter answers reports from views.
type Reporter struct{ pool *pgxpool.Pool }

var _ reporting.Reporter = (*Reporter)(nil)

// Open creates or replaces the report views, one replica at a time, and
// returns a reporter over them.
func Open(ctx context.Context, pool *pgxpool.Pool) (*Reporter, error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("report views: %w", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, int64(viewsLock)); err != nil {
		return nil, fmt.Errorf("report views: %w", err)
	}
	defer conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, int64(viewsLock))
	if _, err := conn.Exec(ctx, views); err != nil {
		return nil, fmt.Errorf("report views: %w", err)
	}
	return &Reporter{pool: pool}, nil
}

// Names lists the standard reports.
func (*Reporter) Names() []string { return reporting.Standard }

// Run reads a report's view.
func (r *Reporter) Run(ctx context.Context, name string) (reporting.Table, error) {
	if !slices.Contains(reporting.Standard, name) {
		return reporting.Table{}, fmt.Errorf("%w: %s", reporting.ErrNoReport, name)
	}
	view := "report_" + strings.ReplaceAll(name, "-", "_")
	rows, err := r.pool.Query(ctx, `SELECT * FROM `+view+` ORDER BY 1, 2, 3, 4`)
	if err != nil {
		return reporting.Table{}, fmt.Errorf("report %s: %w", name, err)
	}
	defer rows.Close()
	t := reporting.Table{Name: name, Rows: [][]any{}}
	for _, f := range rows.FieldDescriptions() {
		t.Columns = append(t.Columns, f.Name)
	}
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return t, fmt.Errorf("report %s: %w", name, err)
		}
		for i, v := range vals {
			// Times as the computed reports write them.
			if tm, ok := v.(time.Time); ok {
				vals[i] = tm.UTC().Format(time.RFC3339)
			}
		}
		t.Rows = append(t.Rows, vals)
	}
	return t, rows.Err()
}
