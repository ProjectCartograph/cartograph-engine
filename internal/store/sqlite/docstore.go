package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ProjectCartograph/cartograph-engine/internal/store"
)

// DocStore is the SQLite adapter for store.DocStore, in the same
// database as the rest of the index. The database has one connection,
// so every transaction here runs alone, which is what makes Create and
// Append atomic.
type DocStore struct {
	db *sql.DB
}

var _ store.DocStore = (*DocStore)(nil)

// NewDocStore returns a DocStore over a database Open returned.
func NewDocStore(db *sql.DB) *DocStore {
	return &DocStore{db: db}
}

func (d *DocStore) Create(ctx context.Context, kind, id, docID string, snapshot []byte) (string, bool, error) {
	if snapshot == nil {
		snapshot = []byte{}
	}
	res, err := d.db.ExecContext(ctx, `
		INSERT INTO documents (doc_id, kind, id, snapshot) VALUES (?, ?, ?, ?)
		ON CONFLICT (kind, id) DO NOTHING`, docID, kind, id, snapshot)
	if err != nil {
		return "", false, fmt.Errorf("create document for %s/%s: %w", kind, id, err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 1 {
		return docID, true, nil
	}
	existing, err := d.DocumentFor(ctx, kind, id)
	if err != nil {
		return "", false, fmt.Errorf("create document for %s/%s: %w", kind, id, err)
	}
	return existing, false, nil
}

func (d *DocStore) DocumentFor(ctx context.Context, kind, id string) (string, error) {
	var docID string
	err := d.db.QueryRowContext(ctx, `SELECT doc_id FROM documents WHERE kind = ? AND id = ?`, kind, id).Scan(&docID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", store.ErrNoDocument
	}
	if err != nil {
		return "", fmt.Errorf("document for %s/%s: %w", kind, id, err)
	}
	return docID, nil
}

func (d *DocStore) ManifestFor(ctx context.Context, docID string) (string, string, error) {
	var kind, id string
	err := d.db.QueryRowContext(ctx, `SELECT kind, id FROM documents WHERE doc_id = ?`, docID).Scan(&kind, &id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", store.ErrNoDocument
	}
	if err != nil {
		return "", "", fmt.Errorf("manifest for document %s: %w", docID, err)
	}
	return kind, id, nil
}

func (d *DocStore) Append(ctx context.Context, docID string, data []byte) (int64, error) {
	if data == nil {
		data = []byte{}
	}
	var seq int64
	err := d.inTx(ctx, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, `UPDATE documents SET next_seq = next_seq + 1 WHERE doc_id = ? RETURNING next_seq`, docID).Scan(&seq)
		if errors.Is(err, sql.ErrNoRows) {
			return store.ErrNoDocument
		}
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO document_chunks (doc_id, seq, data) VALUES (?, ?, ?)`, docID, seq, data)
		return err
	})
	if err != nil {
		return 0, wrapDoc("append to document", docID, err)
	}
	return seq, nil
}

func (d *DocStore) Load(ctx context.Context, docID string) ([]byte, []store.Chunk, error) {
	var snapshot []byte
	var chunks []store.Chunk
	err := d.inTx(ctx, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, `SELECT snapshot FROM documents WHERE doc_id = ?`, docID).Scan(&snapshot)
		if errors.Is(err, sql.ErrNoRows) {
			return store.ErrNoDocument
		}
		if err != nil {
			return err
		}
		chunks, err = chunksSince(ctx, tx, docID, 0)
		return err
	})
	if err != nil {
		return nil, nil, wrapDoc("load document", docID, err)
	}
	return snapshot, chunks, nil
}

func (d *DocStore) Since(ctx context.Context, docID string, after int64) ([]store.Chunk, error) {
	var chunks []store.Chunk
	err := d.inTx(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM documents WHERE doc_id = ?`, docID).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return store.ErrNoDocument
		}
		var err error
		chunks, err = chunksSince(ctx, tx, docID, after)
		return err
	})
	if err != nil {
		return nil, wrapDoc("read document", docID, err)
	}
	return chunks, nil
}

// Compact ignores a compaction older than the stored one, whose
// snapshot lacks chunks that are already gone.
func (d *DocStore) Compact(ctx context.Context, docID string, snapshot []byte, upTo int64) error {
	if snapshot == nil {
		snapshot = []byte{}
	}
	err := d.inTx(ctx, func(tx *sql.Tx) error {
		var compacted int64
		err := tx.QueryRowContext(ctx, `SELECT compacted_seq FROM documents WHERE doc_id = ?`, docID).Scan(&compacted)
		if errors.Is(err, sql.ErrNoRows) {
			return store.ErrNoDocument
		}
		if err != nil {
			return err
		}
		if upTo < compacted {
			return nil
		}
		if _, err := tx.ExecContext(ctx, `UPDATE documents SET snapshot = ?, compacted_seq = ? WHERE doc_id = ?`, snapshot, upTo, docID); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM document_chunks WHERE doc_id = ? AND seq <= ?`, docID, upTo)
		return err
	})
	if err != nil {
		return wrapDoc("compact document", docID, err)
	}
	return nil
}

func (d *DocStore) inTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func chunksSince(ctx context.Context, tx *sql.Tx, docID string, after int64) ([]store.Chunk, error) {
	rows, err := tx.QueryContext(ctx, `SELECT seq, data FROM document_chunks WHERE doc_id = ? AND seq > ? ORDER BY seq`, docID, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []store.Chunk{}
	for rows.Next() {
		var c store.Chunk
		if err := rows.Scan(&c.Seq, &c.Data); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// wrapDoc keeps ErrNoDocument bare, so a caller can test for it, and
// says what failed otherwise.
func wrapDoc(op, docID string, err error) error {
	if errors.Is(err, store.ErrNoDocument) {
		return err
	}
	return fmt.Errorf("%s %s: %w", op, docID, err)
}
