// Package store defines the two ports the engine is built against
// (ManifestStore and OperationalStore) and the record shapes they persist.
// The SQLite adapter (package sqlite) and the in-memory adapter (package
// memory) both satisfy these interfaces and are proven equivalent by the
// conformance suite in package conformance.
package store

import (
	"context"
	"time"
)

// Version is one immutable, committed version of a manifest.
type Version struct {
	Kind   string
	ID     string
	Number int
	YAML   []byte
	Actor  string
	Reason string
	On     time.Time

	// Doc is the same manifest as a JSON document, decoded by the engine
	// at every save. YAML stays what the person wrote; Doc is what an
	// adapter that keeps documents stores, indexes and queries. An
	// adapter may ignore it, and need not return it.
	Doc []byte
}

// Summary is the list-view projection of a manifest's current version.
type Summary struct {
	Kind      string
	ID        string
	Name      string
	Version   int
	UpdatedOn time.Time

	// Labels is metadata.labels, carried on the summary so a list can be
	// grouped and filtered by them. Nil where the manifest sets none.
	Labels map[string]string

	// Draft is true only for a synthetic Summary standing in for a
	// project that has only ever been saved as a draft, never committed
	// (includeDrafts=true): Version is then 0 and UpdatedOn is the
	// draft's own save time. False (the zero value) for every ordinary,
	// committed Summary a store adapter's own ListSummaries builds.
	Draft bool
}

// Ref is one outgoing reference recorded for a manifest: at Path (a JSON
// pointer within it) it names ToKind/ToID.
type Ref struct {
	Path   string
	ToKind string
	ToID   string
}

// RefFilter narrows ListSummaries to manifests with an outgoing reference
// to Kind/ID.
type RefFilter struct {
	Kind string
	ID   string
}

// ManifestStore holds every manifest version ever committed. Rows are
// immutable: once written, a version is never updated or deleted.
type ManifestStore interface {
	// PutVersion appends a new version. The caller is responsible for
	// computing Number as one more than the current version (or 1 for a
	// new manifest); an adapter rejects a Number that is not exactly that,
	// which is what guards immutability under concurrent writers.
	PutVersion(ctx context.Context, v Version) error

	// GetCurrent returns the highest-numbered version of a manifest.
	GetCurrent(ctx context.Context, kind, id string) (Version, bool, error)

	// GetVersion returns one specific historical version.
	GetVersion(ctx context.Context, kind, id string, number int) (Version, bool, error)

	// ListVersions returns every version of a manifest, oldest first.
	ListVersions(ctx context.Context, kind, id string) ([]Version, error)

	// ListAllVersions returns every version of every manifest across all
	// kinds, newest first, with optional pagination (limit defaults to 20,
	// cursor is opaque and returned by the preceding call). Used by the
	// snapshots list endpoint.
	ListAllVersions(ctx context.Context, limit int, cursor string) (versions []Version, nextCursor string, err error)

	// ListSummaries returns the current version of every manifest of a
	// kind, optionally filtered by a case-insensitive substring match over
	// id and name (query, ignored when empty) and by zero or more
	// reference filters (refs; a manifest must have a current outgoing
	// reference matching every one of them - logical AND, ignored when
	// empty). Always returns a non-nil, possibly empty, slice.
	ListSummaries(ctx context.Context, kind, query string, refs []RefFilter) ([]Summary, error)

	// ListIDs returns the id of every manifest of a kind that has a
	// current version.
	ListIDs(ctx context.Context, kind string) ([]string, error)

	// Counts returns the number of manifests (by id, not by version) for
	// every kind that has at least one.
	Counts(ctx context.Context) (map[string]int, error)

	// IndexReferences replaces the recorded outgoing references for one
	// manifest's current version. The engine calls this once per commit,
	// right after PutVersion, inside the same transaction, with every
	// x-cartograph-ref value found by walking that kind's schema.
	IndexReferences(ctx context.Context, fromKind, fromID string, refs []Ref) error

	// ListReferencedBy returns every outgoing reference recorded for one
	// manifest's current version.
	ListReferencedBy(ctx context.Context, fromKind, fromID string) ([]Ref, error)

	// ListReferencing returns the current summary of every manifest that
	// has an outgoing reference to toKind/toID.
	ListReferencing(ctx context.Context, toKind, toID string) ([]Summary, error)

	// PutWorking writes a manifest's YAML as the working copy without creating
	// a version. Used for autosave writes that don't create an explicit
	// snapshot. The working copy is updated, but no version entry is created.
	PutWorking(ctx context.Context, kind, id string, yamlBytes []byte) error

	// GetWorking returns a manifest's working copy (the file on disk that may
	// not yet be versioned). Returns found=false when no working copy exists.
	GetWorking(ctx context.Context, kind, id string) ([]byte, bool, error)

	// WithinTransaction runs fn against a ManifestStore that commits all of
	// its writes together, or none of them if fn returns an error.
	WithinTransaction(ctx context.Context, fn func(ctx context.Context, tx ManifestStore) error) error

	// Exclude removes a manifest from vault.yaml and records the exclusion.
	// The file remains on disk. Returns an error if the manifest does not exist
	// (use GetCurrent to check first). Refused if the manifest is referenced by
	// a live manifest (call ListReferencing first).
	Exclude(ctx context.Context, kind, id, name, reason, operator string) error

	// ListExcluded returns every excluded manifest, newest first.
	ListExcluded(ctx context.Context) ([]Exclusion, error)

	// Recover re-adds an excluded manifest to vault.yaml and removes the
	// exclusion record. Returns an error if the file is missing from disk.
	Recover(ctx context.Context, kind, id, reason, operator string) error
}

