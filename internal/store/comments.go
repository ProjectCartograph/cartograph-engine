package store

import (
	"context"
	"errors"
	"time"
)

// Comment is one remark on a manifest, beside the part it is about: a
// section of its walk or one item in it (TAXONOMY.md D72). Comments are
// a conversation about the record, not the record: they are kept apart
// from its versions, never in a change set, and never validated as a
// manifest is.
type Comment struct {
	ID string
	// Kind and Manifest name the record it is on.
	Kind, Manifest string
	// Anchor is what it is about: a JSON pointer into the manifest
	// ("/spec/deliverables/{training-pack}") or a section of the walk
	// ("section:scope"); "" for the record as a whole.
	Anchor string
	// Reply is the comment it answers; "" starts a thread.
	Reply string
	Body  string
	// Mentions are who it asks, each a person on the access list or a
	// team, as the writer chose them.
	Mentions []Mention
	Author   string
	At       time.Time
	// Resolved closes a thread: set on the comment that starts it, with
	// who closed it and when.
	Resolved   bool
	ResolvedBy string
	ResolvedAt time.Time
}

// Mention is one party a comment asks: a person by their address on the
// access list, or a team by its id.
type Mention struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// Who a mention names.
const (
	MentionPerson = "person"
	MentionTeam   = "team"
)

// ErrNoComment is a comment that does not exist.
var ErrNoComment = errors.New("no such comment")

// CommentStore keeps comments. Comments are appended and their threads
// resolved; they are read by the record they are on, or by whom they
// mention. A store without it answers that comments are not kept.
type CommentStore interface {
	// PutComment keeps a comment, replacing one with its id.
	PutComment(ctx context.Context, c Comment) error
	// GetComment returns one, or ErrNoComment.
	GetComment(ctx context.Context, id string) (Comment, error)
	// ListComments returns a record's comments, oldest first.
	ListComments(ctx context.Context, kind, manifest string) ([]Comment, error)
	// ListMentioning returns the comments that mention any of the
	// parties, newest first, at most limit.
	ListMentioning(ctx context.Context, mentions []Mention, limit int) ([]Comment, error)
}
