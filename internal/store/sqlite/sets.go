package sqlite

import (
	"context"
	"strings"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

var (
	_ store.SetReader      = (*ManifestStore)(nil)
	_ store.VersionCounter = (*ManifestStore)(nil)
	_ store.StateSetReader = (*OperationalStore)(nil)
)

// CurrentOfKind returns, in one query, what GetCurrent returns for every
// manifest of a kind: the working copy where there is one, else the
// highest version.
func (m *ManifestStore) CurrentOfKind(ctx context.Context, kind string) ([]store.Version, error) {
	rows, err := m.ex.QueryContext(ctx, `
		SELECT kind, id, 0, yaml, 'local', 'working copy', on_ts
		FROM working_copies WHERE kind = ?
		UNION ALL
		SELECT mv.kind, mv.id, mv.number, mv.yaml, mv.actor, mv.reason, mv.on_ts
		FROM manifest_versions mv
		JOIN (SELECT id, MAX(number) AS number FROM manifest_versions WHERE kind = ? GROUP BY id) cur
		  ON cur.id = mv.id AND cur.number = mv.number
		WHERE mv.kind = ?
		  AND NOT EXISTS (SELECT 1 FROM working_copies wc WHERE wc.kind = mv.kind AND wc.id = mv.id)
		ORDER BY 2`, kind, kind, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []store.Version{}
	for rows.Next() {
		var v store.Version
		var text, on string
		if err := rows.Scan(&v.Kind, &v.ID, &v.Number, &text, &v.Actor, &v.Reason, &on); err != nil {
			return nil, err
		}
		v.YAML = []byte(text)
		if t, err := time.Parse(timeLayout, on); err == nil {
			v.On = t
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ReferencingKind returns, in one query, who references each manifest of
// toKind: ListReferencing for all of them at once.
func (m *ManifestStore) ReferencingKind(ctx context.Context, toKind string) (map[string][]store.Summary, error) {
	rows, err := m.ex.QueryContext(ctx, `
		SELECT to_id, kind, id, name, version, on_ts FROM (
			SELECT r.to_id, mv.kind, mv.id, mv.name, mv.number AS version, mv.on_ts, 0 AS pref
			FROM manifest_references r
			JOIN manifest_versions mv ON mv.kind = r.from_kind AND mv.id = r.from_id
			JOIN (SELECT kind, id, MAX(number) AS number FROM manifest_versions GROUP BY kind, id) cur
			  ON cur.kind = mv.kind AND cur.id = mv.id AND cur.number = mv.number
			WHERE r.to_kind = ?
			UNION ALL
			SELECT r.to_id, wc.kind, wc.id, wc.name, 0, wc.on_ts, 1
			FROM manifest_references r
			JOIN working_copies wc ON wc.kind = r.from_kind AND wc.id = r.from_id
			WHERE r.to_kind = ?
		) results
		ORDER BY to_id, kind, id, pref`, toKind, toKind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]store.Summary{}
	seen := map[string]bool{}
	for rows.Next() {
		var to, on string
		var s store.Summary
		if err := rows.Scan(&to, &s.Kind, &s.ID, &s.Name, &s.Version, &on); err != nil {
			return nil, err
		}
		if s.UpdatedOn, err = time.Parse(timeLayout, on); err != nil {
			return nil, err
		}
		// A manifest with a version and a working copy is listed once, as
		// ListReferencing lists it: by its version.
		key := to + "\x00" + s.Kind + "/" + s.ID
		if !seen[key] {
			out[to] = append(out[to], s)
			seen[key] = true
		}
	}
	return out, rows.Err()
}

// LatestNumber returns the highest version number, 0 when there is none.
func (m *ManifestStore) LatestNumber(ctx context.Context, kind, id string) (int, error) {
	var n int
	err := m.ex.QueryRowContext(ctx, `SELECT COALESCE(MAX(number), 0) FROM manifest_versions WHERE kind = ? AND id = ?`, kind, id).Scan(&n)
	return n, err
}

// inList is "?, ?, ..." for n values, and the values as arguments, for
// an IN over ids.
func inList(ids []string, before ...any) (string, []any) {
	marks := make([]string, len(ids))
	args := append([]any(nil), before...)
	for i, id := range ids {
		marks[i] = "?"
		args = append(args, id)
	}
	return strings.Join(marks, ", "), args
}

// CurrentMany returns the current version of each of ids that exists.
func (m *ManifestStore) CurrentMany(ctx context.Context, kind string, ids []string) ([]store.Version, error) {
	all, err := m.CurrentOfKind(ctx, kind)
	if err != nil {
		return nil, err
	}
	want := make(map[string]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	out := []store.Version{}
	for _, v := range all {
		if want[v.ID] {
			out = append(out, v)
		}
	}
	return out, nil
}

// CurrentStates returns the latest state of each project with history,
// a query per thousand projects, inside SQLite's limit on parameters.
func (o *OperationalStore) CurrentStates(ctx context.Context, projectIDs []string) (map[string]string, error) {
	out := map[string]string{}
	for start := 0; start < len(projectIDs); start += 1000 {
		chunk := projectIDs[start:min(start+1000, len(projectIDs))]
		marks, args := inList(chunk)
		rows, err := o.db.QueryContext(ctx, `
			SELECT project_id, state FROM project_state
			WHERE project_id IN (`+marks+`) ORDER BY project_id, on_ts ASC, rowid ASC`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id, state string
			if err := rows.Scan(&id, &state); err != nil {
				rows.Close()
				return nil, err
			}
			out[id] = state // the last row of each project is its state
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
