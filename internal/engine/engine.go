package engine

import (
	"context"
	"errors"
	"fmt"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/identity"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/codec"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/crdt"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/fanout"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/kinds"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/kinds/kit"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

// Engine is the kind-agnostic manifest engine. Construct with New. It
// talks to the outside world through ports only: the two stores every
// deployment has, and the optional ones an adapter may offer (the apply
// gate, the bundle store). It never touches a file system or a browser
// itself.
type Engine struct {
	manifests store.ManifestStore
	ops       store.OperationalStore
	state     store.StateStore    // nil when the store has no apply gate
	bundles   store.BundleStore   // nil when nowhere keeps handoff bundles
	codec     codec.Codec         // the manifest syntax; YAML unless told otherwise
	bus       Bus                 // event fan-out; in-process unless told otherwise
	crdt      crdt.Engine         // the CRDT under shared drafts (docs/adr/0007)
	docs      store.DocStore      // where shared drafts are kept
	fan       fanout.Bus          // hints between replicas (docs/adr/0008)
	shared    *Shared             // built when crdt, docs and fan are all set
	docCache  int                 // the shared-document cache bound; 0 means the default
	authz     identity.Authorizer // asked again at every write (docs/adr/0011); nil asks nothing
	teamCache teamCache           // the team tree, for team chains
	schemas   *schemaSet
	refRules  map[string][]refRule
}

// Option configures an Engine beyond its two required stores.
type Option func(*Engine)

// WithCodec sets the manifest syntax. Every store carries text in this
// codec; the engine never parses a syntax itself.
func WithCodec(c codec.Codec) Option {
	return func(e *Engine) { e.codec = c }
}

// Codec is the manifest syntax this engine reads and writes, for the
// drivers that turn a request body into text and back.
func (e *Engine) Codec() codec.Codec { return e.codec }

// WithState attaches the apply gate (store.StateStore) the manifest store
// offers. Without it, the state endpoints answer ErrNoState.
func WithState(s store.StateStore) Option {
	return func(e *Engine) { e.state = s }
}

// WithBundles attaches where handoff bundles are kept. Without it, a
// handoff is refused.
func WithBundles(b store.BundleStore) Option {
	return func(e *Engine) { e.bundles = b }
}

// New builds an Engine over the given stores, compiling every kind's JSON
// Schema and precomputing its reference rules. An adapter that offers more
// than the two required ports is attached with options; a store that
// satisfies StateStore or BundleStore itself is attached automatically.
func New(manifests store.ManifestStore, ops store.OperationalStore, opts ...Option) (*Engine, error) {
	ss, err := loadSchemas()
	if err != nil {
		return nil, err
	}
	rules := map[string][]refRule{}
	for _, spec := range kinds.All {
		rules[spec.Name] = collectRefRules(ss.raw, spec.SchemaFile)
	}
	e := &Engine{manifests: manifests, ops: ops, schemas: ss, refRules: rules}
	if s, ok := manifests.(store.StateStore); ok {
		e.state = s
	}
	if b, ok := manifests.(store.BundleStore); ok {
		e.bundles = b
	}
	for _, opt := range opts {
		opt(e)
	}
	if e.codec == nil {
		// The syntax is an adapter and the composition root chooses it;
		// the engine never picks one itself.
		return nil, errors.New("engine: a codec is required (engine.WithCodec)")
	}
	if e.bus == nil {
		e.bus = NewMemoryBus()
	}
	e.initShared()
	return e, nil
}

// Kinds lists every registered kind and how many manifests exist for it. A
// store failure degrades to a zero count rather than an error, since the
// kind registry itself never fails and this method has no error return.
func (e *Engine) Kinds() []KindInfo {
	counts, _ := e.manifests.Counts(context.Background())
	out := make([]KindInfo, 0, len(kinds.All))
	for _, spec := range kinds.All {
		out = append(out, KindInfo{Kind: spec.Name, Count: counts[spec.Name]})
	}
	return out
}

// Schema returns the raw JSON Schema document for a kind.
// SchemaJSON returns a kind's schema exactly as it is written on disk.
//
// Schema() decodes into a map, which loses the order the properties were
// written in; the interface builds a sheet's columns from that order, so
// serving the decoded map alphabetised every sheet. This serves the file.
func (e *Engine) SchemaJSON(kind string) ([]byte, error) {
	b, ok := e.schemas.bytes[kind]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownKind, kind)
	}
	return b, nil
}

func (e *Engine) Schema(kind string) (map[string]any, error) {
	doc, ok := e.schemas.docs[kind]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownKind, kind)
	}
	return doc, nil
}

