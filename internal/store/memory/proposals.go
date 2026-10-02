package memory

import (
	"context"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

var _ store.ProposalStore = (*ManifestStore)(nil)

// proposals keeps the memory store's proposals; its zero value is ready.
type proposals struct {
	mu   sync.Mutex
	byID map[string]store.Proposal
}

func cloneProposal(p store.Proposal) store.Proposal {
	p.Text = slices.Clone(p.Text)
	p.Item = slices.Clone(p.Item)
	p.Waivers = slices.Clone(p.Waivers)
	return p
}

// PutProposal keeps a proposal.
func (m *ManifestStore) PutProposal(_ context.Context, p store.Proposal) error {
	m.props.mu.Lock()
	if m.props.byID == nil {
		m.props.byID = map[string]store.Proposal{}
	}
	m.props.byID[p.ID] = cloneProposal(p)
	m.props.mu.Unlock()
	m.mu.Lock()
	m.eventLocked(store.Event{At: p.At, Type: "proposal", Kind: p.Kind, ID: p.ManifestID, Detail: p.Op, Actor: p.Agent})
	m.mu.Unlock()
	return nil
}

// GetProposal returns one proposal.
func (m *ManifestStore) GetProposal(_ context.Context, id string) (store.Proposal, error) {
	m.props.mu.Lock()
	defer m.props.mu.Unlock()
	p, ok := m.props.byID[id]
	if !ok {
		return store.Proposal{}, store.ErrNoProposal
	}
	return cloneProposal(p), nil
}

// ListProposals returns proposals, newest first.
func (m *ManifestStore) ListProposals(_ context.Context, f store.ProposalFilter) ([]store.Proposal, error) {
	m.props.mu.Lock()
	defer m.props.mu.Unlock()
	out := []store.Proposal{}
	for _, p := range m.props.byID {
		if (f.For == "" || p.For == f.For) && (f.Kind == "" || p.Kind == f.Kind) &&
			(f.ManifestID == "" || p.ManifestID == f.ManifestID) && (f.Status == "" || p.Status == f.Status) && (f.Set == "" || p.Set == f.Set) {
			out = append(out, cloneProposal(p))
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

// DecideProposal decides an open proposal.
func (m *ManifestStore) DecideProposal(_ context.Context, id, status, by, reason string, at time.Time, version int) (store.Proposal, error) {
	m.props.mu.Lock()
	p, ok := m.props.byID[id]
	if !ok {
		m.props.mu.Unlock()
		return store.Proposal{}, store.ErrNoProposal
	}
	if p.Status != store.ProposalOpen {
		m.props.mu.Unlock()
		return store.Proposal{}, store.ErrProposalDecided
	}
	p.Status, p.DecidedBy, p.DecisionReason, p.DecidedAt, p.Version = status, by, reason, at, version
	m.props.byID[id] = p
	m.props.mu.Unlock()
	m.mu.Lock()
	m.eventLocked(store.Event{At: at, Type: "proposal", Kind: p.Kind, ID: p.ManifestID, Detail: status, Actor: by})
	m.mu.Unlock()
	return cloneProposal(p), nil
}

// DecideProposalSet decides every proposal of a set, or none.
func (m *ManifestStore) DecideProposalSet(_ context.Context, set, status, by, reason string, at time.Time, versions map[string]int) ([]store.Proposal, error) {
	m.props.mu.Lock()
	var members []store.Proposal
	for _, p := range m.props.byID {
		if set != "" && p.Set == set {
			members = append(members, p)
		}
	}
	if len(members) == 0 {
		m.props.mu.Unlock()
		return nil, store.ErrNoProposal
	}
	for _, p := range members {
		if p.Status != store.ProposalOpen {
			m.props.mu.Unlock()
			return nil, store.ErrProposalDecided
		}
	}
	sort.Slice(members, func(i, j int) bool { return members[i].SetIndex < members[j].SetIndex })
	for i, p := range members {
		p.Status, p.DecidedBy, p.DecisionReason, p.DecidedAt, p.Version = status, by, reason, at, versions[p.ID]
		m.props.byID[p.ID] = p
		members[i] = cloneProposal(p)
	}
	m.props.mu.Unlock()
	m.mu.Lock()
	for _, p := range members {
		m.eventLocked(store.Event{At: at, Type: "proposal", Kind: p.Kind, ID: p.ManifestID, Detail: status, Actor: by})
	}
	m.mu.Unlock()
	return members, nil
}
