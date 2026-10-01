package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ProjectCartograph/cartograph-engine/internal/store"
)

// OperationalStore is the Postgres adapter for store.OperationalStore.
type OperationalStore struct {
	pool *pgxpool.Pool
}

var _ store.OperationalStore = (*OperationalStore)(nil)

// NewOperationalStore returns an OperationalStore over a pool Open
// returned.
func NewOperationalStore(pool *pgxpool.Pool) *OperationalStore {
	return &OperationalStore{pool: pool}
}

// PutProjectStateTransition appends one row to a project's history.
func (o *OperationalStore) PutProjectStateTransition(ctx context.Context, e store.ProjectStateEntry) error {
	_, err := o.pool.Exec(ctx, `
		INSERT INTO project_state (project_id, state, actor, reason, on_ts, snapshot, bundle)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		e.ProjectID, e.State, e.Actor, e.Reason, stamp(e.On), e.Snapshot, e.Bundle)
	if err != nil {
		return fmt.Errorf("put project state %s: %w", e.ProjectID, err)
	}
	return nil
}

// ListProjectStateHistory returns a project's history, oldest first;
// rows written at the same instant keep the order they were written in.
func (o *OperationalStore) ListProjectStateHistory(ctx context.Context, projectID string) ([]store.ProjectStateEntry, error) {
	rows, err := o.pool.Query(ctx, `
		SELECT project_id, state, actor, reason, on_ts, snapshot, bundle FROM project_state
		WHERE project_id = $1 ORDER BY on_ts, seq`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list project state %s: %w", projectID, err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByPos[store.ProjectStateEntry])
	if err != nil {
		return nil, fmt.Errorf("list project state %s: %w", projectID, err)
	}
	for i := range out {
		out[i].On = out[i].On.UTC()
	}
	if out == nil {
		out = []store.ProjectStateEntry{}
	}
	return out, nil
}