// Validate checks a manifest's schema, its references and its kind's
// rules, in that order, against the current store state (no in-flight
// import batch).
func (e *Engine) Validate(ctx context.Context, kind string, yamlBytes []byte) ([]Problem, error) {
	_, problems, err := e.validate(ctx, kind, yamlBytes, nil)
	return problems, err
}

// validate is Validate plus an optional import-batch overlay used by
// ImportDir so members of one import can reference each other.
func (e *Engine) validate(ctx context.Context, kind string, yamlBytes []byte, overlay map[string]map[string]map[string]any) (map[string]any, []Problem, error) {
	spec, ok := kinds.ByName(kind)
	if !ok {
		return nil, nil, fmt.Errorf("%w: %s", ErrUnknownKind, kind)
	}

	var doc map[string]any
	if err := e.codec.DecodeInto(yamlBytes, &doc); err != nil {
		return nil, []Problem{{Path: "", Message: "invalid yaml: " + err.Error()}}, nil
	}
	if doc == nil {
		return nil, []Problem{{Path: "", Message: "empty manifest"}}, nil
	}

	if schema, ok := e.schemas.compiled[kind]; ok {
		if err := schema.Validate(any(doc)); err != nil {
			return doc, schemaProblems(err), nil
		}
	}

	l := &lookup{ctx: ctx, store: e.manifests, codec: e.codec, overlay: overlay}

	var problems []Problem
	for _, fr := range extractRefs(doc, e.refRules[kind]) {
		if !l.exists(fr.kind, fr.id) {
			problems = append(problems, Problem{
				Path:    fr.path,
				Message: fmt.Sprintf("references %s %q, which does not exist", fr.kind, fr.id),
			})
		}
	}
	if len(problems) > 0 {
		return doc, problems, nil
	}

	if spec.Rules != nil {
		id, _ := docID(doc)
		for _, p := range spec.Rules(doc, kit.RuleContext{ID: id, Lookup: l}) {
			problems = append(problems, Problem(p))
		}
	}

	return doc, problems, nil
}

// schemaProblems flattens a jsonschema.ValidationError into Problems. It
// uses DetailedOutput rather than BasicOutput: for some keywords (observed
// with "required") BasicOutput's flattened Errors carry a generic "validation
// failed" message while DetailedOutput's tree keeps the specific one (for
// example "missing property 'direction'") on its leaves, so this walks that
// tree instead and keeps only the leaves that carry a message.
func schemaProblems(err error) []Problem {
	ve, ok := asValidationError(err)
	if !ok {
		return []Problem{{Path: "", Message: err.Error()}}
	}
	var problems []Problem
	var walk func(u jsonschema.OutputUnit)
	walk = func(u jsonschema.OutputUnit) {
		if len(u.Errors) == 0 {
			if u.Error != nil {
				problems = append(problems, Problem{Path: u.InstanceLocation, Message: u.Error.String()})
			}
			return
		}
		for _, c := range u.Errors {
			walk(c)
		}
	}
	walk(*ve.DetailedOutput())
	if len(problems) == 0 {
		problems = append(problems, Problem{Path: "", Message: err.Error()})
	}
	return problems
}

func docID(doc map[string]any) (string, bool) {
	meta, ok := doc["metadata"].(map[string]any)
	if !ok {
		return "", false
	}
	id, ok := meta["id"].(string)
	return id, ok
}

// Commit validates a manifest and, if it is valid, stores it as the next
// immutable version. The id in the manifest's metadata must match id.
// actor is recorded as given (see checkActor's own doc comment for why actor
// validation was removed).
func (e *Engine) Commit(ctx context.Context, kind, id string, yamlBytes []byte, actor, reason string) (Version, error) {
	doc, problems, err := e.validate(ctx, kind, yamlBytes, nil)
	if err != nil {
		return Version{}, err
	}
	if metaID, ok := docID(doc); !ok || metaID != id {
		return Version{}, fmt.Errorf("%w: manifest metadata.id %q does not match %q", ErrConflict, metaID, id)
	}
	if p, err := e.checkActor(ctx, actor); err != nil {
		return Version{}, err
	} else if p != nil {
		problems = append(problems, *p)
	}
	if len(problems) > 0 {
		return Version{}, &ValidationError{Problems: problems}
	}
	if err := e.guardDoc(ctx, kind, id, doc); err != nil {
		return Version{}, err
	}

	// Compute next version number: use highest snapshot number (from ListVersions), not working copy number
	// Working copy has Number=0; snapshots have Number≥1. Always use highest snapshot + 1.
	versions, err := e.manifests.ListVersions(ctx, kind, id)
	if err != nil {
		return Version{}, err
	}
	number := 1
	if len(versions) > 0 {
		number = versions[len(versions)-1].Number + 1
	}
	v := Version{Kind: kind, ID: id, Number: number, YAML: yamlBytes, Actor: actor, Reason: reason, On: timeNow().UTC()}
	refs := extractRefs(doc, e.refRules[kind])
	storeRefs := make([]store.Ref, len(refs))
	for i, r := range refs {
		storeRefs[i] = store.Ref{Path: r.path, ToKind: r.kind, ToID: r.id}
	}

	err = e.manifests.WithinTransaction(ctx, func(ctx context.Context, tx store.ManifestStore) error {
		if err := tx.PutVersion(ctx, v); err != nil {
			return err
		}
		return tx.IndexReferences(ctx, kind, id, storeRefs)
	})
	if err != nil {
		return Version{}, err
	}
	e.forgetTeams(kind)
	e.afterVersion(ctx, v)
	return v, nil
}

