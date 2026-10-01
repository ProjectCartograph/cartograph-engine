package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

// BundleStore keeps handoff bundles as rows of bytes. A bundle is a few
// rendered documents, small enough that a database is a fine home; an
// object store is the adapter to write when that stops being true.
type BundleStore struct {
	pool *pgxpool.Pool
}

var _ store.BundleStore = (*BundleStore)(nil)

// NewBundleStore returns a BundleStore over a pool Open returned.
func NewBundleStore(pool *pgxpool.Pool) *BundleStore {
	return &BundleStore{pool: pool}
}

// PutBundle stores files under "handoff/<project>/v<n>", the location
// the other adapters use, replacing whatever was there, in one
// transaction.
func (b *BundleStore) PutBundle(ctx context.Context, projectID string, version int, files map[string][]byte) (store.Bundle, error) {
	location := fmt.Sprintf("handoff/%s/v%d", projectID, version)
	paths := make(map[string]string, len(files))
	on := stamp(time.Time{})
	err := pgx.BeginFunc(ctx, b.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM bundle_files WHERE location = $1`, location); err != nil {
			return err
		}
		for name, content := range files {
			if content == nil {
				content = []byte{}
			}
			if _, err := tx.Exec(ctx, `INSERT INTO bundle_files (location, name, content, on_ts) VALUES ($1, $2, $3, $4)`,
				location, name, content, on); err != nil {
				return err
			}
			paths[name] = location + "/" + name
		}
		return nil
	})
	if err != nil {
		return store.Bundle{}, fmt.Errorf("put bundle %s: %w", location, err)
	}
	return store.Bundle{Location: location, Paths: paths}, nil
}

// File returns one file of a stored bundle, by the location PutBundle
// returned and the file's name.
func (b *BundleStore) File(ctx context.Context, location, name string) ([]byte, bool, error) {
	var content []byte
	err := b.pool.QueryRow(ctx, `SELECT content FROM bundle_files WHERE location = $1 AND name = $2`, location, name).Scan(&content)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read bundle file %s/%s: %w", location, name, err)
	}
	return content, true, nil
}
