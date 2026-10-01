// Package crdt is the port between the engine and the replicated data
// type under every shared draft. A manifest's draft is one CRDT document
// (docs/adr/0007): every replica of the engine and every interface holds
// a copy, edits it optimistically, and exchanges changes until all copies
// are equal. Any set of changes, delivered in any order, any number of
// times, across any partition, materialises the same JSON.
//
// The engine depends on this package only. The adapter (crdt/automerge)
// runs Automerge, compiled to WebAssembly, so that interfaces using the
// stock Automerge libraries speak to the engine without translation.
//
// A Doc is not safe for concurrent use; the engine serialises access to
// each one. Every method that returns bytes returns a copy the caller
// owns.
package crdt

import "errors"

// ErrClosed is returned by a Doc or SyncState used after Close.
var ErrClosed = errors.New("crdt: closed")

// Heads identify a document's state: the hashes of the changes nothing
// else depends on, hex-encoded and sorted. Two documents with equal heads
// hold equal content.
type Heads []string

// Equal reports whether two sets of heads name the same state.
func (h Heads) Equal(o Heads) bool {
	if len(h) != len(o) {
		return false
	}
	for i := range h {
		if h[i] != o[i] {
			return false
		}
	}
	return true
}

// Shape says how a manifest's JSON maps onto the document's types, so a
// whole document can be reconciled into a CRDT without losing the
// identity of what did not change. Both come from the kind's schema.
type Shape struct {
	// ListKeys names, per list, the field that identifies its items:
	// "/spec/keyResults" -> "id". A pointer segment "*" stands for any
	// index, so "/spec/phases/*/deliverables" covers every phase. An item
	// whose key matches keeps its identity across a reconcile; a list
	// with no key is matched by position.
	ListKeys map[string]string
	// Texts are the string fields edited character by character (and so
	// merged character by character), with "*" for any index. Every
	// other string is a scalar: the last write wins and a concurrent
	// write is kept as a conflict.
	Texts []string
}

// Change is the metadata recorded with a change the engine makes.
type Change struct {
	// Message is recorded in the change, for history: the principal the
	// engine acted for and why ("import", "file changed", "save").
	Message string
	// Time is the change's timestamp in Unix milliseconds; zero records
	// none.
	Time int64
}

// Conflict is a field where concurrent writes were not resolved by a
// later one: every value is kept, and Values[0] is the one the
// materialised document shows (the deterministic winner).
type Conflict struct {
	Path   string
	Values []any
}

// Doc is one CRDT document.
type Doc interface {
	// JSON materialises the document: maps as objects, lists as arrays,
	// texts and scalar strings as strings, counters as numbers.
	JSON() (map[string]any, error)
	// Reconcile makes the document equal to doc with the fewest changes,
	// following shape, and records them as one change. It reports
	// whether anything changed.
	Reconcile(doc map[string]any, shape Shape, meta Change) (bool, error)
	// Heads returns the current heads.
	Heads() (Heads, error)
	// Conflicts lists every field holding concurrent values.
	Conflicts() ([]Conflict, error)

	// Save returns the whole document, compacted.
	Save() ([]byte, error)
	// SaveIncremental returns the changes since the last Save,
	// SaveIncremental or Load, or nil when there are none. Appending
	// these chunks to a saved document and loading the lot reproduces
	// the document.
	SaveIncremental() ([]byte, error)
	// LoadIncremental applies saved bytes (a snapshot or chunks) to this
	// document. Applying bytes it already holds changes nothing.
	LoadIncremental(data []byte) error

	// GenerateSyncMessage returns the next message for the peer whose
	// state is s, or ok=false when the peer is up to date.
	GenerateSyncMessage(s SyncState) (msg []byte, ok bool, err error)
	// ReceiveSyncMessage applies a message from the peer whose state is
	// s. Changes it carries are applied; their order does not matter.
	ReceiveSyncMessage(s SyncState, msg []byte) error

	// Fork returns an independent copy with a new actor.
	Fork() (Doc, error)
	Close() error
}

// SyncState is what one side of the sync protocol remembers about one
// peer for one document. It lives as long as the connection; a new one
// starts from heads and arrives at the same place, only slower.
type SyncState interface {
	Close() error
}

// Engine makes documents and sync states. It is the port the composition
// root chooses an adapter for.
type Engine interface {
	// New returns an empty document with a fresh, random actor.
	New() (Doc, error)
	// Load returns a document from saved bytes (a snapshot followed by
	// any chunks), with a fresh, random actor for the changes it makes.
	Load(data []byte) (Doc, error)
	// NewSyncState returns the state for a new peer.
	NewSyncState() (SyncState, error)
	Close() error
}