// Snapshot validates a manifest and, if it is valid, stores it as a new
// immutable version with the reason and operator as the actor. Similar to
// Commit but used explicitly for creating snapshots. The manifest's
// metadata.id must match id. operator is recorded as the actor.
// For a Project with no state history, appends a "defined" state entry.
func (e *Engine) Snapshot(ctx context.Context, kind, id string, yamlBytes []byte, reason, operator string) (Version, error) {
	if reason == "" {
		return Version{}, &ValidationError{Problems: []Problem{{Message: "reason is required for a snapshot"}}}
	}

	// Commit creates the version
	v, err := e.Commit(ctx, kind, id, yamlBytes, operator, reason)
	if err != nil {
		return Version{}, err
	}

	// For Projects with no state history, auto-transition to "defined"
	if kind == "Project" {
		hist, err := e.ops.ListProjectStateHistory(ctx, id)
		if err != nil {
			return Version{}, err
		}
		if len(hist) == 0 {
			// No state history yet; append "defined" entry
			entry := store.ProjectStateEntry{
				ProjectID: id,
				State:     ProjectStateDefined,
				Actor:     operator,
				Reason:    "",
				On:        timeNow().UTC(),
			}
			if err := e.ops.PutProjectStateTransition(ctx, entry); err != nil {
				return Version{}, err
			}
		}
	}

	e.afterVersion(ctx, v)
	return v, nil
}

// checkActor returns a Problem (not an error) when actor is rejected, so
// callers can fold it into a ValidationError alongside other problems.
//
// Cartograph is a
// single-person application synchronised through shared manifests, not a
// shared platform, so the interface no longer asks "who is doing this" at
// all -- every write from the SPA carries the literal actor "local". The
// API accepts any actor unconditionally (actor validation was removed
// entirely, so the interface never validates against any directory).
func (e *Engine) checkActor(ctx context.Context, actor string) (*Problem, error) {
	_ = ctx
	_ = actor
	return nil, nil
}

// Get returns the current version of a manifest.
func (e *Engine) Get(ctx context.Context, kind, id string) (Version, error) {
	if _, ok := kinds.ByName(kind); !ok {
		return Version{}, fmt.Errorf("%w: %s", ErrUnknownKind, kind)
	}
	v, found, err := e.manifests.GetCurrent(ctx, kind, id)
	if err != nil {
		return Version{}, err
	}
	if !found {
		return Version{}, fmt.Errorf("%w: %s/%s", ErrNotFound, kind, id)
	}
	// A vault file can still be in last release's shape; every reader sees
	// the current one.
	v.YAML = e.normalizeLegacy(kind, v.YAML)
	return v, nil
}

// List returns summaries of every manifest of a kind matching f. Sorted by
// id throughout, the same order ListSummaries's own committed results already
// use.
func (e *Engine) List(ctx context.Context, kind string, f Filter, includeDrafts bool) ([]Summary, error) {
	if _, ok := kinds.ByName(kind); !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownKind, kind)
	}
	filters := make([]store.RefFilter, len(f.Refs))
	for i, r := range f.Refs {
		filters[i] = store.RefFilter{Kind: r.Kind, ID: r.ID}
	}
	return e.manifests.ListSummaries(ctx, kind, f.Q, filters)
}

// References returns what a manifest points to (Outgoing) and what points
// back at it (Incoming).
func (e *Engine) References(ctx context.Context, kind, id string) (Refs, error) {
	if _, ok := kinds.ByName(kind); !ok {
		return Refs{}, fmt.Errorf("%w: %s", ErrUnknownKind, kind)
	}
	out, err := e.manifests.ListReferencedBy(ctx, kind, id)
	if err != nil {
		return Refs{}, err
	}
	in, err := e.manifests.ListReferencing(ctx, kind, id)
	if err != nil {
		return Refs{}, err
	}
	outgoing := make([]Ref, len(out))
	for i, r := range out {
		outgoing[i] = Ref{Kind: r.ToKind, ID: r.ToID, Path: r.Path}
	}
	return Refs{Outgoing: outgoing, Incoming: in}, nil
}

