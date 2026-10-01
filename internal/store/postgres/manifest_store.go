package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ProjectCartograph/cartograph-engine/internal/store"
	"github.com/ProjectCartograph/cartograph-engine/internal/store/manifestmeta"
)

// ManifestStore is the Postgres adapter for store.ManifestStore.
// Construct it with NewManifestStore.
type ManifestStore struct {
	pool *pgxpool.Pool // nil inside a transaction
	q    querier
}

var _ store.ManifestStore = (*ManifestStore)(nil)

// NewManifestStore returns a ManifestStore over a pool Open returned.
func NewManifestStore(pool *pgxpool.Pool) *ManifestStore {
	return &ManifestStore{pool: pool, q: pool}
}

// PutVersion inserts v only when its number is one more than the
// highest recorded, in one statement. Two writers racing for the same
// number both pass that test, and the primary key refuses the second.
func (m *ManifestStore) PutVersion(ctx context.Context, v store.Version) error {
	tag, err := m.q.Exec(ctx, `
		INSERT INTO manifest_versions (kind, id, number, name, yaml, actor, reason, on_ts)
		SELECT $1, $2, $3, $4, $5, $6, $7, $8
		WHERE $3 = 1 + COALESCE((SELECT max(number) FROM manifest_versions WHERE kind = $1 AND id = $2), 0)`,
		v.Kind, v.ID, v.Number, manifestmeta.Name(v.YAML), string(v.YAML), v.Actor, v.Reason, stamp(v.On))
	if err != nil {
		return fmt.Errorf("put version %s/%s %d: %w", v.Kind, v.ID, v.Number, err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("put version %s/%s: %d is not one more than the current version", v.Kind, v.ID, v.Number)
	}
	return nil
}

// PutWorking replaces a manifest's working copy.
func (m *ManifestStore) PutWorking(ctx context.Context, kind, id string, yamlBytes []byte) error {
	_, err := m.q.Exec(ctx, `
		INSERT INTO working_copies (kind, id, name, yaml, on_ts) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (kind, id) DO UPDATE SET name = excluded.name, yaml = excluded.yaml, on_ts = excluded.on_ts`,
		kind, id, manifestmeta.Name(yamlBytes), string(yamlBytes), stamp(time.Time{}))
	if err != nil {
		return fmt.Errorf("put working copy %s/%s: %w", kind, id, err)
	}
	return nil
}

// GetWorking returns a manifest's working copy, if it has one.
func (m *ManifestStore) GetWorking(ctx context.Context, kind, id string) ([]byte, bool, error) {
	var text string
	err := m.q.QueryRow(ctx, `SELECT yaml FROM working_copies WHERE kind = $1 AND id = $2`, kind, id).Scan(&text)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("get working copy %s/%s: %w", kind, id, err)
	}
	return []byte(text), true, nil
}

// GetCurrent returns the working copy when there is one, as version 0,
// and the highest version otherwise: the same answer the SQLite adapter
// gives.
func (m *ManifestStore) GetCurrent(ctx context.Context, kind, id string) (store.Version, bool, error) {
	var text string
	var on time.Time
	err := m.q.QueryRow(ctx, `SELECT yaml, on_ts FROM working_copies WHERE kind = $1 AND id = $2`, kind, id).Scan(&text, &on)
	switch {
	case err == nil:
		return store.Version{Kind: kind, ID: id, YAML: []byte(text), Actor: "local", Reason: "working copy", On: on.UTC()}, true, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return store.Version{}, false, fmt.Errorf("get current %s/%s: %w", kind, id, err)
	}
	return m.oneVersion(ctx, `
		SELECT kind, id, number, yaml, actor, reason, on_ts FROM manifest_versions
		WHERE kind = $1 AND id = $2 ORDER BY number DESC LIMIT 1`, kind, id)
}

