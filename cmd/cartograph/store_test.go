package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/fanout"
)

func TestComposeAVault(t *testing.T) {
	ctx := context.Background()
	c, err := compose(ctx, storeOptions{Target: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.FanoutAdapter != "memory" {
		t.Fatalf("a vault fans out in memory, got %q", c.FanoutAdapter)
	}
	if _, created, err := c.Docs.Create(ctx, "Goal", "g1", "doc-1", nil); err != nil || !created {
		t.Fatalf("the vault's document store: created=%v err=%v", created, err)
	}
	roundTrip(t, c.Fanout, c.Fanout)

	if _, err := compose(ctx, storeOptions{Target: t.TempDir(), Fanout: "postgres"}); err == nil {
		t.Fatal("fan-out over Postgres needs a Postgres store")
	}
}

// TestComposeTwoReplicasOnPostgres composes twice over one database, as
// two replicas would, and checks that what one writes the other reads
// and what one publishes the other hears. It needs
// CARTOGRAPH_TEST_POSTGRES, which `just test-postgres` sets.
func TestComposeTwoReplicasOnPostgres(t *testing.T) {
	base := os.Getenv("CARTOGRAPH_TEST_POSTGRES")
	if base == "" {
		t.Skip("CARTOGRAPH_TEST_POSTGRES is unset; `just test-postgres` runs this against a throwaway server")
	}
	ctx := context.Background()
	target := schemaURL(t, base)

	var replicas []*composition
	for i := 0; i < 2; i++ {
		c, err := compose(ctx, storeOptions{Target: target})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.Close() })
		if c.FanoutAdapter != "postgres" {
			t.Fatalf("a Postgres store fans out over Postgres by default, got %q", c.FanoutAdapter)
		}
		replicas = append(replicas, c)
	}
	a, b := replicas[0], replicas[1]

	seeded, err := a.Engine.SeedStandardUnits(ctx)
	if err != nil || len(seeded) == 0 {
		t.Fatalf("first replica seeds units: %v, %v", seeded, err)
	}
	again, err := b.Engine.SeedStandardUnits(ctx)
	if err != nil || len(again) != 0 {
		t.Fatalf("second replica sees them and seeds nothing: %v, %v", again, err)
	}

	docID, _, err := a.Docs.Create(ctx, "Goal", "g1", "doc-1", []byte("snap"))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := b.Docs.DocumentFor(ctx, "Goal", "g1"); err != nil || got != docID {
		t.Fatalf("second replica's document for Goal/g1: %q, %v", got, err)
	}
	roundTrip(t, a.Fanout, b.Fanout)
}

func roundTrip(t *testing.T, from, to fanout.Bus) {
	t.Helper()
	ctx := context.Background()
	s, err := to.Subscribe(ctx, "doc-1")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := from.Publish(ctx, "doc-1", []byte("changed")); err != nil {
		t.Fatal(err)
	}
	select {
	case m := <-s.C():
		if string(m) != "changed" {
			t.Fatalf("got %q", m)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no message")
	}
}

// schemaURL makes a schema for this test alone and returns base with its
// connections pointed at it.
func schemaURL(t *testing.T, base string) string {
	t.Helper()
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
		c.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
	})
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String()
}
