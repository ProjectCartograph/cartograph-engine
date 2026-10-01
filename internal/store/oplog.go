package store

import (
	"context"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/pkg/merge"
)

// Op is one write to one leaf of one manifest's shared working document,
// as the op log keeps it: the merge op plus where it belongs and when it
// arrived. Seq is the log's own order, assigned on append, dense per
// manifest; clients catch up by asking for everything after the last Seq
// they saw.
type Op struct {
	Kind string
	ID   string
	Seq  int64
	On   time.Time
	merge.Op
}

// OpLog is the port under multiplayer editing: an append-only log of
// leaf writes per manifest. Reads are by position, so a client that was
// away (a TUI that lost its connection, a browser tab that slept) asks
// for what it missed and merges it; because the ops form a CRDT, the
// order it receives them in does not matter.
//
// The log is not the record of truth: that is the committed version. The
// log is the shared draft between versions, and Compact may drop
// everything at or before the op that became a version.
type OpLog interface {
	// Append records ops for one manifest and returns the Seq of the
	// first, with the rest following in order. Every op must name the
	// same kind and id.
	Append(ctx context.Context, ops []Op) (firstSeq int64, err error)

	// Since returns ops for one manifest with Seq greater than after,
	// oldest first, at most limit (0 means no limit). Always a non-nil
	// slice.
	Since(ctx context.Context, kind, id string, after int64, limit int) ([]Op, error)

	// Latest returns the highest Seq for one manifest, 0 when it has none.
	Latest(ctx context.Context, kind, id string) (int64, error)

	// Compact removes ops at or before seq for one manifest, after the
	// state they produced has been committed as a version.
	Compact(ctx context.Context, kind, id string, seq int64) error
}