// GetVersion returns one version, if it exists.
func (m *ManifestStore) GetVersion(ctx context.Context, kind, id string, number int) (store.Version, bool, error) {
	return m.oneVersion(ctx, `
		SELECT kind, id, number, yaml, actor, reason, on_ts FROM manifest_versions
		WHERE kind = $1 AND id = $2 AND number = $3`, kind, id, number)
}

func (m *ManifestStore) oneVersion(ctx context.Context, sql string, args ...any) (store.Version, bool, error) {
	rows, err := m.q.Query(ctx, sql, args...)
	if err != nil {
		return store.Version{}, false, fmt.Errorf("get version: %w", err)
	}
	vs, err := collectVersions(rows)
	if err != nil || len(vs) == 0 {
		return store.Version{}, false, err
	}
	return vs[0], true, nil
}

func collectVersions(rows pgx.Rows) ([]store.Version, error) {
	out := []store.Version{}
	defer rows.Close()
	for rows.Next() {
		var v store.Version
		var text string
		if err := rows.Scan(&v.Kind, &v.ID, &v.Number, &text, &v.Actor, &v.Reason, &v.On); err != nil {
			return nil, fmt.Errorf("read version: %w", err)
		}
		v.YAML = []byte(text)
		v.On = v.On.UTC()
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read versions: %w", err)
	}
	return out, nil
}

// ListVersions returns every version of a manifest, oldest first.
func (m *ManifestStore) ListVersions(ctx context.Context, kind, id string) ([]store.Version, error) {
	rows, err := m.q.Query(ctx, `
		SELECT kind, id, number, yaml, actor, reason, on_ts FROM manifest_versions
		WHERE kind = $1 AND id = $2 ORDER BY number`, kind, id)
	if err != nil {
		return nil, fmt.Errorf("list versions %s/%s: %w", kind, id, err)
	}
	return collectVersions(rows)
}

// ListAllVersions pages through every version, newest first. The cursor
// is the last row's sort key, "on,kind,id,number", the format the other
// adapters use.
func (m *ManifestStore) ListAllVersions(ctx context.Context, limit int, cursor string) ([]store.Version, string, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	sql := `SELECT kind, id, number, yaml, actor, reason, on_ts FROM manifest_versions`
	args := []any{}
	if parts := strings.Split(cursor, ","); len(parts) == 4 {
		on, err := time.Parse(time.RFC3339Nano, parts[0])
		if err != nil {
			return nil, "", fmt.Errorf("list all versions: cursor %q: %w", cursor, err)
		}
		n, err := strconv.Atoi(parts[3])
		if err != nil {
			return nil, "", fmt.Errorf("list all versions: cursor %q: %w", cursor, err)
		}
		// The rows after the cursor in the order below, which mixes
		// directions, so a plain row comparison would not do.
		sql += ` WHERE on_ts < $1 OR (on_ts = $1 AND (kind > $2 OR (kind = $2 AND (id > $3 OR (id = $3 AND number < $4)))))`
		args = append(args, on, parts[1], parts[2], n)
	}
	sql += fmt.Sprintf(` ORDER BY on_ts DESC, kind, id, number DESC LIMIT %d`, limit+1)
	rows, err := m.q.Query(ctx, sql, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list all versions: %w", err)
	}
	out, err := collectVersions(rows)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > limit {
		out = out[:limit]
		last := out[limit-1]
		next = fmt.Sprintf("%s,%s,%s,%d", last.On.Format(time.RFC3339Nano), last.Kind, last.ID, last.Number)
	}
	return out, next, nil
}

// current is every manifest's current content, one row per manifest:
// the highest version where there is one, else the working copy. That
// is the preference the SQLite and memory adapters show in a list.
const current = `(
	SELECT DISTINCT ON (kind, id) kind, id, name, version, on_ts, yaml FROM (
		SELECT kind, id, name, number AS version, on_ts, yaml, 0 AS pref FROM manifest_versions
		UNION ALL
		SELECT kind, id, name, 0, on_ts, yaml, 1 FROM working_copies
	) c
	ORDER BY kind, id, pref, version DESC
)`

