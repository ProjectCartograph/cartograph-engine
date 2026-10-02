package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store/manifestmeta"
)

// ManifestStore is the Postgres adapter for store.ManifestStore.
// Construct it with NewManifestStore.
type ManifestStore struct {
	pool *pgxpool.Pool // nil inside a transaction
	q    querier
}

var (
	_ store.ManifestStore = (*ManifestStore)(nil)
	_ store.SummaryPager  = (*ManifestStore)(nil)
)

// NewManifestStore returns a ManifestStore over a pool Open returned.
func NewManifestStore(pool *pgxpool.Pool) *ManifestStore {
	return &ManifestStore{pool: pool, q: pool}
}

// Two SQLSTATEs mean a version that is not the next one: the version
// trigger's own (migrations/0003_documents.sql), and a unique violation
// when two writers race for the first version of a new manifest.
const (
	errNotNext   = "CG001"
	errDuplicate = "23505"
)

// PutVersion appends v with its document. The trigger on
// manifest_versions accepts it only as one more than the manifest's
// current version, with the manifest's row locked, and moves the row on.
func (m *ManifestStore) PutVersion(ctx context.Context, v store.Version) error {
	_, err := m.q.Exec(ctx, `
		INSERT INTO manifest_versions (kind, id, number, name, yaml, actor, reason, on_ts, doc)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		v.Kind, v.ID, v.Number, manifestmeta.Name(v.YAML), string(v.YAML), v.Actor, v.Reason, stamp(v.On), docOf(v.Doc, v.YAML))
	var pgErr *pgconn.PgError
	switch {
	case errors.As(err, &pgErr) && (pgErr.Code == errNotNext || pgErr.Code == errDuplicate):
		return fmt.Errorf("put version %s/%s: %d is not one more than the current version", v.Kind, v.ID, v.Number)
	case err != nil:
		return fmt.Errorf("put version %s/%s %d: %w", v.Kind, v.ID, v.Number, err)
	}
	return nil
}

// docOf is the document to store: the engine's where it gave one, else
// one read from the text, else none (jsonb null), which the engine's
// repair fills later.
func docOf(doc, text []byte) any {
	if doc == nil {
		doc = manifestmeta.Doc(text)
	}
	if doc == nil {
		return nil
	}
	return string(doc)
}

// PutWorking replaces a manifest's working copy.
func (m *ManifestStore) PutWorking(ctx context.Context, kind, id string, yamlBytes []byte) error {
	return m.PutWorkingDoc(ctx, kind, id, yamlBytes, nil)
}

// PutWorkingDoc replaces a manifest's working copy, with its document.
func (m *ManifestStore) PutWorkingDoc(ctx context.Context, kind, id string, text, doc []byte) error {
	_, err := m.q.Exec(ctx, `
		INSERT INTO working_copies (kind, id, name, yaml, on_ts, doc) VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (kind, id) DO UPDATE SET name = excluded.name, yaml = excluded.yaml, on_ts = excluded.on_ts, doc = excluded.doc`,
		kind, id, manifestmeta.Name(text), string(text), stamp(time.Time{}), docOf(doc, text))
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

// currentSQL selects what GetCurrent returns, for the manifests a WHERE
// clause on m names: the working copy where there is one, as version 0,
// else the highest version, found through the manifest's own row.
const currentSQL = `
	SELECT m.kind, m.id,
	       CASE WHEN w.kind IS NULL THEN v.number ELSE 0 END,
	       coalesce(w.yaml, v.yaml),
	       CASE WHEN w.kind IS NULL THEN v.actor ELSE 'local' END,
	       CASE WHEN w.kind IS NULL THEN v.reason ELSE 'working copy' END,
	       coalesce(w.on_ts, v.on_ts)
	FROM manifests m
	LEFT JOIN working_copies w ON m.has_working AND w.kind = m.kind AND w.id = m.id
	LEFT JOIN manifest_versions v ON v.kind = m.kind AND v.id = m.id AND v.number = m.version`

// GetCurrent returns the working copy when there is one, as version 0,
// and the highest version otherwise.
func (m *ManifestStore) GetCurrent(ctx context.Context, kind, id string) (store.Version, bool, error) {
	return m.oneVersion(ctx, currentSQL+` WHERE m.kind = $1 AND m.id = $2`, kind, id)
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

// ListAllVersions pages through every version, newest first. The cursor
// is the last row's sort key, "on,kind,id,number".
func (m *ManifestStore) ListAllVersions(ctx context.Context, limit int, cursor string) ([]store.Version, string, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	sql := `SELECT ` + historyCols + ` FROM manifest_versions`
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
		// The rows after the cursor in the order below. The order mixes
		// directions, so no single row comparison says it; the plain bound
		// on on_ts lets manifest_versions_on_ts seek to the cursor, leaving
		// only rows saved at the same instant to the rest.
		sql += ` WHERE on_ts <= $1 AND (on_ts < $1 OR (kind > $2 OR (kind = $2 AND (id > $3 OR (id = $3 AND number < $4)))))`
		args = append(args, on, parts[1], parts[2], n)
	}
	sql += fmt.Sprintf(` ORDER BY on_ts DESC, kind, id, number DESC LIMIT %d`, limit+1)
	rows, err := m.q.Query(ctx, sql, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list all versions: %w", err)
	}
	raw, err := scanStored(rows)
	if err != nil {
		return nil, "", err
	}
	out := make([]store.Version, len(raw))
	for i, r := range raw {
		if r.patch == nil {
			vs, err := rebuild([]stored{r})
			if err != nil {
				return nil, "", err
			}
			out[i] = vs[0]
			continue
		}
		// A patch needs the versions before it, back to a keyframe.
		v, _, err := m.GetVersion(ctx, r.v.Kind, r.v.ID, r.v.Number)
		if err != nil {
			return nil, "", err
		}
		out[i] = v
	}
	next := ""
	if len(out) > limit {
		out = out[:limit]
		last := out[limit-1]
		next = fmt.Sprintf("%s,%s,%s,%d", last.On.Format(time.RFC3339Nano), last.Kind, last.ID, last.Number)
	}
	return out, next, nil
}

// summarySQL selects a manifest's list entry from its row: its version
// where it has one, else its working copy as version 0.
const summarySQL = `
	SELECT m.kind, m.id, m.title, m.version,
	       CASE WHEN m.version > 0 THEN m.updated_on ELSE m.working_on END,
	       m.labels
	FROM manifests m`

// ListSummaries returns the current entry of every manifest of a kind
// that matches the query and every reference filter.
func (m *ManifestStore) ListSummaries(ctx context.Context, kind, query string, refs []store.RefFilter) ([]store.Summary, error) {
	return m.ListSummariesAfter(ctx, kind, query, refs, "", 0)
}

// ListSummariesAfter is ListSummaries from the id after afterID, at most
// limit rows (0 for all): a seek in the primary key, then the page.
func (m *ManifestStore) ListSummariesAfter(ctx context.Context, kind, query string, refs []store.RefFilter, afterID string, limit int) ([]store.Summary, error) {
	var b strings.Builder
	b.WriteString(summarySQL + ` WHERE m.kind = $1`)
	args := []any{kind}
	if q := strings.ToLower(strings.TrimSpace(query)); q != "" {
		// LIKE, with the query's own wildcards escaped, is what the
		// trigram indexes answer.
		args = append(args, "%"+likeEscaper.Replace(q)+"%")
		fmt.Fprintf(&b, ` AND (lower(m.id) LIKE $%d OR lower(m.title) LIKE $%d)`, len(args), len(args))
	}
	for _, f := range refs {
		args = append(args, f.Kind, f.ID)
		fmt.Fprintf(&b, ` AND EXISTS (SELECT 1 FROM manifest_references r
			WHERE r.from_kind = m.kind AND r.from_id = m.id AND r.to_kind = $%d AND r.to_id = $%d)`, len(args)-1, len(args))
	}
	if afterID != "" {
		args = append(args, afterID)
		fmt.Fprintf(&b, ` AND m.id > $%d`, len(args))
	}
	b.WriteString(` ORDER BY m.id`)
	if limit > 0 {
		fmt.Fprintf(&b, ` LIMIT %d`, limit)
	}
	rows, err := m.q.Query(ctx, b.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("list summaries %s: %w", kind, err)
	}
	return collectSummaries(rows)
}

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

func collectSummaries(rows pgx.Rows) ([]store.Summary, error) {
	defer rows.Close()
	out := []store.Summary{}
	for rows.Next() {
		s, err := scanSummary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read summaries: %w", err)
	}
	return out, nil
}

// scanSummary reads the columns summarySQL selects, after any the
// caller selected before them.
func scanSummary(rows pgx.Rows, before ...any) (store.Summary, error) {
	var s store.Summary
	var on *time.Time
	var labels map[string]string
	if err := rows.Scan(append(before, &s.Kind, &s.ID, &s.Name, &s.Version, &on, &labels)...); err != nil {
		return store.Summary{}, fmt.Errorf("read summary: %w", err)
	}
	if on != nil {
		s.UpdatedOn = on.UTC()
	}
	if len(labels) > 0 {
		s.Labels = labels
	}
	return s, nil
}

// ListIDs returns the id of every manifest of a kind, versioned or only
// saved as a working copy.
func (m *ManifestStore) ListIDs(ctx context.Context, kind string) ([]string, error) {
	rows, err := m.q.Query(ctx, `SELECT id FROM manifests WHERE kind = $1 ORDER BY id`, kind)
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
	rows, err := m.q.Query(ctx, `SELECT kind, count(*) FROM manifests GROUP BY kind`)
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

// IndexReferences replaces a manifest's recorded outgoing references:
// one delete and one insert over arrays, sent together in one round trip.
func (m *ManifestStore) IndexReferences(ctx context.Context, fromKind, fromID string, refs []store.Ref) error {
	paths := make([]string, len(refs))
	kinds := make([]string, len(refs))
	ids := make([]string, len(refs))
	for i, r := range refs {
		paths[i], kinds[i], ids[i] = r.Path, r.ToKind, r.ToID
	}
	return m.inTx(ctx, func(q querier) error {
		b := &pgx.Batch{}
		b.Queue(`DELETE FROM manifest_references WHERE from_kind = $1 AND from_id = $2`, fromKind, fromID)
		b.Queue(`
			INSERT INTO manifest_references (from_kind, from_id, path, to_kind, to_id)
			SELECT $1, $2, p, k, i FROM unnest($3::text[], $4::text[], $5::text[]) AS u(p, k, i)
			ON CONFLICT (from_kind, from_id, path) DO UPDATE SET to_kind = excluded.to_kind, to_id = excluded.to_id`,
			fromKind, fromID, paths, kinds, ids)
		if err := q.SendBatch(ctx, b).Close(); err != nil {
			return fmt.Errorf("index references from %s/%s: %w", fromKind, fromID, err)
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
	rows, err := m.q.Query(ctx, summarySQL+`
		WHERE (m.kind, m.id) IN (SELECT from_kind, from_id FROM manifest_references WHERE to_kind = $1 AND to_id = $2)
		ORDER BY m.kind, m.id`, toKind, toID)
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
