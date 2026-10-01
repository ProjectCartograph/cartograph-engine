package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ProjectCartograph/cartograph-engine/internal/store"
)

// DocStore is the Postgres adapter for store.DocStore: one row per
// document holding its snapshot, and one row per chunk appended since.
type DocStore struct {
	pool *pgxpool.Pool
}

var _ store.DocStore = (*DocStore)(nil)

// NewDocStore returns a DocStore over a pool Open returned.
func NewDocStore(pool *pgxpool.Pool) *DocStore {
	return &DocStore{pool: pool}
}

// Create inserts the document unless the manifest has one. When two
// replicas race, the unique key on the manifest makes the second insert
// wait for the first and then do nothing; the second then reads the
// winner's id, which its own read can see once the first has committed.
func (d *DocStore) Create(ctx context.Context, kind, id, docID string, snapshot []byte) (string, bool, error) {
	if snapshot == nil {
		snapshot = []byte{}
	}
	tag, err := d.pool.Exec(ctx, `
		INSERT INTO documents (doc_id, kind, id, snapshot) VALUES ($1, $2, $3, $4)
		ON CONFLICT (kind, id) DO NOTHING`, docID, kind, id, snapshot)
	if err != nil {
		return "", false, fmt.Errorf("create document for %s/%s: %w", kind, id, err)
	}
	if tag.RowsAffected() == 1 {
		return docID, true, nil
	}
	existing, err := d.DocumentFor(ctx, kind, id)
	if err != nil {
		return "", false, fmt.Errorf("create document for %s/%s: %w", kind, id, err)
	}
	return existing, false, nil
}

// DocumentFor returns the id of a manifest's document.
func (d *DocStore) DocumentFor(ctx context.Context, kind, id string) (string, error) {
	var docID string
	err := d.pool.QueryRow(ctx, `SELECT doc_id FROM documents WHERE kind = $1 AND id = $2`, kind, id).Scan(&docID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", store.ErrNoDocument
	}
	if err != nil {
		return "", fmt.Errorf("document for %s/%s: %w", kind, id, err)
	}
	return docID, nil
}

// ManifestFor returns the manifest a document belongs to.
func (d *DocStore) ManifestFor(ctx context.Context, docID string) (string, string, error) {
	var kind, id string
	err := d.pool.QueryRow(ctx, `SELECT kind, id FROM documents WHERE doc_id = $1`, docID).Scan(&kind, &id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", store.ErrNoDocument
	}
	if err != nil {
		return "", "", fmt.Errorf("manifest for document %s: %w", docID, err)
	}
	return kind, id, nil
}

// Append takes the next sequence number under the document row's lock
// and stores the chunk in the same transaction, so concurrent appenders
// on any replica get distinct, rising numbers, and a number is never
// handed out for a chunk that was not stored.
func (d *DocStore) Append(ctx context.Context, docID string, data []byte) (int64, error) {
	if data == nil {
		data = []byte{}
	}
	var seq int64
	err := pgx.BeginFunc(ctx, d.pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `UPDATE documents SET next_seq = next_seq + 1 WHERE doc_id = $1 RETURNING next_seq`, docID).Scan(&seq)
		if errors.Is(err, pgx.ErrNoRows) {
			return store.ErrNoDocument
		}
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO document_chunks (doc_id, seq, data) VALUES ($1, $2, $3)`, docID, seq, data)
		return err
	})
	if errors.Is(err, store.ErrNoDocument) {
		return 0, err
	}
	if err != nil {
		return 0, fmt.Errorf("append to document %s: %w", docID, err)
	}
	return seq, nil
}

// Load reads the snapshot and the chunks in one repeatable-read
// transaction, so a compaction committing in between cannot leave it
// with a snapshot that lacks chunks it no longer sees.
func (d *DocStore) Load(ctx context.Context, docID string) ([]byte, []store.Chunk, error) {
	var snapshot []byte
	var chunks []store.Chunk
	err := pgx.BeginTxFunc(ctx, d.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `SELECT snapshot FROM documents WHERE doc_id = $1`, docID).Scan(&snapshot)
		if errors.Is(err, pgx.ErrNoRows) {
			return store.ErrNoDocument
		}
		if err != nil {
			return err
		}
		chunks, err = since(ctx, tx, docID, 0)
		return err
	})
	if errors.Is(err, store.ErrNoDocument) {
		return nil, nil, err
	}
	if err != nil {
		return nil, nil, fmt.Errorf("load document %s: %w", docID, err)
	}
	return snapshot, chunks, nil
}

// Since returns the chunks after a sequence number.
func (d *DocStore) Since(ctx context.Context, docID string, after int64) ([]store.Chunk, error) {
	var chunks []store.Chunk
	err := pgx.BeginTxFunc(ctx, d.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM documents WHERE doc_id = $1)`, docID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return store.ErrNoDocument
		}
		var err error
		chunks, err = since(ctx, tx, docID, after)
		return err
	})
	if errors.Is(err, store.ErrNoDocument) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("read document %s since %d: %w", docID, after, err)
	}
	return chunks, nil
}

func since(ctx context.Context, q querier, docID string, after int64) ([]store.Chunk, error) {
	rows, err := q.Query(ctx, `SELECT seq, data FROM document_chunks WHERE doc_id = $1 AND seq > $2 ORDER BY seq`, docID, after)
	if err != nil {
		return nil, err
	}
	chunks, err := pgx.CollectRows(rows, pgx.RowToStructByPos[store.Chunk])
	if err != nil {
		return nil, err
	}
	if chunks == nil {
		chunks = []store.Chunk{}
	}
	return chunks, nil
}

// Compact replaces the snapshot and drops the chunks it includes, under
// the document row's lock. Chunks after upTo are untouched, whoever
// appends them meanwhile. A compaction older than the one already
// stored changes nothing: its snapshot lacks chunks that are gone.
func (d *DocStore) Compact(ctx context.Context, docID string, snapshot []byte, upTo int64) error {
	if snapshot == nil {
		snapshot = []byte{}
	}
	err := pgx.BeginFunc(ctx, d.pool, func(tx pgx.Tx) error {
		var compacted int64
		err := tx.QueryRow(ctx, `SELECT compacted_seq FROM documents WHERE doc_id = $1 FOR UPDATE`, docID).Scan(&compacted)
		if errors.Is(err, pgx.ErrNoRows) {
			return store.ErrNoDocument
		}
		if err != nil {
			return err
		}
		if upTo < compacted {
			return nil
		}
		if _, err := tx.Exec(ctx, `UPDATE documents SET snapshot = $2, compacted_seq = $3 WHERE doc_id = $1`, docID, snapshot, upTo); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `DELETE FROM document_chunks WHERE doc_id = $1 AND seq <= $2`, docID, upTo)
		return err
	})
	if errors.Is(err, store.ErrNoDocument) {
		return err
	}
	if err != nil {
		return fmt.Errorf("compact document %s: %w", docID, err)
	}
	return nil
}