// ListSummaries returns the current content of every manifest of a kind
// that matches the query and every reference filter.
func (m *ManifestStore) ListSummaries(ctx context.Context, kind, query string, refs []store.RefFilter) ([]store.Summary, error) {
	var b strings.Builder
	b.WriteString(`SELECT kind, id, name, version, on_ts, yaml FROM ` + current + ` cur
		WHERE kind = $1 AND ($2 = '' OR strpos(lower(id), $2) > 0 OR strpos(lower(name), $2) > 0)`)
	args := []any{kind, strings.ToLower(strings.TrimSpace(query))}
	for _, f := range refs {
		args = append(args, f.Kind, f.ID)
		fmt.Fprintf(&b, ` AND EXISTS (SELECT 1 FROM manifest_references r
			WHERE r.from_kind = cur.kind AND r.from_id = cur.id AND r.to_kind = $%d AND r.to_id = $%d)`, len(args)-1, len(args))
	}
	b.WriteString(` ORDER BY id`)
	rows, err := m.q.Query(ctx, b.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("list summaries %s: %w", kind, err)
	}
	return collectSummaries(rows)
}

func collectSummaries(rows pgx.Rows) ([]store.Summary, error) {
	defer rows.Close()
	out := []store.Summary{}
	for rows.Next() {
		var s store.Summary
		var text string
		if err := rows.Scan(&s.Kind, &s.ID, &s.Name, &s.Version, &s.UpdatedOn, &text); err != nil {
			return nil, fmt.Errorf("read summary: %w", err)
		}
		s.UpdatedOn = s.UpdatedOn.UTC()
		s.Labels = manifestmeta.Labels([]byte(text))
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read summaries: %w", err)
	}
	return out, nil
}

