package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store/manifestmeta"
)

const timeLayout = time.RFC3339Nano

// execer is satisfied by both *sql.DB and *sql.Tx.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// ManifestStore is the SQLite adapter for store.ManifestStore. The zero
// value is not usable; construct with NewManifestStore.
type ManifestStore struct {
	db *sql.DB // set only on the top-level (non-transactional) instance
	ex execer
}

func NewManifestStore(db *sql.DB) *ManifestStore {
	return &ManifestStore{db: db, ex: db}
}

func (m *ManifestStore) PutVersion(ctx context.Context, v store.Version) error {
	// The expected number follows the version log, not GetCurrent, which
	// returns a working copy as number 0.
	latest, err := m.LatestNumber(ctx, v.Kind, v.ID)
	if err != nil {
		return fmt.Errorf("latest version: %w", err)
	}
	want := latest + 1
	if v.Number != want {
		fmt.Fprintf(os.Stderr, "put version: version number mismatch for %s/%s: expected %d, got %d\n", v.Kind, v.ID, want, v.Number)
		return fmt.Errorf("put version: expected number %d for %s/%s, got %d", want, v.Kind, v.ID, v.Number)
	}
	name := manifestmeta.Name(v.YAML)
	on := v.On
	if on.IsZero() {
		on = time.Now().UTC()
	}
	_, err = m.ex.ExecContext(ctx, `
		INSERT INTO manifest_versions (kind, id, number, name, yaml, actor, reason, on_ts)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		v.Kind, v.ID, v.Number, name, string(v.YAML), v.Actor, v.Reason, on.Format(timeLayout))
	return err
}

// PutWorking stores a manifest's working copy (autosave without versioning).
func (m *ManifestStore) PutWorking(ctx context.Context, kind, id string, yamlBytes []byte) error {
	name := manifestmeta.Name(yamlBytes)
	_, err := m.ex.ExecContext(ctx, `
		INSERT INTO working_copies (kind, id, name, yaml, on_ts)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(kind, id) DO UPDATE SET yaml = ?, name = ?, on_ts = ?`,
		kind, id, name, string(yamlBytes), time.Now().UTC().Format(timeLayout),
		string(yamlBytes), name, time.Now().UTC().Format(timeLayout))
	return err
}

func (m *ManifestStore) GetCurrent(ctx context.Context, kind, id string) (store.Version, bool, error) {
	// Working copy is the current content when one exists
	row := m.ex.QueryRowContext(ctx, `
		SELECT kind, id, yaml, on_ts
		FROM working_copies
		WHERE kind = ? AND id = ?`, kind, id)
	var working store.Version
	var yaml, onText string
	err := row.Scan(&working.Kind, &working.ID, &yaml, &onText)
	if err == nil {
		// Working copy exists, return it as current content
		working.YAML = []byte(yaml)
		working.Number = 0 // Working copies don't have version numbers
		working.Actor = "local"
		working.Reason = "working copy"
		if on, parseErr := time.Parse(timeLayout, onText); parseErr == nil {
			working.On = on
		}
		return working, true, nil
	}
	if err != sql.ErrNoRows {
		return store.Version{}, false, err
	}
	// Fall back to latest snapshot
	row = m.ex.QueryRowContext(ctx, `
		SELECT kind, id, number, yaml, actor, reason, on_ts
		FROM manifest_versions
		WHERE kind = ? AND id = ?
		ORDER BY number DESC LIMIT 1`, kind, id)
	return scanVersion(row)
}

func (m *ManifestStore) GetVersion(ctx context.Context, kind, id string, number int) (store.Version, bool, error) {
	row := m.ex.QueryRowContext(ctx, `
		SELECT kind, id, number, yaml, actor, reason, on_ts
		FROM manifest_versions
		WHERE kind = ? AND id = ? AND number = ?`, kind, id, number)
	return scanVersion(row)
}

func scanVersion(row *sql.Row) (store.Version, bool, error) {
	var v store.Version
	var yamlText, onText string
	err := row.Scan(&v.Kind, &v.ID, &v.Number, &yamlText, &v.Actor, &v.Reason, &onText)
	if err == sql.ErrNoRows {
		return store.Version{}, false, nil
	}
	if err != nil {
		return store.Version{}, false, err
	}
	v.YAML = []byte(yamlText)
	v.On, err = time.Parse(timeLayout, onText)
	if err != nil {
		return store.Version{}, false, err
	}
	return v, true, nil
}

func (m *ManifestStore) ListVersions(ctx context.Context, kind, id string) ([]store.Version, error) {
	rows, err := m.ex.QueryContext(ctx, `
		SELECT kind, id, number, yaml, actor, reason, on_ts
		FROM manifest_versions
		WHERE kind = ? AND id = ?
		ORDER BY number ASC`, kind, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []store.Version{}
	for rows.Next() {
		var v store.Version
		var yamlText, onText string
		if err := rows.Scan(&v.Kind, &v.ID, &v.Number, &yamlText, &v.Actor, &v.Reason, &onText); err != nil {
			return nil, err
		}
		v.YAML = []byte(yamlText)
		if v.On, err = time.Parse(timeLayout, onText); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (m *ManifestStore) ListAllVersions(ctx context.Context, limit int, cursor string) ([]store.Version, string, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	// Cursor is encoded as "on_ts,kind,id,number" to uniquely identify
	// a position in the result set (on_ts may not be unique).
	var fromOpts string
	if cursor != "" {
		fromOpts = cursor
	}

	// Query all versions ordered by on_ts descending (newest first), with
	// a tie-breaker using kind, id, number to ensure deterministic ordering.
	query := `
		SELECT kind, id, number, yaml, actor, reason, on_ts
		FROM manifest_versions`
	args := []any{}

	if fromOpts != "" {
		// Parse cursor: "on_ts,kind,id,number"
		parts := strings.Split(fromOpts, ",")
		if len(parts) == 4 {
			query += ` WHERE (on_ts, kind, id, number) < (?, ?, ?, ?)`
			args = append(args, parts[0], parts[1], parts[2], parts[3])
		}
	}

	query += ` ORDER BY on_ts DESC, kind ASC, id ASC, number DESC LIMIT ?`
	args = append(args, limit+1) // Fetch one extra to determine if there's a next page

	rows, err := m.ex.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	out := []store.Version{}
	for rows.Next() {
		var v store.Version
		var yamlText, onText string
		if err := rows.Scan(&v.Kind, &v.ID, &v.Number, &yamlText, &v.Actor, &v.Reason, &onText); err != nil {
			return nil, "", err
		}
		v.YAML = []byte(yamlText)
		if v.On, err = time.Parse(timeLayout, onText); err != nil {
			return nil, "", err
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}

	nextCursor := ""
	if len(out) > limit {
		// We have more results, so include the next cursor
		last := out[limit-1]
		nextCursor = fmt.Sprintf("%s,%s,%s,%d", last.On.Format(timeLayout), last.Kind, last.ID, last.Number)
		out = out[:limit]
	}

	return out, nextCursor, nil
}

func (m *ManifestStore) ListSummaries(ctx context.Context, kind, query string, refs []store.RefFilter) ([]store.Summary, error) {
	like := "%" + strings.ToLower(strings.TrimSpace(query)) + "%"
	trimmedQuery := strings.TrimSpace(query)

	// Combine versions and working copies, preferring working copies
	q := strings.Builder{}
	args := []any{}
	// yaml comes back so metadata.labels can be read off each row. A
	// label is free-form and has no column; parsing the row's own text is
	// what keeps it that way, and a list is bounded by the register's
	// size rather than by anything a request controls.
	q.WriteString(`
		SELECT kind, id, name, COALESCE(version, 0), on_ts, yaml
		FROM (
			-- Get latest versions
			SELECT mv.kind, mv.id, mv.name, mv.number AS version, mv.on_ts, mv.yaml
			FROM manifest_versions mv
			JOIN (
				SELECT kind, id, MAX(number) AS number
				FROM manifest_versions
				WHERE kind = ?
				GROUP BY kind, id
			) cur ON cur.kind = mv.kind AND cur.id = mv.id AND cur.number = mv.number
			UNION ALL
			-- Get working copies (working copies have no version, preferring them with NULL version)
			SELECT kind, id, name, NULL, on_ts, yaml
			FROM working_copies
			WHERE kind = ?
		) current
		WHERE kind = ?
		  AND (? = '' OR LOWER(id) LIKE ? OR LOWER(name) LIKE ?)`)
	args = append(args, kind, kind, kind, trimmedQuery, like, like)
	for _, f := range refs {
		q.WriteString(`
		  AND EXISTS (
			SELECT 1 FROM manifest_references r
			WHERE r.from_kind = current.kind AND r.from_id = current.id AND r.to_kind = ? AND r.to_id = ?
		  )`)
		args = append(args, f.Kind, f.ID)
	}
	q.WriteString(` ORDER BY id ASC`)

	rows, err := m.ex.QueryContext(ctx, q.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []store.Summary{}
	seen := make(map[string]bool) // Track which IDs we've already seen (prefer working copies)

	for rows.Next() {
		var s store.Summary
		var onText string
		var version sql.NullInt64
		var yamlText string
		if err := rows.Scan(&s.Kind, &s.ID, &s.Name, &version, &onText, &yamlText); err != nil {
			return nil, err
		}
		s.Labels = manifestmeta.Labels([]byte(yamlText))
		if version.Valid {
			s.Version = int(version.Int64)
		} else {
			s.Version = 0
		}
		if s.UpdatedOn, err = time.Parse(timeLayout, onText); err != nil {
			return nil, err
		}

		key := s.Kind + "/" + s.ID
		if !seen[key] {
			out = append(out, s)
			seen[key] = true
		}
	}
	return out, rows.Err()
}

// IndexReferences replaces every recorded outgoing reference for
// fromKind/fromID with refs.
func (m *ManifestStore) IndexReferences(ctx context.Context, fromKind, fromID string, refs []store.Ref) error {
	if _, err := m.ex.ExecContext(ctx,
		`DELETE FROM manifest_references WHERE from_kind = ? AND from_id = ?`, fromKind, fromID); err != nil {
		return err
	}
	for _, r := range refs {
		if _, err := m.ex.ExecContext(ctx, `
			INSERT INTO manifest_references (from_kind, from_id, to_kind, to_id, path)
			VALUES (?, ?, ?, ?, ?)`, fromKind, fromID, r.ToKind, r.ToID, r.Path); err != nil {
			return err
		}
	}
	return nil
}

func (m *ManifestStore) ListReferencedBy(ctx context.Context, fromKind, fromID string) ([]store.Ref, error) {
	rows, err := m.ex.QueryContext(ctx, `
		SELECT to_kind, to_id, path FROM manifest_references
		WHERE from_kind = ? AND from_id = ? ORDER BY path`, fromKind, fromID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []store.Ref{}
	for rows.Next() {
		var r store.Ref
		if err := rows.Scan(&r.ToKind, &r.ToID, &r.Path); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (m *ManifestStore) ListReferencing(ctx context.Context, toKind, toID string) ([]store.Summary, error) {
	rows, err := m.ex.QueryContext(ctx, `
		SELECT DISTINCT kind, id, name, version, on_ts
		FROM (
			-- Current versions
			SELECT mv.kind, mv.id, mv.name, mv.number AS version, mv.on_ts
			FROM manifest_references r
			JOIN manifest_versions mv ON mv.kind = r.from_kind AND mv.id = r.from_id
			JOIN (
				SELECT kind, id, MAX(number) AS number
				FROM manifest_versions
				GROUP BY kind, id
			) cur ON cur.kind = mv.kind AND cur.id = mv.id AND cur.number = mv.number
			WHERE r.to_kind = ? AND r.to_id = ?
			UNION ALL
			-- Working copies
			SELECT wc.kind, wc.id, wc.name, NULL, wc.on_ts
			FROM manifest_references r
			JOIN working_copies wc ON wc.kind = r.from_kind AND wc.id = r.from_id
			WHERE r.to_kind = ? AND r.to_id = ?
		) results
		ORDER BY kind, id`, toKind, toID, toKind, toID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []store.Summary{}
	seen := make(map[string]bool)
	for rows.Next() {
		var s store.Summary
		var onText string
		var version sql.NullInt64
		if err := rows.Scan(&s.Kind, &s.ID, &s.Name, &version, &onText); err != nil {
			return nil, err
		}
		if version.Valid {
			s.Version = int(version.Int64)
		} else {
			s.Version = 0
		}
		if s.UpdatedOn, err = time.Parse(timeLayout, onText); err != nil {
			return nil, err
		}
		key := s.Kind + "/" + s.ID
		if !seen[key] {
			out = append(out, s)
			seen[key] = true
		}
	}
	return out, rows.Err()
}

func (m *ManifestStore) ListIDs(ctx context.Context, kind string) ([]string, error) {
	rows, err := m.ex.QueryContext(ctx, `
		SELECT DISTINCT id FROM manifest_versions WHERE kind = ?
		UNION
		SELECT DISTINCT id FROM working_copies WHERE kind = ?
		ORDER BY id`, kind, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (m *ManifestStore) Counts(ctx context.Context) (map[string]int, error) {
	rows, err := m.ex.QueryContext(ctx, `
		SELECT kind, COUNT(DISTINCT id) FROM manifest_versions GROUP BY kind
		UNION ALL
		SELECT kind, COUNT(DISTINCT id) FROM working_copies GROUP BY kind`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var kind string
		var n int
		if err := rows.Scan(&kind, &n); err != nil {
			return nil, err
		}
		// Combine counts from both tables (use max since an ID might be in both)
		if existing, ok := out[kind]; ok {
			if n > existing {
				out[kind] = n
			}
		} else {
			out[kind] = n
		}
	}
	return out, rows.Err()
}

func (m *ManifestStore) WithinTransaction(ctx context.Context, fn func(ctx context.Context, tx store.ManifestStore) error) error {
	if m.db == nil {
		return fmt.Errorf("nested transactions are not supported")
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	txStore := &ManifestStore{ex: tx}
	if err := fn(ctx, txStore); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// GetWorking returns a manifest's working copy (autosave without versioning).
// Returns found=false when none exists.
func (m *ManifestStore) GetWorking(ctx context.Context, kind, id string) ([]byte, bool, error) {
	row := m.ex.QueryRowContext(ctx, `
		SELECT yaml FROM working_copies
		WHERE kind = ? AND id = ?`, kind, id)
	var yamlText string
	err := row.Scan(&yamlText)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return []byte(yamlText), true, nil
}
