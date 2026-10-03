package memory

import (
	"context"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

var _ store.ChangeSetStore = (*ManifestStore)(nil)

// changeSets keeps the memory store's change sets; its zero value is ready.
type changeSets struct {
	mu    sync.Mutex
	byID  map[string]store.ChangeSet
	items map[string]map[string]store.ChangeItem // set -> kind/id -> item
}

func cloneChangeSet(cs store.ChangeSet) store.ChangeSet {
	cs.Waivers = slices.Clone(cs.Waivers)
	return cs
}

func cloneItem(it store.ChangeItem) store.ChangeItem {
	it.Text = slices.Clone(it.Text)
	return it
}

// PutChangeSet keeps a change set.
func (m *ManifestStore) PutChangeSet(_ context.Context, cs store.ChangeSet) error {
	m.sets.mu.Lock()
	defer m.sets.mu.Unlock()
	if m.sets.byID == nil {
		m.sets.byID = map[string]store.ChangeSet{}
	}
	m.sets.byID[cs.ID] = cloneChangeSet(cs)
	return nil
}

// GetChangeSet returns one change set.
func (m *ManifestStore) GetChangeSet(_ context.Context, id string) (store.ChangeSet, error) {
	m.sets.mu.Lock()
	defer m.sets.mu.Unlock()
	cs, ok := m.sets.byID[id]
	if !ok {
		return store.ChangeSet{}, store.ErrNoChangeSet
	}
	return cloneChangeSet(cs), nil
}

// ListChangeSets returns change sets, newest first.
func (m *ManifestStore) ListChangeSets(_ context.Context, f store.ChangeSetFilter) ([]store.ChangeSet, error) {
	m.sets.mu.Lock()
	defer m.sets.mu.Unlock()
	out := []store.ChangeSet{}
	for _, cs := range m.sets.byID {
		if (f.For == "" || cs.For == f.For) && (f.Owner == "" || cs.Owner == f.Owner) && (f.Status == "" || cs.Status == f.Status) {
			out = append(out, cloneChangeSet(cs))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].At.Equal(out[j].At) {
			return out[i].At.After(out[j].At)
		}
		return out[i].ID > out[j].ID
	})
	return out, nil
}

// MoveChangeSet moves a change set from one status to another.
func (m *ManifestStore) MoveChangeSet(_ context.Context, id, from, to, by, reason string, at time.Time) (store.ChangeSet, error) {
	m.sets.mu.Lock()
	defer m.sets.mu.Unlock()
	cs, ok := m.sets.byID[id]
	if !ok {
		return store.ChangeSet{}, store.ErrNoChangeSet
	}
	if cs.Status != from {
		return store.ChangeSet{}, store.ErrChangeSetMoved
	}
	cs.Status, cs.Updated = to, at
	if by != "" {
		cs.DecidedBy, cs.DecidedAt, cs.DecisionReason = by, at, reason
	}
	m.sets.byID[id] = cs
	return cloneChangeSet(cs), nil
}

// PutChangeItem keeps an item.
func (m *ManifestStore) PutChangeItem(_ context.Context, it store.ChangeItem) error {
	m.sets.mu.Lock()
	defer m.sets.mu.Unlock()
	if m.sets.items == nil {
		m.sets.items = map[string]map[string]store.ChangeItem{}
	}
	if m.sets.items[it.Set] == nil {
		m.sets.items[it.Set] = map[string]store.ChangeItem{}
	}
	m.sets.items[it.Set][it.Kind+"/"+it.ID] = cloneItem(it)
	return nil
}

// GetChangeItem returns one item.
func (m *ManifestStore) GetChangeItem(_ context.Context, set, kind, id string) (store.ChangeItem, bool, error) {
	m.sets.mu.Lock()
	defer m.sets.mu.Unlock()
	it, ok := m.sets.items[set][kind+"/"+id]
	return cloneItem(it), ok, nil
}

// ListChangeItems returns a change set's items, oldest first.
func (m *ManifestStore) ListChangeItems(_ context.Context, set string) ([]store.ChangeItem, error) {
	m.sets.mu.Lock()
	defer m.sets.mu.Unlock()
	out := []store.ChangeItem{}
	for _, it := range m.sets.items[set] {
		out = append(out, cloneItem(it))
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].At.Equal(out[j].At) {
			return out[i].At.Before(out[j].At)
		}
		return out[i].Kind+"/"+out[i].ID < out[j].Kind+"/"+out[j].ID
	})
	return out, nil
}

// DeleteChangeItem drops an item.
func (m *ManifestStore) DeleteChangeItem(_ context.Context, set, kind, id string) error {
	m.sets.mu.Lock()
	defer m.sets.mu.Unlock()
	delete(m.sets.items[set], kind+"/"+id)
	return nil
}
