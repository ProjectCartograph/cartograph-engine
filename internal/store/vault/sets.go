package vault

import (
	"context"
	"sort"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

// The vault holds every current manifest in memory, so the engine's loop
// over GetCurrent is already free here and the vault does not answer
// CurrentOfKind. What it reads from its index, it asks of the index in
// one call when the index can answer so.

// ReferencingKind returns who references each manifest of toKind.
func (m *ManifestStore) ReferencingKind(ctx context.Context, toKind string) (map[string][]store.Summary, error) {
	if sr, ok := m.index.(store.SetReader); ok {
		return sr.ReferencingKind(ctx, toKind)
	}
	ids, err := m.ListIDs(ctx, toKind)
	if err != nil {
		return nil, err
	}
	out := map[string][]store.Summary{}
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

// CurrentOfKind returns the current version of every manifest of a
// kind, from the vault's memory.
func (m *ManifestStore) CurrentOfKind(ctx context.Context, kind string) ([]store.Version, error) {
	ids, err := m.ListIDs(ctx, kind)
	if err != nil {
		return nil, err
	}
	out := make([]store.Version, 0, len(ids))
	for _, id := range ids {
		if v, found, err := m.GetCurrent(ctx, kind, id); err != nil {
			return nil, err
		} else if found {
			out = append(out, v)
		}
	}
	return out, nil
}

// CurrentMany returns the current version of each of ids that exists,
// from the vault's memory.
func (m *ManifestStore) CurrentMany(ctx context.Context, kind string, ids []string) ([]store.Version, error) {
	out := []store.Version{}
	for _, id := range ids {
		if v, found, err := m.GetCurrent(ctx, kind, id); err != nil {
			return nil, err
		} else if found {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// LatestNumber asks the index for the highest version number.
func (m *ManifestStore) LatestNumber(ctx context.Context, kind, id string) (int, error) {
	if vc, ok := m.index.(store.VersionCounter); ok {
		return vc.LatestNumber(ctx, kind, id)
	}
	versions, err := m.index.ListVersions(ctx, kind, id)
	if err != nil || len(versions) == 0 {
		return 0, err
	}
	return versions[len(versions)-1].Number, nil
}

// LatestNumber, in a transaction, asks the index transaction.
func (tx *txStore) LatestNumber(ctx context.Context, kind, id string) (int, error) {
	if vc, ok := tx.index.(store.VersionCounter); ok {
		return vc.LatestNumber(ctx, kind, id)
	}
	versions, err := tx.index.ListVersions(ctx, kind, id)
	if err != nil || len(versions) == 0 {
		return 0, err
	}
	return versions[len(versions)-1].Number, nil
}

// The vault keeps series items and events in its index, which records
// them in the same transaction as the versions it indexes.

// RecordSeries asks the index.
func (m *ManifestStore) RecordSeries(ctx context.Context, items []store.SeriesItem) error {
	ss, ok := m.index.(store.SeriesStore)
	if !ok {
		return nil
	}
	return ss.RecordSeries(ctx, items)
}

// SeriesAsOf asks the index.
func (m *ManifestStore) SeriesAsOf(ctx context.Context, kind, series string, ids []string, at time.Time) (map[string][]store.SeriesItem, error) {
	ss, ok := m.index.(store.SeriesStore)
	if !ok {
		return map[string][]store.SeriesItem{}, nil
	}
	return ss.SeriesAsOf(ctx, kind, series, ids, at)
}

// Events asks the index.
func (m *ManifestStore) Events(ctx context.Context, after int64, limit int) ([]store.Event, error) {
	el, ok := m.index.(store.EventLog)
	if !ok {
		return []store.Event{}, nil
	}
	return el.Events(ctx, after, limit)
}

// RecordSeries, in a transaction, asks the index transaction.
func (tx *txStore) RecordSeries(ctx context.Context, items []store.SeriesItem) error {
	ss, ok := tx.index.(store.SeriesStore)
	if !ok {
		return nil
	}
	return ss.RecordSeries(ctx, items)
}

// SeriesAsOf, in a transaction, asks the index transaction.
func (tx *txStore) SeriesAsOf(ctx context.Context, kind, series string, ids []string, at time.Time) (map[string][]store.SeriesItem, error) {
	ss, ok := tx.index.(store.SeriesStore)
	if !ok {
		return map[string][]store.SeriesItem{}, nil
	}
	return ss.SeriesAsOf(ctx, kind, series, ids, at)
}

// The vault keeps proposals in its index.

func (m *ManifestStore) proposals() (store.ProposalStore, error) {
	ps, ok := m.index.(store.ProposalStore)
	if !ok {
		return nil, store.ErrNoProposal
	}
	return ps, nil
}

// PutProposal asks the index.
func (m *ManifestStore) PutProposal(ctx context.Context, p store.Proposal) error {
	ps, err := m.proposals()
	if err != nil {
		return err
	}
	return ps.PutProposal(ctx, p)
}

// GetProposal asks the index.
func (m *ManifestStore) GetProposal(ctx context.Context, id string) (store.Proposal, error) {
	ps, err := m.proposals()
	if err != nil {
		return store.Proposal{}, err
	}
	return ps.GetProposal(ctx, id)
}

// ListProposals asks the index.
func (m *ManifestStore) ListProposals(ctx context.Context, f store.ProposalFilter) ([]store.Proposal, error) {
	ps, err := m.proposals()
	if err != nil {
		return []store.Proposal{}, nil
	}
	return ps.ListProposals(ctx, f)
}

// DecideProposal asks the index.
func (m *ManifestStore) DecideProposal(ctx context.Context, id, status, by, reason string, at time.Time, version int) (store.Proposal, error) {
	ps, err := m.proposals()
	if err != nil {
		return store.Proposal{}, err
	}
	return ps.DecideProposal(ctx, id, status, by, reason, at, version)
}
