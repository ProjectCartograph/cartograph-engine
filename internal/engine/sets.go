package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/kinds"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

// The engine's reads of many manifests at once. Each asks the store for
// the set in one call when it can (store.SetReader), and otherwise loops
// over the per-manifest calls every store has (docs/adr/0012).

// currentOfKind returns the current version of every manifest of a kind,
// ordered by id.
func currentOfKind(ctx context.Context, s store.ManifestStore, kind string) ([]Version, error) {
	if sr, ok := s.(store.SetReader); ok {
		return sr.CurrentOfKind(ctx, kind)
	}
	ids, err := s.ListIDs(ctx, kind)
	if err != nil {
		return nil, err
	}
	out := make([]Version, 0, len(ids))
	for _, id := range ids {
		v, found, err := s.GetCurrent(ctx, kind, id)
		if err != nil {
			return nil, err
		}
		if found {
			out = append(out, v)
		}
	}
	return out, nil
}

// referencingKind returns who references each manifest of toKind, keyed
// by its id. ids names the manifests to ask about when the store cannot
// answer for the kind in one call.
func referencingKind(ctx context.Context, s store.ManifestStore, toKind string, ids []string) (map[string][]Summary, error) {
	if sr, ok := s.(store.SetReader); ok {
		return sr.ReferencingKind(ctx, toKind)
	}
	out := map[string][]Summary{}
	for _, id := range ids {
		refs, err := s.ListReferencing(ctx, toKind, id)
		if err != nil {
			return nil, err
		}
		if len(refs) > 0 {
			out[id] = refs
		}
	}
	return out, nil
}

// latestNumber returns a manifest's highest version number, 0 when it
// has none. A working copy has no number of its own.
func latestNumber(ctx context.Context, s store.ManifestStore, kind, id string) (int, error) {
	if vc, ok := s.(store.VersionCounter); ok {
		return vc.LatestNumber(ctx, kind, id)
	}
	versions, err := s.ListVersions(ctx, kind, id)
	if err != nil || len(versions) == 0 {
		return 0, err
	}
	return versions[len(versions)-1].Number, nil
}

// docJSON is a decoded manifest as JSON, for a store that keeps
// documents (Version.Doc). Nil when it cannot be written as JSON, which
// leaves the store to treat the text alone as the record.
func docJSON(doc map[string]any) []byte {
	if doc == nil {
		return nil
	}
	b, err := json.Marshal(doc)
	if err != nil {
		return nil
	}
	return b
}

// textDoc decodes text and returns its document as JSON, or nil.
func (e *Engine) textDoc(text []byte) []byte {
	doc, err := e.codec.Decode(text)
	if err != nil {
		return nil
	}
	return docJSON(doc)
}

// putWorking writes a working copy, with its document where the store
// keeps documents.
func (e *Engine) putWorking(ctx context.Context, kind, id string, text []byte) error {
	if dw, ok := e.manifests.(store.DocWorkingStore); ok {
		return dw.PutWorkingDoc(ctx, kind, id, text, e.textDoc(text))
	}
	return e.manifests.PutWorking(ctx, kind, id, text)
}

// RepairDocs gives a document to every version and working copy a store
// holds as text alone, a batch at a time, and returns how many it gave.
// A store that always has its documents needs nothing, and gets nothing.
func (e *Engine) RepairDocs(ctx context.Context) (int, error) {
	r, ok := e.manifests.(store.DocRepairer)
	if !ok {
		return 0, nil
	}
	total := 0
	for {
		stale, err := r.StaleDocs(ctx, 500)
		if err != nil || len(stale) == 0 {
			return total, err
		}
		fixed := make([]Version, 0, len(stale))
		for _, v := range stale {
			// Text that does not decode keeps an empty document, so it is
			// not offered again: the text stays the record of what was
			// written, as it is in every adapter.
			doc := e.textDoc(v.YAML)
			if doc == nil {
				doc = []byte("{}")
			}
			v.Doc = doc
			fixed = append(fixed, v)
		}
		if err := r.PutDocs(ctx, fixed); err != nil {
			return total, err
		}
		total += len(fixed)
	}
}

// GetMany returns the current version of each of ids that exists, in id
// order: Get for a page of a list, in one read where the store can.
func (e *Engine) GetMany(ctx context.Context, kind string, ids []string) ([]Version, error) {
	if _, ok := kinds.ByName(kind); !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownKind, kind)
	}
	var out []Version
	if sr, ok := e.manifests.(store.SetReader); ok {
		vs, err := sr.CurrentMany(ctx, kind, ids)
		if err != nil {
			return nil, err
		}
		out = vs
	} else {
		sorted := append([]string(nil), ids...)
		sort.Strings(sorted)
		for _, id := range sorted {
			v, found, err := e.manifests.GetCurrent(ctx, kind, id)
			if err != nil {
				return nil, err
			}
			if found {
				out = append(out, v)
			}
		}
	}
	for i := range out {
		out[i].YAML = e.normalizeLegacy(kind, out[i].YAML)
	}
	return out, nil
}

