package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store/manifestmeta"
)

// keyframeEvery bounds how many patches a historical version is rebuilt
// from: a version numbered 1, 17, 33, ... keeps its whole document.
const keyframeEvery = 16

// historyCols are the columns a historical version is rebuilt from.
const historyCols = `kind, id, number, yaml, actor, reason, on_ts, doc, patch`

// stored is one manifest_versions row as it is kept: with its text, or
// compacted into a keyframe (doc) or a patch.
type stored struct {
	v     store.Version
	text  *string
	doc   []byte
	patch []byte
}

func scanStored(rows pgx.Rows) ([]stored, error) {
	defer rows.Close()
	out := []stored{}
	for rows.Next() {
		var s stored
		if err := rows.Scan(&s.v.Kind, &s.v.ID, &s.v.Number, &s.text, &s.v.Actor, &s.v.Reason, &s.v.On, &s.doc, &s.patch); err != nil {
			return nil, fmt.Errorf("read version: %w", err)
		}
		s.v.On = s.v.On.UTC()
		out = append(out, s)
	}
	return out, rows.Err()
}

// rebuild turns consecutive rows of one manifest, from a keyframe or a
// row with text on, into versions. A compacted version comes back with
// its document and no text; the engine writes the text with its codec.
func rebuild(rows []stored) ([]store.Version, error) {
	out := make([]store.Version, 0, len(rows))
	var prev []byte
	for _, s := range rows {
		v := s.v
		switch {
		case s.text != nil:
			v.YAML = []byte(*s.text)
			prev = s.doc
			if prev == nil {
				prev = manifestmeta.Doc(v.YAML)
			}
		case s.doc != nil:
			v.Doc, prev = s.doc, s.doc
		case s.patch != nil:
			if prev == nil {
				return nil, fmt.Errorf("version %d of %s/%s: a patch with nothing before it", v.Number, v.Kind, v.ID)
			}
			doc, err := applyPatch(prev, s.patch)
			if err != nil {
				return nil, fmt.Errorf("version %d of %s/%s: %w", v.Number, v.Kind, v.ID, err)
			}
			v.Doc, prev = doc, doc
		default:
			return nil, fmt.Errorf("version %d of %s/%s has neither text nor document", v.Number, v.Kind, v.ID)
		}
		out = append(out, v)
	}
	return out, nil
}

// GetVersion returns one version, rebuilt from the nearest keyframe when
// it is stored as a patch: at most keyframeEvery-1 patches.
func (m *ManifestStore) GetVersion(ctx context.Context, kind, id string, number int) (store.Version, bool, error) {
	rows, err := m.q.Query(ctx, `
		SELECT `+historyCols+` FROM manifest_versions
		WHERE kind = $1 AND id = $2 AND number <= $3 AND number >= (
			SELECT coalesce(max(number), 1) FROM manifest_versions
			WHERE kind = $1 AND id = $2 AND number <= $3 AND (yaml IS NOT NULL OR doc IS NOT NULL))
		ORDER BY number`, kind, id, number)
	if err != nil {
		return store.Version{}, false, fmt.Errorf("get version %s/%s %d: %w", kind, id, number, err)
	}
	raw, err := scanStored(rows)
	if err != nil {
		return store.Version{}, false, err
	}
	if len(raw) == 0 || raw[len(raw)-1].v.Number != number {
		return store.Version{}, false, nil
	}
	vs, err := rebuild(raw)
	if err != nil {
		return store.Version{}, false, err
	}
	return vs[len(vs)-1], true, nil
}

// ListVersions returns every version of a manifest, oldest first,
// rebuilt in one pass.
func (m *ManifestStore) ListVersions(ctx context.Context, kind, id string) ([]store.Version, error) {
	rows, err := m.q.Query(ctx, `SELECT `+historyCols+` FROM manifest_versions WHERE kind = $1 AND id = $2 ORDER BY number`, kind, id)
	if err != nil {
		return nil, fmt.Errorf("list versions %s/%s: %w", kind, id, err)
	}
	raw, err := scanStored(rows)
	if err != nil {
		return nil, err
	}
	return rebuild(raw)
}

