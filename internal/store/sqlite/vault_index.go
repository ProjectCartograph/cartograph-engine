package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

var _ store.VaultIndex = (*VaultIndex)(nil)

// VaultIndex is the SQLite adapter for store.VaultIndex: everything the
// vault keeps beside its files, in one database. It wraps a ManifestStore,
// an ApplyJournal, an OperationalStore and a DocStore over the same
// *sql.DB, and adds
// the file-hash and meta tables (vault_files, vault_meta) that used to be
// raw SQL inside the vault package itself.
type VaultIndex struct {
	*ManifestStore
	db      *sql.DB
	journal *ApplyJournal
	ops     *OperationalStore
	docs    *DocStore
	access  *AccessStore
}

// NewVaultIndex builds a VaultIndex over an already-open, already-migrated
// database.
func NewVaultIndex(db *sql.DB) *VaultIndex {
	return &VaultIndex{
		ManifestStore: NewManifestStore(db),
		db:            db,
		journal:       NewApplyJournal(db),
		ops:           NewOperationalStore(db),
		docs:          NewDocStore(db),
		access:        NewAccessStore(db),
	}
}

// OpenVaultIndexIn opens the index a vault keeps in dir, as
// dir/index.sqlite. Its type is vault.Options.OpenIndex, so the
// composition root hands it to a vault without either adapter importing
// the other.
func OpenVaultIndexIn(ctx context.Context, dir string) (store.VaultIndex, error) {
	return OpenVaultIndex(ctx, filepath.Join(dir, "index.sqlite"))
}

// OpenVaultIndex opens (creating if needed) a SQLite database at path,
// applies migrations, and returns a VaultIndex over it.
func OpenVaultIndex(ctx context.Context, path string) (*VaultIndex, error) {
	db, err := Open(ctx, path)
	if err != nil {
		return nil, err
	}
	return NewVaultIndex(db), nil
}

// Journal returns the apply journal over the same database.
func (v *VaultIndex) Journal() store.ApplyJournal { return v.journal }

// Operational returns the project-state store over the same database.
func (v *VaultIndex) Operational() store.OperationalStore { return v.ops }

// Docs returns the shared drafts' documents over the same database.
func (v *VaultIndex) Docs() store.DocStore { return v.docs }

// Access returns the access list over the same database.
func (v *VaultIndex) Access() store.AccessStore { return v.access }

// GetFileHash returns the SHA-256 last recorded for kind/id, if any.
func (v *VaultIndex) GetFileHash(ctx context.Context, kind, id string) (string, bool, error) {
	var hash string
	err := v.db.QueryRowContext(ctx, `
		SELECT sha256 FROM vault_files WHERE kind = ? AND id = ?`, kind, id).Scan(&hash)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return hash, true, nil
}

// PutFileHash upserts a file's hash and mtime.
func (v *VaultIndex) PutFileHash(ctx context.Context, kind, id, sha256, mtime string) error {
	_, err := v.db.ExecContext(ctx, `
		INSERT INTO vault_files (kind, id, sha256, mtime)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(kind, id) DO UPDATE SET sha256 = excluded.sha256, mtime = excluded.mtime`,
		kind, id, sha256, mtime)
	return err
}

// DeleteFileHash removes a file's recorded hash, if any.
func (v *VaultIndex) DeleteFileHash(ctx context.Context, kind, id string) error {
	_, err := v.db.ExecContext(ctx, `DELETE FROM vault_files WHERE kind = ? AND id = ?`, kind, id)
	return err
}

// GetMeta returns one recorded fact about the index itself (for example
// the vault.yaml hash recorded at the last open, under key
// "vault.yaml.hash"). Both that key and the rebuild-time key the vault
// used to keep in a separate index_meta table live in vault_meta now:
// nothing but this method ever reads or writes either.
func (v *VaultIndex) GetMeta(ctx context.Context, key string) (string, bool, error) {
	var value string
	err := v.db.QueryRowContext(ctx, `SELECT value FROM vault_meta WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

// PutMeta upserts one fact about the index itself.
func (v *VaultIndex) PutMeta(ctx context.Context, key, value string) error {
	_, err := v.db.ExecContext(ctx, `
		INSERT INTO vault_meta (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// Close closes the underlying database.
func (v *VaultIndex) Close() error {
	return v.db.Close()
}
