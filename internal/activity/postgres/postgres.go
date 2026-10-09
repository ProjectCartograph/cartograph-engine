// Package postgres keeps the people's trace in Postgres (docs/adr/0034),
// so every replica of a stateless deployment appends to one place and
// none keeps a file of its own. It knows its port and its database,
// nothing of the record.
package postgres

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/activity"
)

//go:embed activity.sql
var schema string

// schemaLock is the advisory lock replicas take while making the table.
const schemaLock = 0x6361727461637476

// recordWithin bounds one append: a trace never holds up the request it
// records.
const recordWithin = 2 * time.Second

// Store appends acts to a table and reads them back.
type Store struct{ pool *pgxpool.Pool }

var (
	_ activity.Recorder = (*Store)(nil)
	_ activity.Reader   = (*Store)(nil)
)

// Open makes the table, one replica at a time, and returns a store over
// it.
func Open(ctx context.Context, pool *pgxpool.Pool) (*Store, error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("activity table: %w", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, int64(schemaLock)); err != nil {
		return nil, fmt.Errorf("activity table: %w", err)
	}
	defer conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, int64(schemaLock))
	if _, err := conn.Exec(ctx, schema); err != nil {
		return nil, fmt.Errorf("activity table: %w", err)
	}
	return &Store{pool: pool}, nil
}

// Record appends one act. An append that fails or takes too long is
// dropped: a trace never fails the act it records.
func (s *Store) Record(e activity.Event) {
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), recordWithin)
	defer cancel()
	_, _ = s.pool.Exec(ctx, `INSERT INTO cartograph_activity (at, person, act) VALUES ($1, $2, $3)`, e.At, e.Person, b)
}

// Read returns the acts q asks for, in the order they happened.
func (s *Store) Read(ctx context.Context, q activity.Query) ([]activity.Event, error) {
	var from, to *time.Time
	if !q.From.IsZero() {
		from = &q.From
	}
	if !q.To.IsZero() {
		to = &q.To
	}
	rows, err := s.pool.Query(ctx, `SELECT act FROM cartograph_activity
		WHERE ($1::timestamptz IS NULL OR at >= $1) AND ($2::timestamptz IS NULL OR at < $2) AND ($3 = '' OR person = $3)
		ORDER BY at, id`, from, to, q.Person)
	if err != nil {
		return nil, fmt.Errorf("read activity: %w", err)
	}
	defer rows.Close()
	var out []activity.Event
	for rows.Next() {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			return nil, err
		}
		var e activity.Event
		if err := json.Unmarshal(b, &e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