// Versions returns every version of a manifest, oldest first.
func (e *Engine) Versions(ctx context.Context, kind, id string) ([]Version, error) {
	if _, ok := kinds.ByName(kind); !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownKind, kind)
	}
	return e.manifests.ListVersions(ctx, kind, id)
}

// ListAllVersions returns every version across all kinds, newest first, with pagination.
func (e *Engine) ListAllVersions(ctx context.Context, limit int, cursor string) ([]store.Version, string, error) {
	return e.manifests.ListAllVersions(ctx, limit, cursor)
}

// GetVersion returns one specific historical version.
func (e *Engine) GetVersion(ctx context.Context, kind, id string, number int) (Version, error) {
	if _, ok := kinds.ByName(kind); !ok {
		return Version{}, fmt.Errorf("%w: %s", ErrUnknownKind, kind)
	}
	v, found, err := e.manifests.GetVersion(ctx, kind, id, number)
	if err != nil {
		return Version{}, err
	}
	if !found {
		return Version{}, fmt.Errorf("%w: %s/%s#%d", ErrNotFound, kind, id, number)
	}
	return v, nil
}

// Diff computes the structural difference between two versions of a
// manifest.
func (e *Engine) Diff(ctx context.Context, kind, id string, from, to int) ([]Change, error) {
	fv, err := e.GetVersion(ctx, kind, id, from)
	if err != nil {
		return nil, err
	}
	tv, err := e.GetVersion(ctx, kind, id, to)
	if err != nil {
		return nil, err
	}
	var fd, td map[string]any
	if err := e.codec.DecodeInto(fv.YAML, &fd); err != nil {
		return nil, fmt.Errorf("parse version %d: %w", from, err)
	}
	if err := e.codec.DecodeInto(tv.YAML, &td); err != nil {
		return nil, fmt.Errorf("parse version %d: %w", to, err)
	}
	return diffValues("", fd, td), nil
}

// GetWorking returns a manifest's working copy (the file on disk that may
// not yet be versioned). Returns found=false when no working copy exists.
func (e *Engine) GetWorking(ctx context.Context, kind, id string) ([]byte, bool, error) {
	return e.manifests.GetWorking(ctx, kind, id)
}

// DiscardWorking throws away a staged draft, putting back whatever the
// store holds as saved. Stores that keep no staging directory have nothing
// to discard, so this is a no-op for them rather than an error: the caller
// asked for the draft to be gone, and it is.
func (e *Engine) DiscardWorking(ctx context.Context, kind, id string) error {
	discarder, ok := e.manifests.(interface {
		DiscardWorking(ctx context.Context, kind, id string) error
	})
	if !ok {
		return nil
	}
	// Discarding puts the saved version back: a change from the draft to it.
	if v, found, err := e.manifests.GetCurrent(ctx, kind, id); err != nil {
		return err
	} else if found {
		if err := e.guardText(ctx, kind, id, v.YAML); err != nil {
			return err
		}
	}
	if err := discarder.DiscardWorking(ctx, kind, id); err != nil {
		return err
	}
	// The shared draft goes back to what is saved too, for everyone.
	if e.shared != nil {
		if v, found, err := e.manifests.GetCurrent(ctx, kind, id); err == nil && found {
			if doc, err := e.codec.Decode(v.YAML); err == nil {
				return e.shared.Reconcile(ctx, kind, id, doc, "draft discarded")
			}
		}
	}
	return nil
}

// PutWorking writes a manifest's YAML as the working copy without creating
// a version. Used for autosave writes that don't create an explicit snapshot.
// It also re-indexes the manifest's outgoing references.
func (e *Engine) PutWorking(ctx context.Context, kind, id string, yamlBytes []byte) error {
	if err := e.guardText(ctx, kind, id, yamlBytes); err != nil {
		return err
	}
	// Write the working copy
	if err := e.manifests.PutWorking(ctx, kind, id, yamlBytes); err != nil {
		return err
	}

	// Re-index references from the working copy
	var doc map[string]any
	if err := e.codec.DecodeInto(yamlBytes, &doc); err != nil {
		// Working copy should already be written even if YAML is invalid
		// (validation happens in PutManifest/Snapshot, not PutWorking)
		return nil
	}

	refs := extractRefs(doc, e.refRules[kind])
	storeRefs := make([]store.Ref, len(refs))
	for i, r := range refs {
		storeRefs[i] = store.Ref{Path: r.path, ToKind: r.kind, ToID: r.id}
	}

	return e.manifests.IndexReferences(ctx, kind, id, storeRefs)
}

