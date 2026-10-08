//go:build integration

package postgres_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store/conformance"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store/postgres"
)

// testURL is the database the tests run against, from
// CARTOGRAPH_TEST_POSTGRES. `just test-postgres` starts a throwaway
// server and sets it; without it, every test here is skipped, which
// keeps `just test` free of a database.
func testURL(t *testing.T) string {
	t.Helper()
	u := os.Getenv("CARTOGRAPH_TEST_POSTGRES")
	if u == "" {
		t.Skip("CARTOGRAPH_TEST_POSTGRES is unset; `just test-postgres` runs these against a throwaway server")
	}
	return u
}

// freshSchema creates a schema of its own for one test, dropped when
// the test ends, and returns a URL whose connections use it. Tests in
// other packages share the server, so nothing here touches the public
// schema.
func freshSchema(t *testing.T) string {
	t.Helper()
	base := testURL(t)
	ctx := context.Background()
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	schema := "test_" + hex.EncodeToString(b)
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c, err := pgx.Connect(context.Background(), base)
		if err != nil {
			t.Error(err)
			return
		}
		defer c.Close(context.Background())
		if _, err := c.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error(err)
		}
	})
	return withSearchPath(t, base, schema)
}

func withSearchPath(t *testing.T, base, schema string) string {
	t.Helper()
	if !strings.Contains(base, "://") {
		return base + " search_path=" + schema
	}
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String()
}

func openPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := postgres.Open(context.Background(), freshSchema(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestManifestStoreConformance(t *testing.T) {
	testURL(t)
	conformance.RunManifestStore(t, func(t *testing.T) store.ManifestStore {
		return postgres.NewManifestStore(openPool(t))
	})
}

func TestOperationalStoreConformance(t *testing.T) {
	testURL(t)
	conformance.RunOperationalStore(t, func(t *testing.T) store.OperationalStore {
		return postgres.NewOperationalStore(openPool(t))
	})
}

func TestDocStoreConformance(t *testing.T) {
	testURL(t)
	conformance.RunDocStore(t, func(t *testing.T) store.DocStore {
		return postgres.NewDocStore(openPool(t))
	})
}

func TestAccessStoreConformance(t *testing.T) {
	testURL(t)
	conformance.RunAccessStore(t, func(t *testing.T) store.AccessStore {
		return postgres.NewAccessStore(openPool(t))
	})
}

func TestMigrationsAreIdempotentAndSafeToRace(t *testing.T) {
	u := freshSchema(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make(chan error, 3)
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pool, err := postgres.Open(ctx, u)
			if err != nil {
				errs <- err
				return
			}
			pool.Close()
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("replicas opening a fresh database at once: %v", err)
	}
	pool, err := postgres.Open(ctx, u)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer pool.Close()
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	// Each migration once, however many replicas raced to apply it.
	files, err := filepath.Glob("migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	if n != len(files) {
		t.Fatalf("%d migrations recorded, want %d", n, len(files))
	}
}

func TestVersionsAreImmutable(t *testing.T) {
	pool := openPool(t)
	ctx := context.Background()
	m := postgres.NewManifestStore(pool)
	if err := m.PutVersion(ctx, store.Version{Kind: "Goal", ID: "g1", Number: 1, YAML: []byte("metadata:\n  id: g1\n  name: G\n")}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE manifest_versions SET yaml = 'x'`); err == nil {
		t.Fatal("a version was updated")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM manifest_versions`); err == nil {
		t.Fatal("a version was deleted")
	}
}

func TestTwoReplicasRacingForOneVersionNumber(t *testing.T) {
	u := freshSchema(t)
	ctx := context.Background()
	var stores []*postgres.ManifestStore
	for i := 0; i < 2; i++ {
		pool, err := postgres.Open(ctx, u)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(pool.Close)
		stores = append(stores, postgres.NewManifestStore(pool))
	}
	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = stores[i%2].PutVersion(ctx, store.Version{Kind: "Goal", ID: "g1", Number: 1, YAML: []byte("metadata:\n  id: g1\n  name: G\n")})
		}(i)
	}
	wg.Wait()
	ok := 0
	for _, err := range errs {
		if err == nil {
			ok++
		}
	}
	if ok != 1 {
		t.Fatalf("%d writers got version 1, want exactly 1", ok)
	}
}

func TestListAllVersionsPages(t *testing.T) {
	m := postgres.NewManifestStore(openPool(t))
	ctx := context.Background()
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, id := range []string{"a", "b", "c", "d", "e"} {
		if err := m.PutVersion(ctx, store.Version{Kind: "Goal", ID: id, Number: 1, YAML: []byte("metadata:\n  id: " + id + "\n"), On: at.Add(time.Duration(i%2) * time.Second)}); err != nil {
			t.Fatal(err)
		}
	}
	var seen []string
	cursor := ""
	for {
		page, next, err := m.ListAllVersions(ctx, 2, cursor)
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range page {
			seen = append(seen, v.ID)
		}
		if next == "" {
			break
		}
		cursor = next
	}
	// Newest first, then by kind and id: b and d are a second later.
	if got := strings.Join(seen, ","); got != "b,d,a,c,e" {
		t.Fatalf("pages gave %s", got)
	}
}

func TestBundleStore(t *testing.T) {
	ctx := context.Background()
	b := postgres.NewBundleStore(openPool(t))

	pdf := []byte{'%', 'P', 'D', 'F', 0, 0xff}
	got, err := b.PutBundle(ctx, "p1", 2, map[string][]byte{"charter.html": []byte("<html>"), "charter.pdf": pdf})
	if err != nil {
		t.Fatal(err)
	}
	if got.Location != "handoff/p1/v2" || got.Paths["charter.pdf"] != "handoff/p1/v2/charter.pdf" {
		t.Fatalf("got %+v", got)
	}
	content, found, err := b.File(ctx, got.Location, "charter.pdf")
	if err != nil || !found || string(content) != string(pdf) {
		t.Fatalf("read back %q found=%v err=%v", content, found, err)
	}

	// All or nothing: a file the database refuses (a name with a zero
	// byte is not text) leaves the bundle as it was.
	if _, err := b.PutBundle(ctx, "p1", 2, map[string][]byte{"charter.html": []byte("<new>"), "bad\x00name": []byte("x")}); err == nil {
		t.Fatal("expected a refusal")
	}
	content, found, err = b.File(ctx, got.Location, "charter.html")
	if err != nil || !found || string(content) != "<html>" {
		t.Fatalf("a failed put changed the bundle: %q found=%v err=%v", content, found, err)
	}

	// Putting the same version again replaces it whole.
	if _, err := b.PutBundle(ctx, "p1", 2, map[string][]byte{"charter.html": []byte("<again>")}); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := b.File(ctx, got.Location, "charter.pdf"); found {
		t.Fatal("a file from the replaced bundle is still there")
	}
	if _, found, _ := b.File(ctx, "handoff/p1/v9", "charter.html"); found {
		t.Fatal("found a file that was never put")
	}
}
