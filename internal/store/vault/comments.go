package vault

import (
	"context"
	"errors"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

// The vault keeps comments in its index, beside its change sets: they are
// a conversation about the record, not files of it.

var _ store.CommentStore = (*ManifestStore)(nil)

// errNoComments is an index that keeps no comments.
var errNoComments = errors.New("the vault's index keeps no comments")

func (m *ManifestStore) commentStore() (store.CommentStore, error) {
	cs, ok := m.index.(store.CommentStore)
	if !ok {
		return nil, errNoComments
	}
	return cs, nil
}

// PutComment asks the index.
func (m *ManifestStore) PutComment(ctx context.Context, c store.Comment) error {
	s, err := m.commentStore()
	if err != nil {
		return err
	}
	return s.PutComment(ctx, c)
}

// GetComment asks the index.
func (m *ManifestStore) GetComment(ctx context.Context, id string) (store.Comment, error) {
	s, err := m.commentStore()
	if err != nil {
		return store.Comment{}, err
	}
	return s.GetComment(ctx, id)
}

// ListComments asks the index.
func (m *ManifestStore) ListComments(ctx context.Context, kind, manifest string) ([]store.Comment, error) {
	s, err := m.commentStore()
	if err != nil {
		return nil, err
	}
	return s.ListComments(ctx, kind, manifest)
}

// ListMentioning asks the index.
func (m *ManifestStore) ListMentioning(ctx context.Context, mentions []store.Mention, limit int) ([]store.Comment, error) {
	s, err := m.commentStore()
	if err != nil {
		return nil, err
	}
	return s.ListMentioning(ctx, mentions, limit)
}