// Delete excludes a manifest from the live state. Allowed only when nothing
// currently references it (leaf rule). Returns ValidationError listing every
// referencing manifest when the manifest is not a leaf; ErrNotFound when the
// manifest does not exist.
func (e *Engine) Delete(ctx context.Context, kind, id, actor, reason string) error {
	v, found, err := e.manifests.GetCurrent(ctx, kind, id)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%w: %s/%s", ErrNotFound, kind, id)
	}
	if err := e.guardDoc(ctx, kind, id, nil); err != nil {
		return err
	}
	refs, err := e.manifests.ListReferencing(ctx, kind, id)
	if err != nil {
		return err
	}
	if len(refs) > 0 {
		problems := make([]Problem, len(refs))
		for i, r := range refs {
			problems[i] = Problem{Message: fmt.Sprintf("%s/%s (%s) still references this manifest", r.Kind, r.ID, r.Name)}
		}
		return &ValidationError{Problems: problems}
	}

	// Extract name from the YAML for the exclusion record
	var doc struct {
		Metadata struct {
			Name string `yaml:"name"`
		} `yaml:"metadata"`
	}
	if err := e.codec.DecodeInto(v.YAML, &doc); err != nil {
		doc.Metadata.Name = id
	}

	// Exclude the manifest from vault.yaml
	return e.manifests.Exclude(ctx, kind, id, doc.Metadata.Name, reason, actor)
}

// indexWorking tells the index a manifest exists, without staging it as
// somebody's draft. Stores whose PutWorking is already only an index write
// (memory, sqlite) get that; a vault gets the index-only path.
func (e *Engine) indexWorking(ctx context.Context, kind, id string, yamlBytes []byte) error {
	if indexer, ok := e.manifests.(interface {
		IndexWorking(ctx context.Context, kind, id string, yamlBytes []byte) error
	}); ok {
		return indexer.IndexWorking(ctx, kind, id, yamlBytes)
	}
	return e.manifests.PutWorking(ctx, kind, id, yamlBytes)
}

// Reindex walks every current manifest and indexes its outgoing references.
// Called after the vault rehydrates to rebuild the reference index from scratch.
// Also stores working copies so ListReferencing can find them.
func (e *Engine) Reindex(ctx context.Context) error {
	// Reindex all manifests of all kinds
	for _, spec := range kinds.All {
		summaries, err := e.manifests.ListSummaries(ctx, spec.Name, "", nil)
		if err != nil {
			return fmt.Errorf("list %s: %w", spec.Name, err)
		}

		for _, summary := range summaries {
			v, found, err := e.manifests.GetCurrent(ctx, spec.Name, summary.ID)
			if err != nil {
				return fmt.Errorf("get current %s/%s: %w", spec.Name, summary.ID, err)
			}
			if !found {
				continue
			}

			// The index's working-copy table is what ListReferencing reads,
			// so it has to know about every manifest. For a vault that is a
			// separate call from staging a draft: writing a draft of
			// everything on open is not what reindexing means.
			if err := e.indexWorking(ctx, spec.Name, summary.ID, v.YAML); err != nil {
				return fmt.Errorf("index working %s/%s: %w", spec.Name, summary.ID, err)
			}

			// Extract and index references (same logic as engine.PutWorking)
			var doc map[string]any
			if err := e.codec.DecodeInto(v.YAML, &doc); err != nil {
				// Skip manifests with invalid YAML
				continue
			}

			refs := extractRefs(doc, e.refRules[spec.Name])
			storeRefs := make([]store.Ref, len(refs))
			for i, r := range refs {
				storeRefs[i] = store.Ref{Path: r.path, ToKind: r.kind, ToID: r.id}
			}

			if err := e.manifests.IndexReferences(ctx, spec.Name, summary.ID, storeRefs); err != nil {
				return fmt.Errorf("index %s/%s: %w", spec.Name, summary.ID, err)
			}
		}
	}

	return nil
}

// afterVersion runs once a version is written and tells its listeners.
// The shared draft carries on unchanged: a version is read from it, never
// a reset of it (docs/adr/0007).
func (e *Engine) afterVersion(_ context.Context, v Version) {
	e.bus.Publish(Event{Type: "version", Kind: v.Kind, ID: v.ID, On: v.On, Seq: int64(v.Number), Actor: v.Actor})
}
