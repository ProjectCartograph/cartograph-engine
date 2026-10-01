package postgres_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/fanout"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/fanout/conformance"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/fanout/postgres"
)

// newBus opens a Bus with a pool of its own on the test database, so
// two of them are two replicas. CARTOGRAPH_TEST_POSTGRES names the
// database; `just test-postgres` sets it, and without it the test is
// skipped.
func newBus(t *testing.T) fanout.Bus {
	t.Helper()
	u := os.Getenv("CARTOGRAPH_TEST_POSTGRES")
	if u == "" {
		t.Skip("CARTOGRAPH_TEST_POSTGRES is unset; `just test-postgres` runs these against a throwaway server")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, u)
	if err != nil {
		t.Fatal(err)
	}
	b, err := postgres.New(ctx, pool)
	if err != nil {
		pool.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		b.Close()
		pool.Close()
	})
	return b
}

func TestConformance(t *testing.T) {
	if os.Getenv("CARTOGRAPH_TEST_POSTGRES") == "" {
		t.Skip("CARTOGRAPH_TEST_POSTGRES is unset; `just test-postgres` runs these against a throwaway server")
	}
	conformance.Run(t, newBus)
}

// TestListenerReconnects ends the listening connection from the server
// side, as a database restart or a network drop would, and checks that
// messages flow again once the Bus has reconnected.
func TestListenerReconnects(t *testing.T) {
	b := newBus(t)
	ctx := context.Background()
	s, err := b.Subscribe(ctx, "reconnect")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	pool, err := pgxpool.New(ctx, os.Getenv("CARTOGRAPH_TEST_POSTGRES"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE query = 'LISTEN `+postgres.Channel+`' AND pid <> pg_backend_pid()`); err != nil {
		t.Fatal(err)
	}
	// Publish until one arrives: those sent before the listener is back
	// are lost, which the port allows.
	deadline := time.After(10 * time.Second)
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case m := <-s.C():
			if string(m) != "again" {
				t.Fatalf("got %q", m)
			}
			return
		case <-tick.C:
			if err := b.Publish(ctx, "reconnect", []byte("again")); err != nil {
				t.Fatal(err)
			}
		case <-deadline:
			t.Fatal("no message after the listening connection was terminated")
		}
	}
}