// OperationalStore holds facts about manifests: project state history and
// project drafts. Later increments add readings and data-quality check results.
type OperationalStore interface {
	// PutProjectStateTransition appends one row to a project's state
	// history (the table is append-only; the current state is the most
	// recent row). A project with no rows is implicitly "draft".
	PutProjectStateTransition(ctx context.Context, e ProjectStateEntry) error

	// ListProjectStateHistory returns every state transition for a
	// project, oldest first. Empty (never nil) when the project has never
	// moved out of the implicit "draft" state.
	ListProjectStateHistory(ctx context.Context, projectID string) ([]ProjectStateEntry, error)
}

// ProjectStateEntry is one row of a project's state history: at On, Actor
// moved the project to State, giving Reason (required only for a move to
// cancelled; empty otherwise). Snapshot and Bundle are set when state is
// handed off: Snapshot is the version number, Bundle is the directory path
// where the charter bundle was written (relative to vault root).
type ProjectStateEntry struct {
	ProjectID string
	State     string
	Actor     string
	Reason    string
	On        time.Time
	Snapshot  int
	Bundle    string
}

// ManifestDraft is a manifest's working copy: the YAML as last saved with
// the draft header, never validated beyond its basic shape, never
// versioned. Keyed by Kind and ID (generalised from the
// Project-only project_draft table).
type ManifestDraft struct {
	Kind string
	ID   string
	YAML []byte
	On   time.Time
}

// ErrNotFound is returned by Get-style methods is expressed with the bool
// "found" return instead of a sentinel error, to keep call sites simple;
// this type exists only for adapters that need a concrete not-found error
// internally (for example inside a transaction helper).
type NotFoundError struct {
	Kind string
	ID   string
}

func (e *NotFoundError) Error() string {
	return "not found: " + e.Kind + "/" + e.ID
}

// ConflictError is returned by PutVersion when a manifest's file on disk
// has changed since it was last read, indicating concurrent edits that need
// reconciliation. Both Ours (the version being written) and Theirs (the
// current file on disk) carry the full YAML text.
type ConflictError struct {
	Kind   string
	ID     string
	Ours   []byte
	Theirs []byte
}

func (e *ConflictError) Error() string {
	return "conflict: " + e.Kind + "/" + e.ID + " has been changed on disk"
}

// ApplyUnit is one mutation: a set of file writes recorded first with applied=false,
// then files written (temp file and rename, vault.yaml last), then marked applied
// and the index updated in one transaction. On open, replay every unit not applied.
type ApplyUnit struct {
	ID       string      // Unique identifier for this unit (uuid)
	On       time.Time   // When the mutation happened
	Operator string      // Who performed it (actor, "local" for file operations)
	Reason   string      // Why (for exclusion, recovery, or user-supplied reason)
	Files    []ApplyFile // Files to write: {path, sha256, content}
	Applied  bool        // Whether this unit was applied (committed to files and index)
}

// ApplyFile is one file write within an ApplyUnit: path relative to vault root,
// sha256 hash of content (for idempotence during replay), and the content itself.
type ApplyFile struct {
	Path    string // Path relative to vault root (e.g., "vault.yaml" or "Goal/some-goal.yaml")
	SHA256  string // SHA-256 hash of content (hex, 64 chars)
	Content []byte // File content
}

// Exclusion is one manifest that has been excluded from the live state.
type Exclusion struct {
	Kind     string    // Manifest kind
	ID       string    // Manifest id
	Name     string    // Manifest name at time of exclusion
	On       time.Time // When excluded
	Reason   string    // Why excluded
	Operator string    // Who excluded it
}

// ApplyJournal records every mutation (apply, exclude, recover, etc.) with a
// two-phase commit: write the unit (applied=false), commit files to disk, then
// mark applied and update the index in one transaction.
type ApplyJournal interface {
	// PutUnit writes a new unit with applied=false.
	PutUnit(ctx context.Context, unit ApplyUnit) error

	// MarkApplied marks a unit as applied and updates the index. Called in the
	// same transaction as index updates (rebuild, rehydration, reference indexing).
	MarkApplied(ctx context.Context, unitID string) error

	// ListUnapplied returns every unit with applied=false, oldest first.
	ListUnapplied(ctx context.Context) ([]ApplyUnit, error)

	// PruneOld removes applied units older than the given count (keeps the most
	// recent `keepCount` applied units). A keepCount of 0 or less means unlimited.
	PruneOld(ctx context.Context, keepCount int) error

	// WithinTransaction runs fn against the journal in a transaction (adapters
	// that support transactions). Idempotent with database adapters.
	WithinTransaction(ctx context.Context, fn func(ctx context.Context, tx ApplyJournal) error) error
}
