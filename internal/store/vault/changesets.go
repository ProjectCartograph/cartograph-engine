package vault

import (
	"context"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

// The vault keeps change sets in its index, beside its proposals: they
// are drafts of work, not files of the record.

var _ store.ChangeSetStore = (*ManifestStore)(nil)

func (m *ManifestStore) changeSets() (store.ChangeSetStore, error) {
	cs, ok := m.index.(store.ChangeSetStore)
	if !ok {
		return nil, store.ErrNoChangeSet
	}
	return cs, nil
}

// PutChangeSet asks the index.
func (m *ManifestStore) PutChangeSet(ctx context.Context, cs store.ChangeSet) error {
	s, err := m.changeSets()
	if err != nil {
		return err
	}
	return s.PutChangeSet(ctx, cs)
}

// GetChangeSet asks the index.
func (m *ManifestStore) GetChangeSet(ctx context.Context, id string) (store.ChangeSet, error) {
	s, err := m.changeSets()
	if err != nil {
		return store.ChangeSet{}, err
	}
	return s.GetChangeSet(ctx, id)
}

// ListChangeSets asks the index.
func (m *ManifestStore) ListChangeSets(ctx context.Context, f store.ChangeSetFilter) ([]store.ChangeSet, error) {
	s, err := m.changeSets()
	if err != nil {
		return []store.ChangeSet{}, nil
	}
	return s.ListChangeSets(ctx, f)
}

// MoveChangeSet asks the index.
func (m *ManifestStore) MoveChangeSet(ctx context.Context, id, from, to, by, reason string, at time.Time) (store.ChangeSet, error) {
	s, err := m.changeSets()
	if err != nil {
		return store.ChangeSet{}, err
	}
	return s.MoveChangeSet(ctx, id, from, to, by, reason, at)
}

// PutChangeItem asks the index.
func (m *ManifestStore) PutChangeItem(ctx context.Context, it store.ChangeItem) error {
	s, err := m.changeSets()
	if err != nil {
		return err
	}
	return s.PutChangeItem(ctx, it)
}

// GetChangeItem asks the index.
func (m *ManifestStore) GetChangeItem(ctx context.Context, set, kind, id string) (store.ChangeItem, bool, error) {
	s, err := m.changeSets()
	if err != nil {
		return store.ChangeItem{}, false, err
	}
	return s.GetChangeItem(ctx, set, kind, id)
}

// ListChangeItems asks the index.
func (m *ManifestStore) ListChangeItems(ctx context.Context, set string) ([]store.ChangeItem, error) {
	s, err := m.changeSets()
	if err != nil {
		return []store.ChangeItem{}, nil
	}
	return s.ListChangeItems(ctx, set)
}

// DeleteChangeItem asks the index.
func (m *ManifestStore) DeleteChangeItem(ctx context.Context, set, kind, id string) error {
	s, err := m.changeSets()
	if err != nil {
		return err
	}
	return s.DeleteChangeItem(ctx, set, kind, id)
}
