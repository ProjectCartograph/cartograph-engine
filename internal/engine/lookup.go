package engine

import (
	"context"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/codec"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

// lookup answers "does this manifest exist" and "give me every current
// document of this kind", checked first against an in-flight batch
// (overlay, used only during ImportDir so the members of one import can
// reference each other before any of them is committed) and then against
// the store. A nil overlay means "no batch in flight": every check goes
// straight to the store. lookup also satisfies kit.Lookup, so the same
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
	ids, err := l.store.ListIDs(l.ctx, kind)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		if _, already := out[id]; already {
			continue // the batch overlay wins over what is currently stored
		}
		v, found, err := l.store.GetCurrent(l.ctx, kind, id)
		if err != nil {
			return nil, err
		}
		if !found {
			continue
		}
		var doc map[string]any
		if err := l.codec.DecodeInto(v.YAML, &doc); err == nil {
			out[id] = doc
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
