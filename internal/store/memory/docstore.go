package memory

import (
	"context"
	"sync"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

// DocStore is the in-memory adapter for store.DocStore. One lock covers
// every document, which is all the atomicity the port asks for.
type DocStore struct {
	mu        sync.Mutex
	docs      map[string]*document // by document id
	manifests map[manifestKey]string
}

type document struct {
	kind, id  string
	snapshot  []byte
	compacted int64 // the snapshot includes every chunk up to this
	next      int64 // the last sequence number handed out
	chunks    []store.Chunk
}

var _ store.DocStore = (*DocStore)(nil)

// NewDocStore returns an empty DocStore.
func NewDocStore() *DocStore {
	return &DocStore{docs: map[string]*document{}, manifests: map[manifestKey]string{}}
}

func (s *DocStore) Create(_ context.Context, kind, id, docID string, snapshot []byte) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := manifestKey{kind, id}
	if existing, ok := s.manifests[k]; ok {
		return existing, false, nil
	}
	s.manifests[k] = docID
	s.docs[docID] = &document{kind: kind, id: id, snapshot: clone(snapshot)}
	return docID, true, nil
}

func (s *DocStore) DocumentFor(_ context.Context, kind, id string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	docID, ok := s.manifests[manifestKey{kind, id}]
	if !ok {
		return "", store.ErrNoDocument
	}
	return docID, nil
}

func (s *DocStore) ManifestFor(_ context.Context, docID string) (string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.docs[docID]
	if !ok {
		return "", "", store.ErrNoDocument
	}
	return d.kind, d.id, nil
}

func (s *DocStore) Append(_ context.Context, docID string, data []byte) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.docs[docID]
	if !ok {
		return 0, store.ErrNoDocument
	}
	d.next++
	d.chunks = append(d.chunks, store.Chunk{Seq: d.next, Data: clone(data)})
	return d.next, nil
}

func (s *DocStore) Load(_ context.Context, docID string) ([]byte, []store.Chunk, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.docs[docID]
	if !ok {
		return nil, nil, store.ErrNoDocument
	}
	return clone(d.snapshot), d.since(0), nil
}

func (s *DocStore) Since(_ context.Context, docID string, after int64) ([]store.Chunk, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.docs[docID]
	if !ok {
		return nil, store.ErrNoDocument
	}
	return d.since(after), nil
}

// Compact ignores a compaction older than the stored one, whose
// snapshot lacks chunks that are already gone.
func (s *DocStore) Compact(_ context.Context, docID string, snapshot []byte, upTo int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.docs[docID]
	if !ok {
		return store.ErrNoDocument
	}
	if upTo < d.compacted {
		return nil
	}
	d.snapshot = clone(snapshot)
	d.compacted = upTo
	kept := []store.Chunk{}
	for _, c := range d.chunks {
		if c.Seq > upTo {
			kept = append(kept, c)
		}
	}
	d.chunks = kept
	return nil
}

func (d *document) since(after int64) []store.Chunk {
	out := []store.Chunk{}
	for _, c := range d.chunks {
		if c.Seq > after {
			out = append(out, store.Chunk{Seq: c.Seq, Data: clone(c.Data)})
		}
	}
	return out
}

func clone(b []byte) []byte { return append([]byte{}, b...) }
