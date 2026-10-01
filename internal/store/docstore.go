package store

import (
	"context"
	"errors"
)

// ErrNoDocument is returned for a document id or a manifest that has no
// shared document.
var ErrNoDocument = errors.New("no such document")

// Chunk is one saved piece of a shared document: incremental changes,
// appended in the order the store received them.
type Chunk struct {
	Seq  int64
	Data []byte
}

// DocStore keeps the shared draft of each manifest as a CRDT document
// (docs/adr/0007): a compacted snapshot plus the chunks appended since.
// Loading the snapshot and every chunk, in any order, reproduces the
// document, so concurrent appends from different replicas never need
// arbitration. It holds bytes only; what they mean is the crdt port's
// business.
//
// The document is not the record of truth; versions are. It is the draft
// between them, and it carries on across versions.
type DocStore interface {
	// Create records a new document for a manifest with its first
	// snapshot, unless the manifest already has one. created is false,
	// and docID the existing document's, when another writer got there
	// first; the caller then loads that one and discards its own.
	Create(ctx context.Context, kind, id, docID string, snapshot []byte) (existing string, created bool, err error)
	// DocumentFor returns the document id of a manifest's draft, or
	// ErrNoDocument.
	DocumentFor(ctx context.Context, kind, id string) (string, error)
	// ManifestFor returns the manifest a document belongs to, or
	// ErrNoDocument.
	ManifestFor(ctx context.Context, docID string) (kind, id string, err error)
	// Append stores a chunk and returns its sequence number, which is
	// greater than every earlier one for the document.
	Append(ctx context.Context, docID string, data []byte) (int64, error)
	// Load returns the snapshot and every chunk since, oldest first.
	Load(ctx context.Context, docID string) (snapshot []byte, chunks []Chunk, err error)
	// Since returns the chunks after seq, oldest first; always non-nil.
	Since(ctx context.Context, docID string, after int64) ([]Chunk, error)
	// Compact replaces the snapshot with one that includes every chunk
	// up to and including upTo, and drops those chunks. Chunks appended
	// after upTo, even while compacting, are kept.
	Compact(ctx context.Context, docID string, snapshot []byte, upTo int64) error
}
