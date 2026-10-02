package memory

import (
	"context"
	"slices"
	"sort"
	"sync"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

// AccessStore is the in-memory adapter for store.AccessStore. One lock
// covers the list, which is all the atomicity the port asks for.
type AccessStore struct {
	mu     sync.Mutex
	people map[string]store.Person     // by address
	grants map[string]store.AgentGrant // by id
}

var _ store.AccessStore = (*AccessStore)(nil)

// NewAccessStore returns an empty access list.
func NewAccessStore() *AccessStore {
	return &AccessStore{people: map[string]store.Person{}}
}

func (s *AccessStore) ListPeople(context.Context) ([]store.Person, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]store.Person, 0, len(s.people))
	for _, p := range s.people {
		out = append(out, clonePerson(p))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Email < out[j].Email })
	return out, nil
}

func (s *AccessStore) GetPerson(_ context.Context, email string) (store.Person, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.people[email]
	if !ok {
		return store.Person{}, store.ErrNoPerson
	}
	return clonePerson(p), nil
}

func (s *AccessStore) GrantPerson(_ context.Context, p store.Person) (store.Person, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.people[p.Email]
	if !ok {
		cur = store.Person{Email: p.Email, AddedBy: p.AddedBy, AddedOn: p.AddedOn}
	}
	cur.Roles = slices.Clone(p.Roles)
	cur.Teams = slices.Clone(p.Teams)
	cur.AgentsOff = p.AgentsOff
	s.people[p.Email] = cur
	return clonePerson(cur), nil
}

func (s *AccessStore) RecordSignIn(_ context.Context, in store.SignIn) (store.Person, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.people[in.Email]
	if !ok {
		if !in.Enrol {
			return store.Person{}, store.ErrNoPerson
		}
		cur = store.Person{Email: in.Email, AddedBy: store.EnrolledByDirectory, AddedOn: in.At}
	}
	cur.Name, cur.Subject = in.Name, in.Subject
	cur.DirectoryRoles = slices.Clone(in.Roles)
	cur.DirectoryTeams = slices.Clone(in.Teams)
	cur.LastSignedIn = in.At
	s.people[in.Email] = cur
	return clonePerson(cur), nil
}

func (s *AccessStore) DeletePerson(_ context.Context, email string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.people[email]; !ok {
		return store.ErrNoPerson
	}
	delete(s.people, email)
	return nil
}

// clonePerson copies p's slices, so a caller changing what it got back
// never changes the list.
func clonePerson(p store.Person) store.Person {
	p.Roles = slices.Clone(p.Roles)
	p.Teams = slices.Clone(p.Teams)
	p.DirectoryRoles = slices.Clone(p.DirectoryRoles)
	p.DirectoryTeams = slices.Clone(p.DirectoryTeams)
	return p
}
