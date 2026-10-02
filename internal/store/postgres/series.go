package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

var (
	_ store.SeriesStore = (*ManifestStore)(nil)
	_ store.EventLog    = (*ManifestStore)(nil)
)

// RecordSeries appends series items, in one round trip.
func (m *ManifestStore) RecordSeries(ctx context.Context, items []store.SeriesItem) error {
	if len(items) == 0 {
		return nil
	}
	return m.inTx(ctx, func(q querier) error {
		b := &pgx.Batch{}
		for _, it := range items {
			var item any
			if it.Item != nil {
				item = string(it.Item)
			}
			b.Queue(`INSERT INTO series_items (kind, id, series, key, item, recorded_at, recorded_by, reason)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
				it.Kind, it.ID, it.Series, it.Key, item, stamp(it.RecordedAt), it.RecordedBy, it.Reason)
		}
		if err := q.SendBatch(ctx, b).Close(); err != nil {
			return fmt.Errorf("record series: %w", err)
		}
		return nil
	})
}

// SeriesAsOf merges each key's rows into its value at at: the last row
// recorded by then, through series_items_as_of.
func (m *ManifestStore) SeriesAsOf(ctx context.Context, kind, series string, ids []string, at time.Time) (map[string][]store.SeriesItem, error) {
	if at.IsZero() {
		at = time.Now()
	}
	rows, err := m.q.Query(ctx, `
		SELECT id, key, item, recorded_at, recorded_by, reason FROM (
			SELECT DISTINCT ON (id, key) id, key, item, recorded_at, recorded_by, reason FROM series_items
			WHERE kind = $1 AND series = $2 AND ($3::text[] IS NULL OR id = ANY($3)) AND recorded_at <= $4
			ORDER BY id, key, recorded_at DESC, seq DESC
		) last WHERE item IS NOT NULL ORDER BY id, key`, kind, series, ids, stamp(at))
	if err != nil {
		return nil, fmt.Errorf("series %s %s: %w", kind, series, err)
	}
	defer rows.Close()
	out := map[string][]store.SeriesItem{}
	for rows.Next() {
		it := store.SeriesItem{Kind: kind, Series: series}
		if err := rows.Scan(&it.ID, &it.Key, &it.Item, &it.RecordedAt, &it.RecordedBy, &it.Reason); err != nil {
			return nil, fmt.Errorf("series %s %s: %w", kind, series, err)
		}
		it.RecordedAt = it.RecordedAt.UTC()
		out[it.ID] = append(out[it.ID], it)
	}
	return out, rows.Err()
}

// Events returns events after a cursor, only those older than every
// transaction still open, so none can commit behind the cursor later.
func (m *ManifestStore) Events(ctx context.Context, after int64, limit int) ([]store.Event, error) {
	rows, err := m.q.Query(ctx, `
		SELECT seq, at, type, kind, id, number, detail, actor FROM events
		WHERE seq > $1 AND tx < pg_snapshot_xmin(pg_current_snapshot())
		ORDER BY seq LIMIT $2`, after, limit)
	if err != nil {
		return nil, fmt.Errorf("events: %w", err)
	}
	defer rows.Close()
	out := []store.Event{}
	for rows.Next() {
		var e store.Event
		if err := rows.Scan(&e.Seq, &e.At, &e.Type, &e.Kind, &e.ID, &e.Number, &e.Detail, &e.Actor); err != nil {
			return nil, fmt.Errorf("events: %w", err)
		}
		e.At = e.At.UTC()
		out = append(out, e)
	}
	return out, rows.Err()
}
