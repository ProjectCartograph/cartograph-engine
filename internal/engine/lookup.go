package engine

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/codec"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

// lookup answers "does this manifest exist" and "give me every current
// document of this kind", checked first against an in-flight batch
// (overlay, used only during ImportDir so the members of one import can
// reference each other before any of them is committed) and then against
// the store. A nil overlay means "no batch in flight". Drafts being
// checked together on the context (proposedDocs) come next, so a check
// reads a change set's drafts in place of what is stored. lookup also satisfies kit.Lookup, so the same
// object serves both the generic reference checker and a kind's Rules
// function.
type lookup struct {
	ctx     context.Context
	store   store.ManifestStore
	codec   codec.Codec
	overlay map[string]map[string]map[string]any // kind -> id -> parsed document
}

func (l *lookup) exists(kind, id string) bool {
	if l.overlay != nil {
		if m, ok := l.overlay[kind]; ok {
			if _, ok := m[id]; ok {
				return true
			}
		}
	}
	if _, ok := proposedDocs(l.ctx)[kind+"/"+id]; ok {
		return true
	}
	_, found, err := l.store.GetCurrent(l.ctx, kind, id)
	return err == nil && found
}

// Documents implements kit.Lookup.
func (l *lookup) Documents(kind string) (map[string]map[string]any, error) {
	out := map[string]map[string]any{}
	if l.overlay != nil {
		for id, doc := range l.overlay[kind] {
			out[id] = doc
		}
	}
	// The drafts being checked together (a change set, a proposal) stand
	// in for their saved versions, so a check reads the record as it
	// will be, not as it was.
	for key, doc := range proposedDocs(l.ctx) {
		if id, ok := strings.CutPrefix(key, kind+"/"); ok {
			if _, already := out[id]; !already {
				out[id] = doc
			}
		}
	}
	// One read for the whole kind, where the store can answer it so: a
	// rule that needs every manifest of a kind runs at every save.
	versions, err := currentOfKind(l.ctx, l.store, kind)
	if err != nil {
		return nil, err
	}
	for _, v := range versions {
		if _, already := out[v.ID]; already {
			continue // the batch overlay wins over what is currently stored
		}
		var doc map[string]any
		if err := l.codec.DecodeInto(v.YAML, &doc); err == nil {
			out[v.ID] = doc
		}
	}
	return out, nil
}

// HasSnapshots implements kit.Lookup.
func (l *lookup) HasSnapshots(kind, id string) (bool, error) {
	versions, err := l.store.ListVersions(l.ctx, kind, id)
	if err != nil {
		return false, err
	}
	for _, v := range versions {
		if v.Number > 0 {
			return true, nil
		}
	}
	return false, nil
}

// currentDoc reads a manifest as a check must see it: the draft being
// checked with it where there is one (a change set's, a proposal's),
// else its current version. Every check that reads another manifest goes
// through here, so none reads the stored record behind a draft's back.
func (e *Engine) currentDoc(ctx context.Context, kind, id string) (map[string]any, bool, error) {
	if doc, ok := proposedDocs(ctx)[kind+"/"+id]; ok {
		return doc, true, nil
	}
	v, found, err := e.manifests.GetCurrent(ctx, kind, id)
	if err != nil || !found {
		return nil, found, err
	}
	var doc map[string]any
	if err := e.codec.DecodeInto(v.YAML, &doc); err != nil {
		return nil, false, fmt.Errorf("parse %s/%s: %w", kind, id, err)
	}
	return doc, true, nil
}

// referencing is every manifest that references kind/id, as a check must
// see it: the saved ones, less a draft that no longer names it, plus a
// draft that does.
func (e *Engine) referencing(ctx context.Context, kind, id string) ([]store.Summary, error) {
	saved, err := e.manifests.ListReferencing(ctx, kind, id)
	if err != nil {
		return nil, err
	}
	drafts := proposedDocs(ctx)
	if len(drafts) == 0 {
		return saved, nil
	}
	names := func(fromKind string, doc map[string]any) bool {
		for _, r := range extractRefs(doc, e.refRules[fromKind]) {
			if r.kind == kind && r.id == id {
				return true
			}
		}
		return false
	}
	var out []store.Summary
	seen := map[string]bool{}
	for _, s := range saved {
		key := s.Kind + "/" + s.ID
		seen[key] = true
		if doc, ok := drafts[key]; ok && !names(s.Kind, doc) {
			continue
		}
		out = append(out, s)
	}
	keys := make([]string, 0, len(drafts))
	for key := range drafts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fromKind, fromID, _ := strings.Cut(key, "/")
		if seen[key] || !names(fromKind, drafts[key]) {
			continue
		}
		out = append(out, store.Summary{Kind: fromKind, ID: fromID, Name: docName(drafts[key], fromID)})
	}
	return out, nil
}
