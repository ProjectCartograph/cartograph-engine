package store

import "context"

// SetReader is a ManifestStore that answers for many manifests in one
// call. The engine asks for sets wherever it would otherwise loop over
// GetCurrent or ListReferencing, which in a database across a network
// is a round trip per manifest (docs/adr/0012). An adapter implements it
// when it can answer each in one query; the engine falls back to the
// loop when it does not.
type SetReader interface {
	// CurrentOfKind returns what GetCurrent returns for every manifest of
	// a kind (the working copy where there is one, else the highest
	// version), ordered by id.
	CurrentOfKind(ctx context.Context, kind string) ([]Version, error)

	// ReferencingKind returns, for every manifest of toKind that something
	// references, what ListReferencing returns for it, keyed by its id.
	ReferencingKind(ctx context.Context, toKind string) (map[string][]Summary, error)

	// CurrentMany returns what GetCurrent returns for each of ids that
	// exists, ordered by id: a page of a list, read in one call.
	CurrentMany(ctx context.Context, kind string, ids []string) ([]Version, error)
}

// SummaryPager is a ManifestStore that lists one page of a kind at a
// time, so a page costs its own size and not the kind's.
type SummaryPager interface {
	// ListSummariesAfter is ListSummaries limited to ids after afterID
	// ("" for the first page), at most limit of them, in id order.
	ListSummariesAfter(ctx context.Context, kind, query string, refs []RefFilter, afterID string, limit int) ([]Summary, error)
}

// StateSetReader is an OperationalStore that answers the current state
// of many projects in one call, for a list of projects.
type StateSetReader interface {
	// CurrentStates returns the latest state of each project given that
	// has any history; a project without one is in the implicit "draft".
	CurrentStates(ctx context.Context, projectIDs []string) (map[string]string, error)
}

// DocWorkingStore is a ManifestStore that keeps a working copy's
// document beside its text, as it keeps a version's (Version.Doc).
type DocWorkingStore interface {
	PutWorkingDoc(ctx context.Context, kind, id string, text, doc []byte) error
}

// DocRepairer is a ManifestStore whose documents can be missing: rows a
// writer stored as text alone, such as a replica of an earlier release
// during a rolling upgrade. The engine decodes them and puts the
// documents back.
type DocRepairer interface {
	// StaleDocs returns up to limit versions and working copies stored
	// without a document. A working copy has Number 0.
	StaleDocs(ctx context.Context, limit int) ([]Version, error)

	// PutDocs stores the document of each version or working copy given,
	// matched by Kind, ID and Number. It changes nothing else.
	PutDocs(ctx context.Context, docs []Version) error
}

// VersionCounter is a ManifestStore that knows a manifest's highest
// version number without reading its history. The engine asks it for
// the next number at every save; without it, it lists every version.
type VersionCounter interface {
	// LatestNumber returns the highest version number recorded, 0 when
	// there is none.
	LatestNumber(ctx context.Context, kind, id string) (int, error)
}
