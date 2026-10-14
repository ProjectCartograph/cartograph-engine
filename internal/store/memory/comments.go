package memory

import (
	"context"
	"slices"
	"sort"
	"sync"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

var _ store.CommentStore = (*ManifestStore)(nil)

// comments keeps the memory store's comments; its zero value is ready.
type comments struct {
	mu   sync.Mutex
	byID map[string]store.Comment
}

func cloneComment(c store.Comment) store.Comment {
	c.Mentions = slices.Clone(c.Mentions)
	return c
}

// PutComment keeps a comment.
func (m *ManifestStore) PutComment(_ context.Context, c store.Comment) error {
	m.comments.mu.Lock()
	defer m.comments.mu.Unlock()
	if m.comments.byID == nil {
		m.comments.byID = map[string]store.Comment{}
	}
	m.comments.byID[c.ID] = cloneComment(c)
	return nil
}

// GetComment returns one comment.
func (m *ManifestStore) GetComment(_ context.Context, id string) (store.Comment, error) {
	m.comments.mu.Lock()
	defer m.comments.mu.Unlock()
	c, ok := m.comments.byID[id]
	if !ok {
		return store.Comment{}, store.ErrNoComment
	}
	return cloneComment(c), nil
}

// ListComments returns a record's comments, oldest first.
func (m *ManifestStore) ListComments(_ context.Context, kind, manifest string) ([]store.Comment, error) {
	m.comments.mu.Lock()
	defer m.comments.mu.Unlock()
	out := []store.Comment{}
	for _, c := range m.comments.byID {
		if c.Kind == kind && c.Manifest == manifest {
			out = append(out, cloneComment(c))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].At.Equal(out[j].At) {
			return out[i].At.Before(out[j].At)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// ListMentioning returns the comments that mention any of the parties,
// newest first.
func (m *ManifestStore) ListMentioning(_ context.Context, mentions []store.Mention, limit int) ([]store.Comment, error) {
	m.comments.mu.Lock()
	defer m.comments.mu.Unlock()
	out := []store.Comment{}
	for _, c := range m.comments.byID {
		if slices.ContainsFunc(c.Mentions, func(x store.Mention) bool { return slices.Contains(mentions, x) }) {
			out = append(out, cloneComment(c))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].At.Equal(out[j].At) {
			return out[i].At.After(out[j].At)
		}
		return out[i].ID > out[j].ID
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