// ListIDs returns the id of every manifest of a kind, versioned or only
// saved as a working copy.
func (m *ManifestStore) ListIDs(ctx context.Context, kind string) ([]string, error) {
	rows, err := m.q.Query(ctx, `
		SELECT id FROM manifest_versions WHERE kind = $1
		UNION
		SELECT id FROM working_copies WHERE kind = $1
		ORDER BY id`, kind)
	if err != nil {
		return nil, fmt.Errorf("list ids %s: %w", kind, err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("list ids %s: %w", kind, err)
	}
	if ids == nil {
		ids = []string{}
	}
	return ids, nil
}

// Counts returns how many manifests each kind has.
func (m *ManifestStore) Counts(ctx context.Context) (map[string]int, error) {
	rows, err := m.q.Query(ctx, `
		SELECT kind, count(*) FROM (
			SELECT kind, id FROM manifest_versions
			UNION
			SELECT kind, id FROM working_copies
		) m GROUP BY kind`)
	if err != nil {
		return nil, fmt.Errorf("count manifests: %w", err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var kind string
		var n int
		if err := rows.Scan(&kind, &n); err != nil {
			return nil, fmt.Errorf("count manifests: %w", err)
		}
		out[kind] = n
	}
	return out, rows.Err()
}

// IndexReferences replaces a manifest's recorded outgoing references.
func (m *ManifestStore) IndexReferences(ctx context.Context, fromKind, fromID string, refs []store.Ref) error {
	return m.inTx(ctx, func(q querier) error {
		if _, err := q.Exec(ctx, `DELETE FROM manifest_references WHERE from_kind = $1 AND from_id = $2`, fromKind, fromID); err != nil {
			return err
		}
		for _, r := range refs {
			if _, err := q.Exec(ctx, `
				INSERT INTO manifest_references (from_kind, from_id, path, to_kind, to_id) VALUES ($1, $2, $3, $4, $5)
				ON CONFLICT (from_kind, from_id, path) DO UPDATE SET to_kind = excluded.to_kind, to_id = excluded.to_id`,
				fromKind, fromID, r.Path, r.ToKind, r.ToID); err != nil {
				return err
			}
		}
		return nil
	})
}

// ListReferencedBy returns a manifest's recorded outgoing references.
func (m *ManifestStore) ListReferencedBy(ctx context.Context, fromKind, fromID string) ([]store.Ref, error) {
	rows, err := m.q.Query(ctx, `
		SELECT path, to_kind, to_id FROM manifest_references
		WHERE from_kind = $1 AND from_id = $2 ORDER BY path`, fromKind, fromID)
	if err != nil {
		return nil, fmt.Errorf("list references from %s/%s: %w", fromKind, fromID, err)
	}
	refs, err := pgx.CollectRows(rows, pgx.RowToStructByPos[store.Ref])
	if err != nil {
		return nil, fmt.Errorf("list references from %s/%s: %w", fromKind, fromID, err)
	}
	if refs == nil {
		refs = []store.Ref{}
	}
	return refs, nil
}

// ListReferencing returns the current summary of every manifest that
// references toKind/toID.
func (m *ManifestStore) ListReferencing(ctx context.Context, toKind, toID string) ([]store.Summary, error) {
	rows, err := m.q.Query(ctx, `
		SELECT kind, id, name, version, on_ts, yaml FROM `+current+` cur
		WHERE EXISTS (SELECT 1 FROM manifest_references r
			WHERE r.from_kind = cur.kind AND r.from_id = cur.id AND r.to_kind = $1 AND r.to_id = $2)
		ORDER BY kind, id`, toKind, toID)
	if err != nil {
		return nil, fmt.Errorf("list references to %s/%s: %w", toKind, toID, err)
	}
	return collectSummaries(rows)
}

// WithinTransaction runs fn against a ManifestStore inside one
// transaction, committed when fn returns nil.
func (m *ManifestStore) WithinTransaction(ctx context.Context, fn func(ctx context.Context, tx store.ManifestStore) error) error {
	if m.pool == nil {
		return errors.New("postgres: nested transactions are not supported")
	}
	return pgx.BeginFunc(ctx, m.pool, func(tx pgx.Tx) error {
		return fn(ctx, &ManifestStore{q: tx})
	})
}

// inTx runs fn in a transaction of its own, or in the one already open.
func (m *ManifestStore) inTx(ctx context.Context, fn func(q querier) error) error {
	if m.pool == nil {
		return fn(m.q)
	}
	return pgx.BeginFunc(ctx, m.pool, func(tx pgx.Tx) error { return fn(tx) })
}

// Exclude records that a manifest is excluded. There is no state
// manifest to remove it from; the record is what an interface reads.
func (m *ManifestStore) Exclude(ctx context.Context, kind, id, name, reason, operator string) error {
	_, err := m.q.Exec(ctx, `
		INSERT INTO exclusions (kind, id, name, on_ts, reason, operator) VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (kind, id) DO UPDATE SET name = excluded.name, on_ts = excluded.on_ts, reason = excluded.reason, operator = excluded.operator`,
		kind, id, name, stamp(time.Time{}), reason, operator)
	if err != nil {
		return fmt.Errorf("exclude %s/%s: %w", kind, id, err)
	}
	return nil
}

// ListExcluded returns every exclusion, newest first.
func (m *ManifestStore) ListExcluded(ctx context.Context) ([]store.Exclusion, error) {
	rows, err := m.q.Query(ctx, `SELECT kind, id, name, on_ts, reason, operator FROM exclusions ORDER BY on_ts DESC, kind, id`)
	if err != nil {
		return nil, fmt.Errorf("list exclusions: %w", err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByPos[store.Exclusion])
	if err != nil {
		return nil, fmt.Errorf("list exclusions: %w", err)
	}
	for i := range out {
		out[i].On = out[i].On.UTC()
	}
	if out == nil {
		out = []store.Exclusion{}
	}
	return out, nil
}

// Recover removes an exclusion record.
func (m *ManifestStore) Recover(ctx context.Context, kind, id, reason, operator string) error {
	if _, err := m.q.Exec(ctx, `DELETE FROM exclusions WHERE kind = $1 AND id = $2`, kind, id); err != nil {
		return fmt.Errorf("recover %s/%s: %w", kind, id, err)
	}
	return nil
}
