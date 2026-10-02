package memory

import (
	"context"
	"sort"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

var (
	_ store.SetReader      = (*ManifestStore)(nil)
	_ store.VersionCounter = (*ManifestStore)(nil)
	_ store.StateSetReader = (*OperationalStore)(nil)
)

// CurrentOfKind returns the current version of every manifest of a kind.
// Memory has no round trips to save, so it answers from the
// per-manifest calls; it implements the set so the conformance suite
// checks the same answers here as in every other adapter.
func (m *ManifestStore) CurrentOfKind(ctx context.Context, kind string) ([]store.Version, error) {
	ids, err := m.ListIDs(ctx, kind)
	if err != nil {
		return nil, err
	}
	out := make([]store.Version, 0, len(ids))
	for _, id := range ids {
		v, found, err := m.GetCurrent(ctx, kind, id)
		if err != nil {
			return nil, err
		}
		if found {
			out = append(out, v)
		}
	}
	return out, nil
}

// ReferencingKind returns who references each manifest of toKind.
func (m *ManifestStore) ReferencingKind(ctx context.Context, toKind string) (map[string][]store.Summary, error) {
	m.mu.Lock()
	targets := map[string]bool{}
	for _, have := range m.refs {
		for _, r := range have {
			if r.ToKind == toKind {
				targets[r.ToID] = true
			}
		}
	}
	m.mu.Unlock()
	ids := make([]string, 0, len(targets))
	for id := range targets {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make(map[string][]store.Summary, len(ids))
	for _, id := range ids {
		refs, err := m.ListReferencing(ctx, toKind, id)
		if err != nil {
			return nil, err
		}
		if len(refs) > 0 {
			out[id] = refs
		}
	}
	return out, nil
}

// CurrentMany returns the current version of each of ids that exists.
func (m *ManifestStore) CurrentMany(ctx context.Context, kind string, ids []string) ([]store.Version, error) {
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	out := []store.Version{}
	for _, id := range sorted {
		v, found, err := m.GetCurrent(ctx, kind, id)
		if err != nil {
			return nil, err
		}
		if found {
			out = append(out, v)
		}
	}
	return out, nil
}

// CurrentStates returns the latest state of each project with history.
func (o *OperationalStore) CurrentStates(_ context.Context, projectIDs []string) (map[string]string, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := map[string]string{}
	for _, id := range projectIDs {
		if rows := o.projectStates[id]; len(rows) > 0 {
			out[id] = rows[len(rows)-1].State
		}
	}
	return out, nil
}

// LatestNumber returns the highest version number, 0 when there is none.
func (m *ManifestStore) LatestNumber(_ context.Context, kind, id string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.versions[manifestKey{kind, id}]), nil
}