// CompactHistory compacts the history of up to limit manifests: every
// version saved before before, other than a manifest's latest, keeps a
// keyframe or a patch and loses its text. It returns how many versions
// it compacted. One replica compacts at a time; another that asks
// meanwhile compacts nothing.
func (m *ManifestStore) CompactHistory(ctx context.Context, before time.Time, limit int) (int, error) {
	if m.pool == nil {
		return 0, fmt.Errorf("compact history: not inside a transaction")
	}
	conn, err := m.pool.Acquire(ctx)
	if err != nil {
		return 0, fmt.Errorf("compact history: %w", err)
	}
	defer conn.Release()
	var mine bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, int64(compactLock)).Scan(&mine); err != nil || !mine {
		return 0, err
	}
	defer conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, int64(compactLock))

	rows, err := conn.Query(ctx, `
		SELECT DISTINCT v.kind, v.id FROM manifest_versions v
		JOIN manifests m ON m.kind = v.kind AND m.id = v.id
		WHERE v.yaml IS NOT NULL AND v.on_ts < $1 AND v.number < m.version
		  AND NOT EXISTS (SELECT 1 FROM manifest_versions s
		                  WHERE s.kind = v.kind AND s.id = v.id AND s.doc IS NULL AND s.patch IS NULL)
		LIMIT $2`, before, limit)
	if err != nil {
		return 0, fmt.Errorf("compact history: %w", err)
	}
	type key struct{ kind, id string }
	var todo []key
	for rows.Next() {
		var k key
		if err := rows.Scan(&k.kind, &k.id); err != nil {
			rows.Close()
			return 0, err
		}
		todo = append(todo, k)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	total := 0
	for _, k := range todo {
		n, err := compactOne(ctx, conn, k.kind, k.id, before)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

// compactLock is the advisory lock key a replica holds while compacting.
const compactLock = 0x636172746f636d70

// compactOne compacts one manifest's history, in one transaction.
func compactOne(ctx context.Context, conn interface {
	Begin(context.Context) (pgx.Tx, error)
}, kind, id string, before time.Time) (int, error) {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var latest int
	// The manifest's row, locked: a version saved meanwhile waits.
	if err := tx.QueryRow(ctx, `SELECT version FROM manifests WHERE kind = $1 AND id = $2 FOR UPDATE`, kind, id).Scan(&latest); err != nil {
		return 0, fmt.Errorf("compact %s/%s: %w", kind, id, err)
	}
	rows, err := tx.Query(ctx, `SELECT `+historyCols+` FROM manifest_versions WHERE kind = $1 AND id = $2 ORDER BY number`, kind, id)
	if err != nil {
		return 0, err
	}
	raw, err := scanStored(rows)
	if err != nil {
		return 0, err
	}
	vs, err := rebuild(raw)
	if err != nil {
		return 0, err
	}
	b := &pgx.Batch{}
	var prev []byte
	for i, s := range raw {
		doc := vs[i].Doc
		if doc == nil {
			doc = s.doc
		}
		eligible := s.text != nil && s.v.Number < latest && s.v.On.Before(before)
		if eligible {
			keep := s.v.Number%keyframeEvery == 1 || prev == nil
			var patch []byte
			if !keep {
				if patch, err = diffJSON(prev, doc); err != nil {
					return 0, fmt.Errorf("compact %s/%s %d: %w", kind, id, s.v.Number, err)
				}
				keep = len(patch)*2 > len(doc)
			}
			if keep {
				b.Queue(`UPDATE manifest_versions SET yaml = NULL WHERE kind = $1 AND id = $2 AND number = $3`, kind, id, s.v.Number)
			} else {
				b.Queue(`UPDATE manifest_versions SET yaml = NULL, doc = NULL, patch = $4 WHERE kind = $1 AND id = $2 AND number = $3`,
					kind, id, s.v.Number, string(patch))
			}
		}
		prev = doc
	}
	n := b.Len()
	if n == 0 {
		return 0, nil
	}
	if err := tx.SendBatch(ctx, b).Close(); err != nil {
		return 0, fmt.Errorf("compact %s/%s: %w", kind, id, err)
	}
	return n, tx.Commit(ctx)
}
