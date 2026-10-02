package memory

import (
	"context"
	"sort"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

func (s *AccessStore) PutAgentGrant(_ context.Context, t store.AgentGrant) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.grants == nil {
		s.grants = map[string]store.AgentGrant{}
	}
	s.grants[t.ID] = t
	return nil
}

func (s *AccessStore) GetAgentGrant(_ context.Context, id string) (store.AgentGrant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.grants[id]
	if !ok {
		return store.AgentGrant{}, store.ErrNoAgentGrant
	}
	return t, nil
}

func (s *AccessStore) ListAgentGrants(_ context.Context, email string) ([]store.AgentGrant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []store.AgentGrant{}
	for _, t := range s.grants {
		if email == "" || t.Email == email {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID > out[j].ID
	})
	return out, nil
}

func (s *AccessStore) RevokeAgentGrant(_ context.Context, id string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.grants[id]
	if !ok {
		return store.ErrNoAgentGrant
	}
	if t.RevokedAt.IsZero() {
		t.RevokedAt = at
		s.grants[id] = t
	}
	return nil
}

func (s *AccessStore) TouchAgentGrant(_ context.Context, id string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.grants[id]; ok {
		t.LastUsed = at
		s.grants[id] = t
	}
	return nil
}

func (s *AccessStore) RotateAgentGrant(_ context.Context, id string, from int) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.grants[id]
	if !ok || t.Generation != from {
		return false, nil
	}
	t.Generation++
	s.grants[id] = t
	return true, nil
}
