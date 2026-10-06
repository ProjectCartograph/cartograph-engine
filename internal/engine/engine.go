package engine

import (
	"context"
	"errors"
	"fmt"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/decide"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/identity"
	"sort"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/codec"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/crdt"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/fanout"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/kinds"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/kinds/kit"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/layout"
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
	layout    layout.Layout       // nil when the graph has no layout (WithLayout)
	codec     codec.Codec         // the manifest syntax; YAML unless told otherwise
	bus       Bus                 // event fan-out; in-process unless told otherwise
	crdt      crdt.Engine         // the CRDT under shared drafts (docs/adr/0007)
	docs      store.DocStore      // where shared drafts are kept
	fan       fanout.Bus          // hints between replicas (docs/adr/0008)
	shared    *Shared             // built when crdt, docs and fan are all set
	docCache  int                 // the shared-document cache bound; 0 means the default
	authz     identity.Authorizer // asked again at every write (docs/adr/0011); nil asks nothing
	access    store.AccessStore   // the access list, when access is by role and team
	directory Directory           // how directory groups map to roles and teams
	teamCache teamCache           // the team tree, for team chains
	schemas   *schemaSet
	refRules  map[string][]refRule
	// seriesRules are the series each kind's schema marks.
	seriesRules map[string][]seriesRule
	// decider answers typed questions about text (docs/adr/0023); nil
	// when the deployment chose none, and decisions caches its answers.
	decider   decide.Decider
	decisions *decisionCache
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
	series := map[string][]seriesRule{}
	for _, spec := range kinds.All {
		rules[spec.Name] = collectRefRules(ss.raw, spec.SchemaFile)
		series[spec.Name] = collectSeriesRules(ss.raw, spec.SchemaFile)
	}
	e := &Engine{manifests: manifests, ops: ops, schemas: ss, refRules: rules, seriesRules: series}
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
		info := KindInfo{Kind: spec.Name, Count: counts[spec.Name]}
		if g, ok, _ := GuideBundleFor(spec.Name, DefaultLocale); ok {
			info.Summary = g.Summary
		}
		out = append(out, info)
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

// SchemaDefs are the shared definitions a kind's schema points into
// (common.schema.json's Metadata, Ref, KeyResult and the rest), by name,
// followed through each other: so whoever reads the schema can see every
// field without resolving a reference.
func (e *Engine) SchemaDefs(kind string) (map[string]any, error) {
	doc, err := e.Schema(kind)
	if err != nil {
		return nil, err
	}
	common, _ := e.schemas.raw["common.schema.json"]["$defs"].(map[string]any)
	out := map[string]any{}
	var walk func(any)
	walk = func(n any) {
		switch v := n.(type) {
		case map[string]any:
			if ref, _ := v["$ref"].(string); ref != "" {
				name := ref[strings.LastIndex(ref, "/")+1:]
				if def, ok := common[name]; ok && out[name] == nil && strings.Contains(ref, "$defs/") {
					out[name] = def
					walk(def)
				}
			}
			for _, c := range v {
				walk(c)
			}
		case []any:
			for _, c := range v {
				walk(c)
			}
		}
	}
	walk(doc)
	return out, nil
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

	id, _ := docID(doc)
	if spec.Rules != nil {
		for _, p := range spec.Rules(doc, kit.RuleContext{ID: id, Lookup: l}) {
			problems = append(problems, Problem(p))
		}
	}
	// The record stays a directed acyclic graph (TAXONOMY.md D28). A kind
	// rule that already refuses a loop on a path has said it once. An
	// import brings in records as they were saved, so it is held to what
	// they held then.
	if overlay == nil {
		said := map[string]bool{}
		for _, p := range problems {
			said[p.Path] = true
		}
		for _, p := range e.orderProblems(kind, id, doc, l) {
			if !said[p.Path] {
				problems = append(problems, p)
			}
		}
	}
	problems = append(problems, e.pendingProblems(kind, doc)...)

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
				problems = append(problems, Problem{Path: u.InstanceLocation, Message: plainPattern(u.Error.String())})
			}
			return
		}
		// Under a choice of forms (oneOf, anyOf) every form's failures
		// come back, and the real one is buried among forms the value was
		// never meant to be: only the nearest form, the one with fewest
		// failures, is reported.
		if strings.HasSuffix(u.KeywordLocation, "/oneOf") || strings.HasSuffix(u.KeywordLocation, "/anyOf") {
			best, fewest := -1, 0
			for i, c := range u.Errors {
				if n := leaves(c); best < 0 || n < fewest {
					best, fewest = i, n
				}
			}
			if best >= 0 {
				walk(u.Errors[best])
				return
			}
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

// plainPattern says a date pattern's failure in words: how to write the
// value, rather than the regular expression it missed.
func plainPattern(msg string) string {
	for pattern, how := range map[string]string{
		"'^[0-9]{4}-(0[1-9]|1[0-2])$'":                             "write it as a year and month, YYYY-MM",
		"'^[0-9]{4}(-(0[1-9]|1[0-2]))?$'":                          "write it as a year, YYYY, or a year and month, YYYY-MM",
		"'^[0-9]{4}-(0[1-9]|1[0-2])(-(0[1-9]|[12][0-9]|3[01]))?$'": "write it as YYYY-MM-DD or YYYY-MM",
	} {
		if i := strings.Index(msg, "does not match pattern "+pattern); i >= 0 {
			return msg[:i] + "is not written as asked: " + how
		}
	}
	return msg
}

// leaves counts the failures under an output unit.
func leaves(u jsonschema.OutputUnit) int {
	if len(u.Errors) == 0 {
		if u.Error != nil {
			return 1
		}
		return 0
	}
	n := 0
	for _, c := range u.Errors {
		n += leaves(c)
	}
	return n
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
	if err := refuseAgent(ctx); err != nil {
		return Version{}, err
	}
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
	latest, err := latestNumber(ctx, e.manifests, kind, id)
	if err != nil {
		return Version{}, err
	}
	number := latest + 1
	v := Version{Kind: kind, ID: id, Number: number, YAML: yamlBytes, Actor: actor, Reason: reason, On: timeNow().UTC(), Doc: e.storedDoc(kind, doc)}
	refs := extractRefs(doc, e.refRules[kind])
	storeRefs := make([]store.Ref, len(refs))
	for i, r := range refs {
		storeRefs[i] = store.Ref{Path: r.path, ToKind: r.kind, ToID: r.id}
	}

	err = e.manifests.WithinTransaction(ctx, func(ctx context.Context, tx store.ManifestStore) error {
		if err := tx.PutVersion(ctx, v); err != nil {
			return err
		}
		if err := e.recordSeries(ctx, tx, v, doc); err != nil {
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
	if err := refuseAgent(ctx); err != nil {
		return Version{}, err
	}
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
	// Read as if a change set were accepted: its draft in place of the
	// version it started from, numbered as that version (docs/adr/0024).
	if text, ok := inPlay(ctx, kind, id); ok {
		number := 0
		if found {
			number = v.Number
		}
		return Version{Kind: kind, ID: id, Number: number, YAML: e.normalizeLegacy(kind, text)}, nil
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
	out, err := e.manifests.ListSummaries(ctx, kind, f.Q, filters)
	if err != nil {
		return nil, err
	}
	return e.withProposedSummaries(ctx, kind, f, out), nil
}

// withProposedSummaries lays a change set's drafts of a kind over a list,
// when the list is read as if the change set were accepted: a changed
// manifest takes its draft's name and is marked changed, a new one is
// added and marked new, each still held to the list's filters.
func (e *Engine) withProposedSummaries(ctx context.Context, kind string, f Filter, list []Summary) []Summary {
	refs := inPlayRefs(ctx)
	if len(refs) == 0 {
		return list
	}
	at := map[string]int{}
	for i, s := range list {
		at[s.ID] = i
	}
	q := strings.ToLower(strings.TrimSpace(f.Q))
	for _, r := range refs {
		if r.Kind != kind {
			continue
		}
		text, _ := inPlay(ctx, r.Kind, r.ID)
		var doc map[string]any
		if e.codec.DecodeInto(text, &doc) != nil {
			continue
		}
		name := r.ID
		if md, ok := doc["metadata"].(map[string]any); ok {
			if n, _ := md["name"].(string); n != "" {
				name = n
			}
		}
		if i, ok := at[r.ID]; ok {
			list[i].Name, list[i].Proposed = name, "changed"
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(name), q) && !strings.Contains(r.ID, q) {
			continue
		}
		if !namesAll(extractRefs(doc, e.refRules[kind]), f.Refs) {
			continue
		}
		list = append(list, Summary{Kind: kind, ID: r.ID, Name: name, Proposed: "new"})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	return list
}

// namesAll reports whether found references every one of want.
func namesAll(found []foundRef, want []Ref) bool {
	for _, w := range want {
		ok := false
		for _, f := range found {
			ok = ok || f.kind == w.Kind && f.id == w.ID
		}
		if !ok {
			return false
		}
	}
	return true
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
	var uses []Use
	for _, from := range in {
		refs, err := e.manifests.ListReferencedBy(ctx, from.Kind, from.ID)
		if err != nil {
			return Refs{}, err
		}
		for _, r := range refs {
			if r.ToKind == kind && r.ToID == id {
				uses = append(uses, Use{Kind: from.Kind, ID: from.ID, Name: from.Name, Path: r.Path, As: useOf(r.Path)})
			}
		}
	}
	return Refs{Outgoing: outgoing, Incoming: in, Uses: uses}, nil
}

// useOf names what a reference's place means for its target, for the
// three places a governance body is named (TAXONOMY.md D43).
func useOf(path string) string {
	switch {
	case strings.Contains(path, "/confirmedBy"):
		return "confirms"
	case strings.Contains(path, "/escalate/to"), strings.Contains(path, "/escalationRoute/"):
		return "receives"
	case strings.Contains(path, "/mandate/") && strings.Contains(path, "/issuer"):
		return "decided"
	}
	return ""
}

// Versions returns every version of a manifest, oldest first.
func (e *Engine) Versions(ctx context.Context, kind, id string) ([]Version, error) {
	if _, ok := kinds.ByName(kind); !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownKind, kind)
	}
	vs, err := e.manifests.ListVersions(ctx, kind, id)
	if err != nil {
		return nil, err
	}
	return e.withText(ctx, vs...)
}

// ListAllVersions returns every version across all kinds, newest first, with pagination.
func (e *Engine) ListAllVersions(ctx context.Context, limit int, cursor string) ([]store.Version, string, error) {
	vs, next, err := e.manifests.ListAllVersions(ctx, limit, cursor)
	if err != nil {
		return nil, "", err
	}
	vs, err = e.withText(ctx, vs...)
	return vs, next, err
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
	vs, err := e.withText(ctx, v)
	if err != nil {
		return Version{}, err
	}
	return vs[0], nil
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
	if err := refuseAgent(ctx); err != nil {
		return err
	}
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
	if err := e.putWorking(ctx, kind, id, yamlBytes); err != nil {
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
	if err := refuseAgent(ctx); err != nil {
		return err
	}
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
	return e.putWorking(ctx, kind, id, yamlBytes)
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
