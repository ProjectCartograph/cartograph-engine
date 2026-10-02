package memory

import (
	"context"
	"sort"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

var (
	_ store.SeriesStore = (*ManifestStore)(nil)
	_ store.EventLog    = (*ManifestStore)(nil)
)

// RecordSeries appends series items.
func (m *ManifestStore) RecordSeries(_ context.Context, items []store.SeriesItem) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, it := range items {
		m.series = append(m.series, it)
		m.eventLocked(store.Event{At: it.RecordedAt, Type: "series", Kind: it.Kind, ID: it.ID, Detail: it.Key, Actor: it.RecordedBy})
	}
	return nil
}

// SeriesAsOf merges each key's items into its value at at.
func (m *ManifestStore) SeriesAsOf(_ context.Context, kind, series string, ids []string, at time.Time) (map[string][]store.SeriesItem, error) {
	if at.IsZero() {
		at = time.Now()
	}
	var only map[string]bool
	if ids != nil {
		only = map[string]bool{}
		for _, id := range ids {
			only[id] = true
		}
	}
	m.mu.Lock()
	last := map[[2]string]store.SeriesItem{}
	for _, it := range m.series { // in the order recorded
		if it.Kind != kind || it.Series != series || it.RecordedAt.After(at) || (only != nil && !only[it.ID]) {
			continue
		}
		last[[2]string{it.ID, it.Key}] = it
	}
	m.mu.Unlock()
	out := map[string][]store.SeriesItem{}
	for _, it := range last {
		if it.Item != nil {
			out[it.ID] = append(out[it.ID], it)
		}
	}
	for id := range out {
		sort.Slice(out[id], func(i, j int) bool { return out[id][i].Key < out[id][j].Key })
	}
	return out, nil
}

// Events returns events after a cursor, oldest first.
func (m *ManifestStore) Events(_ context.Context, after int64, limit int) ([]store.Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []store.Event{}
	for i := int(after); i < len(m.events) && len(out) < limit; i++ {
		if i >= 0 {
			out = append(out, m.events[i])
		}
	}
	return out, nil
}
