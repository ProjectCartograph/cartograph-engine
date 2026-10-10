// Package memory keeps people's acts in the process, for tests and for
// a single process that analyses what it just recorded. It keeps nothing
// across a restart; a deployment keeps its trace in jsonl or postgres.
package memory

import (
	"context"
	"sort"
	"sync"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/activity"
)

// Store records acts and reads them back.
type Store struct {
	mu   sync.Mutex
	acts []activity.Event
}

var (
	_ activity.Recorder = (*Store)(nil)
	_ activity.Reader   = (*Store)(nil)
)

// New returns an empty store.
func New() *Store { return &Store{} }

// Record keeps one act.
func (s *Store) Record(e activity.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.acts = append(s.acts, e)
}

// Read returns the acts q asks for, in the order they happened.
func (s *Store) Read(_ context.Context, q activity.Query) ([]activity.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []activity.Event
	for _, e := range s.acts {
		if q.Matches(e) {
			out = append(out, e)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out, nil
}
