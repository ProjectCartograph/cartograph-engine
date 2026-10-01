package sqlite

import (
	"context"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

// Exclude records an exclusion for a manifest.
func (m *ManifestStore) Exclude(ctx context.Context, kind, id, name, reason, operator string) error {
	query := `INSERT INTO exclusions (kind, id, name, "on", reason, operator)
	          VALUES (?, ?, ?, ?, ?, ?)
	          ON CONFLICT(kind, id) DO UPDATE SET name = excluded.name, "on" = excluded."on", reason = excluded.reason, operator = excluded.operator`
	_, err := m.db.ExecContext(ctx, query, kind, id, name, time.Now().Format(time.RFC3339Nano), reason, operator)
	return err
}

// ListExcluded returns every excluded manifest, newest first.
func (m *ManifestStore) ListExcluded(ctx context.Context) ([]store.Exclusion, error) {
	query := `SELECT kind, id, name, "on", reason, operator FROM exclusions
	          ORDER BY "on" DESC`
	rows, err := m.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var exclusions []store.Exclusion
	for rows.Next() {
		var excl store.Exclusion
		var onStr string

		err := rows.Scan(&excl.Kind, &excl.ID, &excl.Name, &onStr, &excl.Reason, &excl.Operator)
		if err != nil {
			return nil, err
		}

		excl.On, err = time.Parse(time.RFC3339Nano, onStr)
		if err != nil {
			return nil, err
		}

		exclusions = append(exclusions, excl)
	}
	return exclusions, rows.Err()
}

// Recover deletes an exclusion record.
func (m *ManifestStore) Recover(ctx context.Context, kind, id, reason, operator string) error {
	query := `DELETE FROM exclusions WHERE kind = ? AND id = ?`
	_, err := m.db.ExecContext(ctx, query, kind, id)
	return err
}
