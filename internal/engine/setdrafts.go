package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/crdt"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/identity"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/kinds"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

// A change set's draft of a manifest is a shared document of its own
// (docs/adr/0024), kept beside the manifest's other drafts in the same
// document store under the kind "set/<change set>/<Kind>": the store's
// key stays kind and id, as the agent feeds' does, and nothing in the
// store changes for it. Edits arrive over the sync socket like any
// shared draft's and land in the change set's item instead of the
// working copy.
const setKindPrefix = "set/"

func setKind(set, kind string) string { return setKindPrefix + set + "/" + kind }

// SetOf splits a document's stored kind into the change set it is a
// draft in and the manifest's kind; set is "" for a manifest's own
// shared draft.
func SetOf(stored string) (set, kind string) {
	rest, ok := strings.CutPrefix(stored, setKindPrefix)
	if !ok {
		return "", stored
	}
	set, kind, ok = strings.Cut(rest, "/")
	if !ok {
		return "", stored
	}
	return set, kind
}

// DocumentInSet returns the id of a change set's shared draft of a
// manifest, making it on first use from the change set's item, else from
// the record (which then starts the item). When the item was changed
// outside the document (an agent's edit, a whole save), the change is
// folded in first.
func (s *Shared) DocumentInSet(ctx context.Context, set, kind, id string) (string, error) {
	if _, ok := kinds.ByName(kind); !ok {
		return "", fmt.Errorf("%w: %s", ErrUnknownKind, kind)
	}
	if !s.e.MayWorkInSet(ctx, set) {
		return "", ErrNotTheirChangeSet
	}
	text, _, err := s.e.ChangeSetText(ctx, set, kind, id)
	if err != nil {
		return "", err
	}
	if text == nil {
		return "", fmt.Errorf("%w: %s/%s", ErrNotFound, kind, id)
	}
	content, err := s.e.codec.Decode(text)
	if err != nil {
		return "", err
	}
	docID, err := s.docs.DocumentFor(ctx, setKind(set, kind), id)
	switch {
	case err == nil:
		if _, err := s.reconcile(ctx, docID, content, crdt.Change{Message: "change set item changed outside the shared draft"}); err != nil {
			return "", err
		}
		return docID, nil
	case !errors.Is(err, store.ErrNoDocument):
		return "", err
	}
	return s.create(ctx, setKind(set, kind), id, content, s.e.Shape(kind))
}

// reconcileItem folds a change set item's new text into its shared
// draft, when it has one: an edit made through the API or by an agent
// reaches everyone editing the draft.
func (s *Shared) reconcileItem(ctx context.Context, set, kind, id string, text []byte) {
	docID, err := s.docs.DocumentFor(ctx, setKind(set, kind), id)
	if err != nil {
		return
	}
	content, err := s.e.codec.Decode(text)
	if err != nil {
		return
	}
	_, _ = s.reconcile(ctx, docID, content, crdt.Change{Message: "change set item changed"})
}

// MayWorkInSet is Engine.MayWorkInSet, for the sync socket.
func (s *Shared) MayWorkInSet(ctx context.Context, set string) bool {
	return s.e.MayWorkInSet(ctx, set)
}

// MayWorkInSet reports whether the principal on ctx may edit a change
// set's drafts: its owner, the person it is for, or any person while it
// is open, since change sets are live like every other draft (docs/adr/
// 0024). An agent works only in its own.
func (e *Engine) MayWorkInSet(ctx context.Context, set string) bool {
	s, err := e.changeSetStore()
	if err != nil {
		return false
	}
	cs, err := s.GetChangeSet(ctx, set)
	if err != nil {
		return false
	}
	p := identity.PrincipalFrom(ctx)
	return e.mayWorkIn(p, cs) || (p.Agent == "" && cs.Status == store.ChangeSetOpen)
}

// putItemText writes a change set's shared draft into its item, starting
// the item from the record the first time. Who may write was decided
// when the document was opened.
func (e *Engine) putItemText(ctx context.Context, set, kind, id string, text []byte) error {
	s, err := e.changeSetStore()
	if err != nil {
		return err
	}
	it, found, err := s.GetChangeItem(ctx, set, kind, id)
	if err != nil {
		return err
	}
	if !found {
		it = store.ChangeItem{Set: set, Kind: kind, ID: id, Included: true}
		if it.Base, err = latestNumber(ctx, e.manifests, kind, id); err != nil {
			return err
		}
	}
	it.Text, it.By, it.At = text, identity.PrincipalFrom(ctx).Actor(e.operator(ctx)), timeNow().UTC()
	return s.PutChangeItem(ctx, it)
}

// currentInPlay is a manifest as a read on ctx sees it: the draft of the
// change set read on ctx, where it has one (docs/adr/0024), else the
// current version.
func (e *Engine) currentInPlay(ctx context.Context, kind, id string) (Version, bool, error) {
	if text, ok := inPlay(ctx, kind, id); ok {
		return Version{Kind: kind, ID: id, YAML: text}, true, nil
	}
	return e.manifests.GetCurrent(ctx, kind, id)
}
