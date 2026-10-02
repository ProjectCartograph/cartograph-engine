package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

var (
	_ store.SetReader       = (*ManifestStore)(nil)
	_ store.VersionCounter  = (*ManifestStore)(nil)
	_ store.DocWorkingStore = (*ManifestStore)(nil)
	_ store.DocRepairer     = (*ManifestStore)(nil)
	_ store.StateSetReader  = (*OperationalStore)(nil)
)

// CurrentOfKind returns what GetCurrent returns for every manifest of a
// kind, in one query over the kind's rows.
func (m *ManifestStore) CurrentOfKind(ctx context.Context, kind string) ([]store.Version, error) {
	rows, err := m.q.Query(ctx, currentSQL+` WHERE m.kind = $1 ORDER BY m.id`, kind)
	if err != nil {
		return nil, fmt.Errorf("current of kind %s: %w", kind, err)
	}
	return collectVersions(rows)
}

// ReferencingKind returns who references each manifest of toKind, in one
// query over the references to that kind.
func (m *ManifestStore) ReferencingKind(ctx context.Context, toKind string) (map[string][]store.Summary, error) {
	rows, err := m.q.Query(ctx, `
		SELECT r.to_id, s.* FROM (SELECT DISTINCT to_id, from_kind, from_id FROM manifest_references WHERE to_kind = $1) r
		JOIN LATERAL (`+summarySQL+` WHERE m.kind = r.from_kind AND m.id = r.from_id) s ON true
		ORDER BY r.to_id, s.kind, s.id`, toKind)
	if err != nil {
		return nil, fmt.Errorf("referencing kind %s: %w", toKind, err)
	}
	defer rows.Close()
	out := map[string][]store.Summary{}
	for rows.Next() {
		var to string
		s, err := scanSummary(rows, &to)
		if err != nil {
			return nil, err
		}
		out[to] = append(out[to], s)
	}
	return out, rows.Err()
}

// CurrentMany returns the current version of each of ids that exists, in
// one query.
func (m *ManifestStore) CurrentMany(ctx context.Context, kind string, ids []string) ([]store.Version, error) {
	rows, err := m.q.Query(ctx, currentSQL+` WHERE m.kind = $1 AND m.id = ANY($2) ORDER BY m.id`, kind, ids)
	if err != nil {
		return nil, fmt.Errorf("current of %d %s: %w", len(ids), kind, err)
	}
	return collectVersions(rows)
}

// CurrentStates returns the latest state of each project with history,
// in one query that project_state_project answers.
func (o *OperationalStore) CurrentStates(ctx context.Context, projectIDs []string) (map[string]string, error) {
	rows, err := o.pool.Query(ctx, `
		SELECT DISTINCT ON (project_id) project_id, state FROM project_state
		WHERE project_id = ANY($1) ORDER BY project_id, on_ts DESC, seq DESC`, projectIDs)
	if err != nil {
		return nil, fmt.Errorf("current states: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, state string
		if err := rows.Scan(&id, &state); err != nil {
			return nil, fmt.Errorf("current states: %w", err)
		}
		out[id] = state
	}
	return out, rows.Err()
}

// LatestNumber reads the manifest's row: one index lookup, whatever the
// length of its history.
func (m *ManifestStore) LatestNumber(ctx context.Context, kind, id string) (int, error) {
	var n int
	err := m.q.QueryRow(ctx, `SELECT version FROM manifests WHERE kind = $1 AND id = $2`, kind, id).Scan(&n)
	if err == pgx.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("latest version %s/%s: %w", kind, id, err)
	}
	return n, nil
}

// StaleDocs returns versions and working copies stored without a
// document: by a replica of the previous release, or as text no writer
// could read. The partial indexes keep this cheap once there are none.
func (m *ManifestStore) StaleDocs(ctx context.Context, limit int) ([]store.Version, error) {
	rows, err := m.q.Query(ctx, `
		(SELECT kind, id, number, yaml FROM manifest_versions WHERE doc IS NULL AND patch IS NULL LIMIT $1)
		UNION ALL
		(SELECT kind, id, 0, yaml FROM working_copies WHERE doc IS NULL LIMIT $1)`, limit)
	if err != nil {
		return nil, fmt.Errorf("stale documents: %w", err)
	}
	defer rows.Close()
	out := []store.Version{}
	for rows.Next() {
		var v store.Version
		var text string
		if err := rows.Scan(&v.Kind, &v.ID, &v.Number, &text); err != nil {
			return nil, fmt.Errorf("stale documents: %w", err)
		}
		v.YAML = []byte(text)
		out = append(out, v)
	}
	return out, rows.Err()
}

// PutDocs gives each version or working copy its document, in one round
// trip. The triggers carry each onto the manifest's row.
func (m *ManifestStore) PutDocs(ctx context.Context, docs []store.Version) error {
	return m.inTx(ctx, func(q querier) error {
		b := &pgx.Batch{}
		for _, v := range docs {
			if v.Number == 0 {
				b.Queue(`UPDATE working_copies SET doc = $3 WHERE kind = $1 AND id = $2 AND doc IS NULL`, v.Kind, v.ID, string(v.Doc))
				continue
			}
			b.Queue(`UPDATE manifest_versions SET doc = $4 WHERE kind = $1 AND id = $2 AND number = $3 AND doc IS NULL`, v.Kind, v.ID, v.Number, string(v.Doc))
		}
		if err := q.SendBatch(ctx, b).Close(); err != nil {
			return fmt.Errorf("put documents: %w", err)
		}
		return nil
	})
}
