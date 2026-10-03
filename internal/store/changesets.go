package store

import (
	"context"
	"errors"
	"time"
)

// ChangeSet is a piece of work kept apart from the record and from every
// other piece of work until it is accepted, as a pull request is
// (docs/adr/0022): its own drafts of every manifest it touches, each with
// the version it started from, reviewed in one place and accepted whole.
type ChangeSet struct {
	ID          string
	Title       string
	Description string
	// Owner is who works in it: an agent's grant, so two agents never
	// share one, or a person who opened it.
	Owner string
	// Agent is the agent working in it, if one is; For is the person it
	// acts for, who alone accepts it ("" for an anonymous operator).
	Agent string
	For   string
	// Status is ChangeSetOpen, ChangeSetProposed, ChangeSetMerging,
	// ChangeSetMerged or ChangeSetClosed.
	Status string
	// Reason and Waivers are what proposing it said: why, and the checks
	// left open with why each.
	Reason  string
	Waivers []Waiver
	At      time.Time
	Updated time.Time
	// DecidedBy, DecidedAt and DecisionReason record the last time it
	// was accepted or closed.
	DecidedBy      string
	DecidedAt      time.Time
	DecisionReason string
}

// ChangeItem is one manifest's draft in a change set.
type ChangeItem struct {
	Set, Kind, ID string
	Text          []byte
	// Base is the manifest's latest version when the change set first
	// touched it; 0 for a manifest the change set creates.
	Base int
	// Included is false for an item trimmed from the next acceptance: it
	// stays in the change set, as a draft, for later.
	Included bool
	By       string
	At       time.Time
}

// Where a change set stands.
const (
	ChangeSetOpen     = "open"
	ChangeSetProposed = "proposed"
	// ChangeSetMerging is held while its items are committed, so two
	// acceptances never both commit them.
	ChangeSetMerging = "merging"
	ChangeSetMerged  = "merged"
	ChangeSetClosed  = "closed"
)

// ChangeSetFilter narrows a list. An empty field matches anything.
type ChangeSetFilter struct {
	For, Owner, Status string
}

var (
	// ErrNoChangeSet is a change set that does not exist.
	ErrNoChangeSet = errors.New("no such change set")
	// ErrChangeSetMoved is a change set no longer where a move expected
	// it: accepted or closed by someone else meanwhile.
	ErrChangeSetMoved = errors.New("the change set has moved on since")
)

// ChangeSetStore keeps change sets and their items. Moving one is atomic:
// of two moves from the same status at once, one wins and the other is
// ErrChangeSetMoved.
type ChangeSetStore interface {
	// PutChangeSet keeps a change set, replacing one with its id.
	PutChangeSet(ctx context.Context, cs ChangeSet) error
	// GetChangeSet returns one, or ErrNoChangeSet.
	GetChangeSet(ctx context.Context, id string) (ChangeSet, error)
	// ListChangeSets returns those the filter matches, newest first.
	ListChangeSets(ctx context.Context, f ChangeSetFilter) ([]ChangeSet, error)
	// MoveChangeSet moves a change set from one status to another,
	// recording who, when and why; ErrChangeSetMoved when it is not in
	// from, ErrNoChangeSet when there is none.
	MoveChangeSet(ctx context.Context, id, from, to, by, reason string, at time.Time) (ChangeSet, error)
	// PutChangeItem keeps an item, replacing the one for its manifest.
	PutChangeItem(ctx context.Context, it ChangeItem) error
	// GetChangeItem returns a change set's item for a manifest; found is
	// false when it has none.
	GetChangeItem(ctx context.Context, set, kind, id string) (it ChangeItem, found bool, err error)
	// ListChangeItems returns a change set's items, oldest first.
	ListChangeItems(ctx context.Context, set string) ([]ChangeItem, error)
	// DeleteChangeItem drops an item; none is no error.
	DeleteChangeItem(ctx context.Context, set, kind, id string) error
}
