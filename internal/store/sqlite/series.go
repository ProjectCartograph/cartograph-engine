package sqlite

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

var (
	_ store.SeriesStore = (*ManifestStore)(nil)
	_ store.EventLog    = (*ManifestStore)(nil)
)

// seriesLayout writes a time at a fixed width, so text order is time
// order and an as-of read can compare it as text.
const seriesLayout = "2006-01-02T15:04:05.000000000Z"

// RecordSeries appends series items.
func (m *ManifestStore) RecordSeries(ctx context.Context, items []store.SeriesItem) error {
	for _, it := range items {
		var item any
		if it.Item != nil {
			item = string(it.Item)
		}
		if _, err := m.ex.ExecContext(ctx, `INSERT INTO series_items (kind, id, series, key, item, recorded_at, recorded_by, reason)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			it.Kind, it.ID, it.Series, it.Key, item, it.RecordedAt.UTC().Format(seriesLayout), it.RecordedBy, it.Reason); err != nil {
			return err
		}
	}
	return nil
}

// SeriesAsOf merges each key's rows into its value at at: the last row
// recorded by then.
func (m *ManifestStore) SeriesAsOf(ctx context.Context, kind, series string, ids []string, at time.Time) (map[string][]store.SeriesItem, error) {
	if at.IsZero() {
		at = time.Now()
	}
	q := `SELECT id, key, item, recorded_at, recorded_by, reason FROM series_items
		WHERE kind = ? AND series = ? AND recorded_at <= ?`
	args := []any{kind, series, at.UTC().Format(seriesLayout)}
	if ids != nil {
		if len(ids) == 0 {
			return map[string][]store.SeriesItem{}, nil
		}
		q += ` AND id IN (` + strings.TrimSuffix(strings.Repeat("?, ", len(ids)), ", ") + `)`
		for _, id := range ids {
			args = append(args, id)
		}
	}
	q += ` ORDER BY id, key, recorded_at DESC, seq DESC`
	rows, err := m.ex.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]store.SeriesItem{}
	var lastID, lastKey string
	first := true
	for rows.Next() {
		it := store.SeriesItem{Kind: kind, Series: series}
		var item sql.NullString
		var on string
		if err := rows.Scan(&it.ID, &it.Key, &item, &on, &it.RecordedBy, &it.Reason); err != nil {
			return nil, err
		}
		if !first && it.ID == lastID && it.Key == lastKey {
			continue // an earlier row for a key already answered
		}
		first, lastID, lastKey = false, it.ID, it.Key
		if !item.Valid {
			continue // removed
		}
		it.Item = []byte(item.String)
		it.RecordedAt, _ = time.Parse(seriesLayout, on)
		out[it.ID] = append(out[it.ID], it)
	}
	return out, rows.Err()
}

// Events returns events after a cursor, oldest first.
func (m *ManifestStore) Events(ctx context.Context, after int64, limit int) ([]store.Event, error) {
	rows, err := m.ex.QueryContext(ctx, `SELECT seq, at, type, kind, id, number, detail, actor FROM events
		WHERE seq > ? ORDER BY seq LIMIT ?`, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []store.Event{}
	for rows.Next() {
		var e store.Event
		var at string
		if err := rows.Scan(&e.Seq, &at, &e.Type, &e.Kind, &e.ID, &e.Number, &e.Detail, &e.Actor); err != nil {
			return nil, err
		}
		if t, err := time.Parse(time.RFC3339Nano, at); err == nil {
			e.At = t
		} else if t, err := time.Parse(seriesLayout, at); err == nil {
			e.At = t
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