// ProjectStates returns the current state of each project given, by id:
// GetProjectState for a list, without the history. A project with no
// history is a draft.
func (e *Engine) ProjectStates(ctx context.Context, ids []string) (map[string]string, error) {
	out := make(map[string]string, len(ids))
	if sr, ok := e.ops.(store.StateSetReader); ok {
		states, err := sr.CurrentStates(ctx, ids)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			out[id] = ProjectStateDraft
			if s, ok := states[id]; ok {
				out[id] = s
			}
		}
		return out, nil
	}
	for _, id := range ids {
		rows, err := e.ops.ListProjectStateHistory(ctx, id)
		if err != nil {
			return nil, err
		}
		out[id] = projectStateFromRows(rows).State
	}
	return out, nil
}

// ListPage returns one page of List: at most limit summaries with ids
// after afterID, and whether more follow. A store that pages reads only
// the page; any other is listed whole and sliced.
func (e *Engine) ListPage(ctx context.Context, kind string, f Filter, afterID string, limit int) ([]Summary, bool, error) {
	if _, ok := kinds.ByName(kind); !ok {
		return nil, false, fmt.Errorf("%w: %s", ErrUnknownKind, kind)
	}
	filters := make([]store.RefFilter, len(f.Refs))
	for i, r := range f.Refs {
		filters[i] = store.RefFilter{Kind: r.Kind, ID: r.ID}
	}
	var page []Summary
	if p, ok := e.manifests.(store.SummaryPager); ok {
		got, err := p.ListSummariesAfter(ctx, kind, f.Q, filters, afterID, limit+1)
		if err != nil {
			return nil, false, err
		}
		page = got
	} else {
		all, err := e.manifests.ListSummaries(ctx, kind, f.Q, filters)
		if err != nil {
			return nil, false, err
		}
		start := sort.Search(len(all), func(i int) bool { return all[i].ID > afterID })
		page = all[start:min(start+limit+1, len(all))]
	}
	if len(page) > limit {
		return page[:limit], true, nil
	}
	return page, false, nil
}

// withText gives every version its text. A store that keeps history as
// documents returns a compacted version without text (docs/adr/0013);
// its text is then the document written by this deployment's codec,
// which is canonical rather than what the person typed.
func (e *Engine) withText(ctx context.Context, vs ...Version) ([]Version, error) {
	for i := range vs {
		if len(vs[i].YAML) > 0 || len(vs[i].Doc) == 0 {
			continue
		}
		// JSON is YAML, so either codec reads the document.
		doc, err := e.codec.Decode(vs[i].Doc)
		if err != nil {
			return nil, fmt.Errorf("version %d of %s/%s: %w", vs[i].Number, vs[i].Kind, vs[i].ID, err)
		}
		// Its series, as they stood when it was saved.
		if err := e.withSeries(ctx, vs[i], doc); err != nil {
			return nil, fmt.Errorf("version %d of %s/%s: %w", vs[i].Number, vs[i].Kind, vs[i].ID, err)
		}
		if vs[i].YAML, err = e.codec.Encode(doc); err != nil {
			return nil, fmt.Errorf("version %d of %s/%s: %w", vs[i].Number, vs[i].Kind, vs[i].ID, err)
		}
	}
	return vs, nil
}

// CurrentOfKind returns every manifest of a kind as it stands now (a
// working copy where there is one), in id order, in one read where the
// store can: what a front door that reads whole kinds needs, a report
// or an agent.
func (e *Engine) CurrentOfKind(ctx context.Context, kind string) ([]Version, error) {
	if _, ok := kinds.ByName(kind); !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownKind, kind)
	}
	vs, err := currentOfKind(ctx, e.manifests, kind)
	if err != nil {
		return nil, err
	}
	for i := range vs {
		vs[i].YAML = e.normalizeLegacy(kind, vs[i].YAML)
	}
	return vs, nil
}

// ReferencingKind returns, for every manifest of a kind that something
// references, what references it, by id: References for a whole kind.
func (e *Engine) ReferencingKind(ctx context.Context, kind string) (map[string][]Summary, error) {
	if _, ok := kinds.ByName(kind); !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownKind, kind)
	}
	var ids []string
	if _, ok := e.manifests.(store.SetReader); !ok {
		var err error
		if ids, err = e.manifests.ListIDs(ctx, kind); err != nil {
			return nil, err
		}
	}
	return referencingKind(ctx, e.manifests, kind, ids)
}
