package sqlite_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/internal/store"
	"github.com/ProjectCartograph/cartograph-engine/internal/store/conformance"
	"github.com/ProjectCartograph/cartograph-engine/internal/store/sqlite"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cartograph.db")
	db, err := sqlite.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestManifestStoreConformance(t *testing.T) {
	conformance.RunManifestStore(t, func(t *testing.T) store.ManifestStore {
		return sqlite.NewManifestStore(openTestDB(t))
	})
}

func TestOperationalStoreConformance(t *testing.T) {
	conformance.RunOperationalStore(t, func(t *testing.T) store.OperationalStore {
		return sqlite.NewOperationalStore(openTestDB(t))
	})
}

func TestVaultIndexConformance(t *testing.T) {
	conformance.RunVaultIndex(t, func(t *testing.T) store.VaultIndex {
		return sqlite.NewVaultIndex(openTestDB(t))
	})
}

func TestDocStoreConformance(t *testing.T) {
	conformance.RunDocStore(t, func(t *testing.T) store.DocStore {
		return sqlite.NewDocStore(openTestDB(t))
	})
}
