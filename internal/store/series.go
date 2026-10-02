package store

import (
	"context"
	"time"
)

// SeriesItem is one element of a series: an array a kind's schema marks
// x-cartograph-series, such as a KPI's readings. Besides living in the
// manifest, every item is kept as an append-only row of what was
// recorded, when and by whom, so a series can be read as it stood at
// any time and reported on without reading every version
// (docs/adr/0013).
type SeriesItem struct {
	Kind, ID string
	// Series is the array's JSON pointer, "/spec/readings".
	Series string
	// Key is the value of the element's key field, its period.
	Key string
	// Item is the element as JSON; nil records that it was removed.
	Item       []byte
	RecordedAt time.Time
	RecordedBy string
	Reason     string
}

// SeriesStore keeps series items. The engine records them in the
// transaction of the save that changed them.
type SeriesStore interface {
	RecordSeries(ctx context.Context, items []SeriesItem) error

	// SeriesAsOf returns each manifest's series as it stood at at (the
	// zero time for now): for every key the item recorded last, leaving
	// out removed ones, in key order, by manifest id. ids limits it to
	// those manifests; nil is every manifest of the kind.
	SeriesAsOf(ctx context.Context, kind, series string, ids []string, at time.Time) (map[string][]SeriesItem, error)
}

// Event is one thing that happened to the record: a version saved, a
// project's state changed, a series item recorded. Seq orders them and
// is a consumer's cursor.
type Event struct {
	Seq    int64
	At     time.Time
	Type   string // "version", "state" or "series"
	Kind   string
	ID     string
	Number int    // the version, for "version"
	Detail string // the new state, or the series item's key
	Actor  string
}

// EventLog reads the events a store wrote as it saved. Every adapter
// writes them in the transaction of the change itself, so an event is
// never lost and never early.
type EventLog interface {
	// Events returns up to limit events after seq after, oldest first.
	Events(ctx context.Context, after int64, limit int) ([]Event, error)
}
