package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

// OperationalStore is the SQLite adapter for store.OperationalStore.
type OperationalStore struct {
	db *sql.DB
}

func NewOperationalStore(db *sql.DB) *OperationalStore {
	return &OperationalStore{db: db}
}

func (o *OperationalStore) PutProjectStateTransition(ctx context.Context, e store.ProjectStateEntry) error {
	on := e.On
	if on.IsZero() {
		on = time.Now().UTC()
	}
	_, err := o.db.ExecContext(ctx, `
		INSERT INTO project_state (project_id, state, actor, reason, on_ts, snapshot, bundle)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		e.ProjectID, e.State, e.Actor, e.Reason, on.Format(timeLayout), e.Snapshot, e.Bundle)
	return err
}

func (o *OperationalStore) ListProjectStateHistory(ctx context.Context, projectID string) ([]store.ProjectStateEntry, error) {
	rows, err := o.db.QueryContext(ctx, `
		SELECT project_id, state, actor, reason, on_ts, snapshot, bundle
		FROM project_state WHERE project_id = ? ORDER BY on_ts ASC, rowid ASC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []store.ProjectStateEntry{}
	for rows.Next() {
		var e store.ProjectStateEntry
		var onText string
		if err := rows.Scan(&e.ProjectID, &e.State, &e.Actor, &e.Reason, &onText, &e.Snapshot, &e.Bundle); err != nil {
			return nil, err
		}
		if e.On, err = time.Parse(timeLayout, onText); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
