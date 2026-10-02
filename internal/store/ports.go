package store

import "context"

// The two ports below are optional: an adapter implements them when it
// can, and the engine answers ErrUnsupported-style errors when it cannot.
// They exist so that nothing outside an adapter knows whether manifests
// live in files, in a database, or somewhere else.

// StateStore is the apply gate: an adapter keeps a state manifest (an
// include list) over manifests that can exist before they are included.
// The vault adapter implements it over vault.yaml. A database adapter
// where every row is live has no need for it.
type StateStore interface {
	// Name is the state manifest's own id (metadata.id).
	Name() string

	// Included returns every ref ("Kind/id") the state manifest includes,
	// in the order it lists them.
	Included(ctx context.Context) ([]string, error)

	// ListUnapplied returns every manifest the adapter can see that is
	// neither included nor excluded: what a person could apply next.
	ListUnapplied(ctx context.Context) ([]UnappliedRef, error)

	// Apply includes the given refs. Applying a ref that is already
	// included is quiet; a ref the adapter cannot find is refused with
	// an error that names it. Returns how many refs were newly included.
	Apply(ctx context.Context, refs []string) (added int, err error)

	// StateYAML returns the state manifest's own bytes, or found=false
	// when the adapter has not written one yet.
	StateYAML(ctx context.Context) (yaml []byte, found bool, err error)

	// Rehydrate reloads the live state from the adapter's source of
	// truth. Called after Apply or Recover changed what is included.
	Rehydrate(ctx context.Context) error
}

// UnappliedRef is one manifest that could be applied.
type UnappliedRef struct {
	Kind string
	ID   string
	Name string
}

// BundleStore keeps the files a handoff produces (the rendered charter in
// its formats) beside the version they were rendered from. The vault
// adapter writes them under .cartograph/handoff; the memory adapter keeps them
// in memory, which is what tests need.
type BundleStore interface {
	// PutBundle stores files (name to content) for one project at one
	// version, all or nothing, and returns where they went.
	PutBundle(ctx context.Context, projectID string, version int, files map[string][]byte) (Bundle, error)
}

// Bundle is where a handoff's files went. Location is what the state
// history records: a path relative to the vault root for the vault
// adapter, an opaque name for others. Paths maps each file name to where a
// person can find it, for the command line to print.
type Bundle struct {
	Location string
	Paths    map[string]string
}

// VaultIndex is everything the vault adapter keeps beside its files: the
// rebuildable cache a database holds for it. SQLite implements it today;
// any adapter that passes conformance.RunVaultIndex may.
type VaultIndex interface {
	ManifestStore                  // versions, summaries, references, working copies, exclusions
	Journal() ApplyJournal         // the two-phase apply journal
	Operational() OperationalStore // project state history (same database)
	Docs() DocStore                // the shared drafts' CRDT documents (same database)
	Access() AccessStore           // the access list (same database)

	// File hashes: what the vault compares a file against on open and on a watcher event.
	GetFileHash(ctx context.Context, kind, id string) (sha256 string, found bool, err error)
	PutFileHash(ctx context.Context, kind, id, sha256, mtime string) error
	DeleteFileHash(ctx context.Context, kind, id string) error

	// Meta: small key/value facts about the index itself (the recorded vault.yaml hash, rebuild time).
	GetMeta(ctx context.Context, key string) (value string, found bool, err error)
	PutMeta(ctx context.Context, key, value string) error

	Close() error
}
