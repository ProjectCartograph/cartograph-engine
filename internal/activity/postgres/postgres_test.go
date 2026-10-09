//go:build integration

package postgres_test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/activity"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/activity/conformance"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/activity/postgres"
)

// Each run of the suite gets a table of its own, in a schema of its own,
// so the runs never read each other's acts. CARTOGRAPH_TEST_POSTGRES
// names the database; `just test-postgres` sets it.
func TestConformance(t *testing.T) {
	u := os.Getenv("CARTOGRAPH_TEST_POSTGRES")
	if u == "" {
		t.Skip("CARTOGRAPH_TEST_POSTGRES is unset; `just test-postgres` runs these against a throwaway server")
	}
	n := 0
	conformance.Run(t, func(t *testing.T) (activity.Recorder, activity.Reader) {
		ctx := context.Background()
		n++
		schema := "activity_test_" + string(rune('a'+n))
		cfg, err := pgxpool.ParseConfig(u)
		if err != nil {
			t.Fatal(err)
		}
		cfg.ConnConfig.RuntimeParams["search_path"] = schema
		admin, err := pgxpool.New(ctx, u)
		if err != nil {
			t.Fatal(err)
		}
		defer admin.Close()
		if _, err := admin.Exec(ctx, `DROP SCHEMA IF EXISTS `+schema+` CASCADE; CREATE SCHEMA `+schema); err != nil {
			t.Fatal(err)
		}
		pool, err := pgxpool.NewWithConfig(ctx, cfg)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(pool.Close)
		s, err := postgres.Open(ctx, pool)
		if err != nil {
			t.Fatal(err)
		}
		return s, s
	})
}
