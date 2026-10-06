package engine

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/identity"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

// Change sets (docs/adr/0022). A piece of work is kept apart from the
// record and from every other piece of work until it is accepted, as a
// pull request is: its own drafts of every manifest it touches, each with
// the version it started from. Agents always work in one, so two agents
// never draft over each other; people keep editing the shared drafts and
// may open a change set to review it, work in it, or start one. Accepting
// one saves every item still included, in one transaction; an item
// trimmed from it stays, for later.

var (
	// ErrNoChangeSets is a store that keeps no change sets.
	ErrNoChangeSets = errors.New("this deployment's store keeps no change sets")
	// ErrNotOpen refuses a change to a change set no longer open for it.
	ErrNotOpen = fmt.Errorf("%w: the change set is not open", ErrConflict)
	// ErrNotTheirChangeSet refuses work in a change set by anyone but who
	// works in it, or its acceptance by anyone but its person.
	ErrNotTheirChangeSet = fmt.Errorf("%w: this change set is not yours", identity.ErrForbidden)
)

func (e *Engine) changeSetStore() (store.ChangeSetStore, error) {
	cs, ok := e.manifests.(store.ChangeSetStore)
	if !ok {
		return nil, ErrNoChangeSets
	}
	return cs, nil
}

// ownerOf is who works in a change set: an agent's grant, else the person
// and the agent's name, else the person.
func ownerOf(p identity.Principal) string {
	if p.Grant != "" {
		return "grant:" + p.Grant
	}
	if p.Agent != "" {
		return "agent:" + personKey(p) + ":" + p.Agent
	}
	return "person:" + personKey(p)
}

// StartChangeSet opens a change set for the principal on ctx to work in.
func (e *Engine) StartChangeSet(ctx context.Context, title, description string) (store.ChangeSet, error) {
	s, err := e.changeSetStore()
	if err != nil {
		return store.ChangeSet{}, err
	}
	p := identity.PrincipalFrom(ctx)
	now := timeNow().UTC()
	if strings.TrimSpace(title) == "" {
		title = "Untitled work"
		if p.Agent != "" {
			title = "Work by " + p.Agent
		}
	}
	cs := store.ChangeSet{ID: newProposalID(), Title: strings.TrimSpace(title), Description: strings.TrimSpace(description),
		Owner: ownerOf(p), Agent: p.Agent, For: personKey(p), Status: store.ChangeSetOpen, At: now, Updated: now}
	return cs, s.PutChangeSet(ctx, cs)
}

// WorkingChangeSet is the change set the principal on ctx works in: id
// when given (theirs, and open), else their latest open one, else a new
// one.
func (e *Engine) WorkingChangeSet(ctx context.Context, id string) (store.ChangeSet, error) {
	s, err := e.changeSetStore()
	if err != nil {
		return store.ChangeSet{}, err
	}
	p := identity.PrincipalFrom(ctx)
	if id != "" {
		cs, err := s.GetChangeSet(ctx, id)
		if errors.Is(err, store.ErrNoChangeSet) {
			return store.ChangeSet{}, fmt.Errorf("%w: change set %s", ErrNotFound, id)
		}
		if err != nil {
			return store.ChangeSet{}, err
		}
		if !e.mayWorkIn(p, cs) {
			return store.ChangeSet{}, ErrNotTheirChangeSet
		}
		if cs.Status != store.ChangeSetOpen {
			return store.ChangeSet{}, ErrNotOpen
		}
		return cs, nil
	}
	open, err := s.ListChangeSets(ctx, store.ChangeSetFilter{Owner: ownerOf(p), Status: store.ChangeSetOpen})
	if err != nil {
		return store.ChangeSet{}, err
	}
	if len(open) > 0 {
		return open[0], nil
	}
	return e.StartChangeSet(ctx, "", "")
}

// mayWorkIn says whether p may change a change set's items: who works in
// it, or the person it is for, joining their agent's work.
func (e *Engine) mayWorkIn(p identity.Principal, cs store.ChangeSet) bool {
	return cs.Owner == ownerOf(p) || (p.Agent == "" && cs.For == personKey(p))
}

// ChangeSetText is a manifest as it stands in a change set: its item, else
// the record (its latest version), and whether it is in the change set.
func (e *Engine) ChangeSetText(ctx context.Context, set, kind, id string) ([]byte, bool, error) {
	s, err := e.changeSetStore()
	if err != nil {
		return nil, false, err
	}
	it, found, err := s.GetChangeItem(ctx, set, kind, id)
	if err != nil || found {
		return it.Text, found, err
	}
	v, found, err := e.manifests.GetCurrent(ctx, kind, id)
	if err != nil {
		return nil, false, err
	}
	if !found {
		return nil, false, fmt.Errorf("%w: %s/%s", ErrNotFound, kind, id)
	}
	return e.normalizeLegacy(kind, v.YAML), false, nil
}

// SaveInChangeSet keeps a whole manifest as a change set's draft of it.
func (e *Engine) SaveInChangeSet(ctx context.Context, set, kind, id string, text []byte) error {
	return e.writeItem(ctx, set, kind, id, func([]byte) ([]byte, error) { return text, nil })
}

// EditInChangeSet sets and clears single fields of a change set's draft of
// a manifest, starting it from the record when the change set has none,
// and returns the draft as it then stands.
func (e *Engine) EditInChangeSet(ctx context.Context, set, kind, id string, put map[string]any, unset []string) ([]byte, error) {
	var out []byte
	err := e.writeItem(ctx, set, kind, id, func(cur []byte) ([]byte, error) {
		doc := map[string]any{"apiVersion": "cartograph/v1", "kind": kind, "metadata": map[string]any{"id": id}, "spec": map[string]any{}}
		if cur != nil {
			if err := e.codec.DecodeInto(cur, &doc); err != nil {
				return nil, err
			}
		}
		if err := applyEdit(doc, id, put, unset); err != nil {
			return nil, err
		}
		text, err := e.codec.Encode(doc)
		out = text
		return text, err
	})
	return out, err
}

// writeItem changes one item of an open change set the principal may work
// in, recording the version it started from the first time.
func (e *Engine) writeItem(ctx context.Context, set, kind, id string, change func(cur []byte) ([]byte, error)) error {
	s, err := e.changeSetStore()
	if err != nil {
		return err
	}
	cs, err := e.WorkingChangeSet(ctx, set)
	if err != nil {
		return err
	}
	it, found, err := s.GetChangeItem(ctx, cs.ID, kind, id)
	if err != nil {
		return err
	}
	var cur []byte
	if found {
		cur = it.Text
	} else {
		it = store.ChangeItem{Set: cs.ID, Kind: kind, ID: id, Included: true}
		if v, ok, err := e.manifests.GetCurrent(ctx, kind, id); err != nil {
			return err
		} else if ok {
			cur = e.normalizeLegacy(kind, v.YAML)
		}
		if it.Base, err = latestNumber(ctx, e.manifests, kind, id); err != nil {
			return err
		}
	}
	text, err := change(cur)
	if err != nil {
		return err
	}
	// Asked as a save would be: may this person change this manifest, from
	// what it was to what it will be (its team, its fields).
	var before, after map[string]any
	if cur != nil {
		_ = e.codec.DecodeInto(cur, &before)
	}
	if err := e.codec.DecodeInto(text, &after); err != nil {
		return fmt.Errorf("%s/%s: %w", kind, id, err)
	}
	if err := e.guard(ctx, kind, id, before, after); err != nil {
		return err
	}
	if problems := deprecatedFieldProblems(kind, after); len(problems) > 0 {
		return &ValidationError{Problems: problems}
	}
	p := identity.PrincipalFrom(ctx)
	// An agent is held to the order of work (TAXONOMY.md D31): what it
	// names is in the record or in its change set already, and it leaves
	// no placeholder.
	if p.Agent != "" {
		items, err := s.ListChangeItems(ctx, cs.ID)
		if err != nil {
			return err
		}
		set := map[string]bool{kind + "/" + id: true}
		for _, other := range items {
			set[other.Kind+"/"+other.ID] = true
		}
		if problems := e.agentOrderProblems(ctx, set, kind, id, after); len(problems) > 0 {
			return &ValidationError{Problems: problems}
		}
	}
	it.Text, it.By, it.At = text, p.Actor(e.operator(ctx)), timeNow().UTC()
	if err := s.PutChangeItem(ctx, it); err != nil {
		return err
	}
	cs.Updated = it.At
	return s.PutChangeSet(ctx, cs)
}

// ChangeSetItem is one item as a reviewer reads it: what it changes from
// the version it started from, its checks with the rest of the change set,
// and whether the record has moved on since.
type ChangeSetItem struct {
	Item    store.ChangeItem
	Name    string
	Changes []Change
	Checks  []Check
	// Stale is the version saved since the item started, 0 when none was.
	Stale int
}

// ChangeSetView is a change set and its items.
type ChangeSetView struct {
	ChangeSet store.ChangeSet
	Items     []ChangeSetItem
}

// ViewChangeSet reads a change set for review.
func (e *Engine) ViewChangeSet(ctx context.Context, id string) (ChangeSetView, error) {
	s, err := e.changeSetStore()
	if err != nil {
		return ChangeSetView{}, err
	}
	cs, err := s.GetChangeSet(ctx, id)
	if errors.Is(err, store.ErrNoChangeSet) {
		return ChangeSetView{}, fmt.Errorf("%w: change set %s", ErrNotFound, id)
	}
	if err != nil {
		return ChangeSetView{}, err
	}
	items, err := s.ListChangeItems(ctx, id)
	if err != nil {
		return ChangeSetView{}, err
	}
	docs := map[string]map[string]any{}
	for _, it := range items {
		var doc map[string]any
		if e.codec.DecodeInto(it.Text, &doc) == nil {
			docs[it.Kind+"/"+it.ID] = doc
		}
	}
	checkCtx := withProposed(ctx, docs)
	view := ChangeSetView{ChangeSet: cs, Items: []ChangeSetItem{}}
	for _, it := range items {
		doc := docs[it.Kind+"/"+it.ID]
		item := ChangeSetItem{Item: it, Changes: []Change{}, Checks: []Check{}}
		if meta, ok := doc["metadata"].(map[string]any); ok {
			item.Name, _ = meta["name"].(string)
		}
		before := map[string]any{}
		if it.Base > 0 {
			if v, err := e.GetVersion(ctx, it.Kind, it.ID, it.Base); err == nil {
				_ = e.codec.DecodeInto(v.YAML, &before)
			}
		}
		item.Changes = diffValues("", before, doc)
		if checks, err := e.ChecksOf(checkCtx, it.Kind, it.ID, it.Text); err == nil {
			item.Checks = checks
		}
		if latest, err := latestNumber(ctx, e.manifests, it.Kind, it.ID); err == nil && latest != it.Base {
			item.Stale = latest
		}
		view.Items = append(view.Items, item)
	}
	return view, nil
}

// ChangeSets lists change sets: the person's own and their agents', or
// everyone's for an administrator who asks.
func (e *Engine) ChangeSets(ctx context.Context, status string, everyone bool) ([]store.ChangeSet, error) {
	s, err := e.changeSetStore()
	if err != nil {
		return nil, err
	}
	f := store.ChangeSetFilter{Status: status}
	if !everyone || !e.isAdministrator(ctx) {
		f.For = personKey(identity.PrincipalFrom(ctx))
	}
	return s.ListChangeSets(ctx, f)
}

// RetitleChangeSet changes what a change set says it is.
func (e *Engine) RetitleChangeSet(ctx context.Context, id, title, description string) (store.ChangeSet, error) {
	s, err := e.changeSetStore()
	if err != nil {
		return store.ChangeSet{}, err
	}
	cs, err := e.WorkingChangeSet(ctx, id)
	if err != nil {
		return store.ChangeSet{}, err
	}
	if strings.TrimSpace(title) != "" {
		cs.Title = strings.TrimSpace(title)
	}
	cs.Description = strings.TrimSpace(description)
	cs.Updated = timeNow().UTC()
	return cs, s.PutChangeSet(ctx, cs)
}

// IncludeChangeItem includes an item in the next acceptance, or trims it
// from it: the person it is for may, while it is open or proposed.
func (e *Engine) IncludeChangeItem(ctx context.Context, set, kind, id string, included bool) error {
	s, err := e.changeSetStore()
	if err != nil {
		return err
	}
	cs, err := s.GetChangeSet(ctx, set)
	if err != nil {
		return err
	}
	p := identity.PrincipalFrom(ctx)
	if cs.For != personKey(p) && !e.mayWorkIn(p, cs) {
		return ErrNotTheirChangeSet
	}
	if cs.Status != store.ChangeSetOpen && cs.Status != store.ChangeSetProposed {
		return ErrNotOpen
	}
	it, found, err := s.GetChangeItem(ctx, set, kind, id)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%w: %s/%s is not in the change set", ErrNotFound, kind, id)
	}
	it.Included = included
	return s.PutChangeItem(ctx, it)
}

// DropChangeItem takes an item out of a change set altogether.
func (e *Engine) DropChangeItem(ctx context.Context, set, kind, id string) error {
	s, err := e.changeSetStore()
	if err != nil {
		return err
	}
	if _, err := e.WorkingChangeSet(ctx, set); err != nil {
		return err
	}
	return s.DeleteChangeItem(ctx, set, kind, id)
}

// included returns the items to accept, as members, checked and ordered.
func (e *Engine) included(ctx context.Context, s store.ChangeSetStore, set string) ([]store.ChangeItem, []SetMember, map[string]map[string]any, error) {
	items, err := s.ListChangeItems(ctx, set)
	if err != nil {
		return nil, nil, nil, err
	}
	var in []store.ChangeItem
	var members []SetMember
	for _, it := range items {
		if it.Included {
			in = append(in, it)
			members = append(members, SetMember{Kind: it.Kind, ID: it.ID, Text: it.Text})
		}
	}
	if len(members) == 0 {
		return nil, nil, nil, fmt.Errorf("%w: nothing in the change set is included", ErrConflict)
	}
	ordered, docs, err := e.checkMembers(ctx, members)
	return in, ordered, docs, err
}

// ProposeChangeSet puts a change set up for its person to accept: every
// included item checked as one set, as a proposal set is, open checks
// refused unless waived with a reason (by Kind/id, then check id).
func (e *Engine) ProposeChangeSet(ctx context.Context, id, reason string, waive map[string]map[string]string) (store.ChangeSet, error) {
	s, err := e.changeSetStore()
	if err != nil {
		return store.ChangeSet{}, err
	}
	cs, err := e.WorkingChangeSet(ctx, id)
	if err != nil {
		return store.ChangeSet{}, err
	}
	open, err := e.openIn(ctx, s, cs.ID)
	if err != nil {
		return store.ChangeSet{}, err
	}
	var unmet []OpenCheck
	var waivers []store.Waiver
	for _, c := range open {
		if why := strings.TrimSpace(waive[c.Kind+"/"+c.ManifestID][c.ID]); why != "" {
			waivers = append(waivers, store.Waiver{On: c.Kind + "/" + c.ManifestID, Check: c.ID, Message: c.Message, Reason: why})
			continue
		}
		unmet = append(unmet, c)
	}
	if len(unmet) > 0 {
		return store.ChangeSet{}, &OpenChecksError{Open: unmet}
	}
	cs.Reason, cs.Waivers, cs.Updated = strings.TrimSpace(reason), waivers, timeNow().UTC()
	if err := s.PutChangeSet(ctx, cs); err != nil {
		return store.ChangeSet{}, err
	}
	return s.MoveChangeSet(ctx, cs.ID, store.ChangeSetOpen, store.ChangeSetProposed, "", "", cs.Updated)
}

// OpenInChangeSet is every check still open across the drafts a change
// set would propose, each checked with the others: exactly what
// ProposeChangeSet refuses on, so an agent asking before it proposes
// hears the same answer it will get when it does.
func (e *Engine) OpenInChangeSet(ctx context.Context, id string) ([]OpenCheck, error) {
	s, err := e.changeSetStore()
	if err != nil {
		return nil, err
	}
	return e.openIn(ctx, s, id)
}

func (e *Engine) openIn(ctx context.Context, s store.ChangeSetStore, id string) ([]OpenCheck, error) {
	_, ordered, docs, err := e.included(ctx, s, id)
	if err != nil {
		return nil, err
	}
	checkCtx := withProposed(ctx, docs)
	var open []OpenCheck
	for _, m := range ordered {
		checks, err := e.ChecksOf(checkCtx, m.Kind, m.ID, m.Text)
		if err != nil {
			return nil, err
		}
		for _, c := range checks {
			if c.Open() {
				open = append(open, OpenCheck{Kind: m.Kind, ManifestID: m.ID, Check: c})
			}
		}
	}
	return open, nil
}

// AcceptChangeSet saves every included item of a proposed change set as
// the person it is for, in one transaction and in the order references
// need, each still on the version it started from. It is claimed first,
// so two acceptances never both save it. Items trimmed from it stay, and
// the change set is open again with them; with none left it is merged.
func (e *Engine) AcceptChangeSet(ctx context.Context, id, reason string) ([]Version, error) {
	if err := refuseAgent(ctx); err != nil {
		return nil, err
	}
	s, err := e.changeSetStore()
	if err != nil {
		return nil, err
	}
	cs, err := s.GetChangeSet(ctx, id)
	if errors.Is(err, store.ErrNoChangeSet) {
		return nil, fmt.Errorf("%w: change set %s", ErrNotFound, id)
	}
	if err != nil {
		return nil, err
	}
	p := identity.PrincipalFrom(ctx)
	if cs.For != personKey(p) {
		return nil, ErrNotTheirChangeSet
	}
	actor := p.Actor(e.operator(ctx))
	now := timeNow().UTC()
	// Claimed, so nobody else accepts or closes it meanwhile.
	if _, err := s.MoveChangeSet(ctx, id, store.ChangeSetProposed, store.ChangeSetMerging, actor, reason, now); err != nil {
		if errors.Is(err, store.ErrChangeSetMoved) {
			return nil, fmt.Errorf("%w: the change set is not proposed", ErrConflict)
		}
		return nil, err
	}
	release := func() { _, _ = s.MoveChangeSet(ctx, id, store.ChangeSetMerging, store.ChangeSetProposed, "", "", now) }
	in, ordered, docs, err := e.included(ctx, s, id)
	if err != nil {
		release()
		return nil, err
	}
	base := map[string]int{}
	for _, it := range in {
		latest, err := latestNumber(ctx, e.manifests, it.Kind, it.ID)
		if err != nil {
			release()
			return nil, err
		}
		if latest != it.Base {
			release()
			return nil, fmt.Errorf("%w: %s/%s started from version %d, and version %d is saved now", ErrProposalStale, it.Kind, it.ID, it.Base, latest)
		}
		base[it.Kind+"/"+it.ID] = it.Base
	}
	if p, err := e.checkActor(ctx, actor); err != nil {
		release()
		return nil, err
	} else if p != nil {
		release()
		return nil, &ValidationError{Problems: []Problem{*p}}
	}
	why := cs.Title
	if cs.Agent != "" {
		why += " (by " + cs.Agent + ")"
	}
	if reason != "" {
		why += "; " + reason
	}
	var saved []Version
	err = e.manifests.WithinTransaction(ctx, func(ctx context.Context, tx store.ManifestStore) error {
		for _, m := range ordered {
			key := m.Kind + "/" + m.ID
			latest, err := latestNumber(ctx, tx, m.Kind, m.ID)
			if err != nil {
				return err
			}
			if latest != base[key] {
				return fmt.Errorf("%w: %s moved on while it was being accepted", ErrProposalStale, key)
			}
			v := Version{Kind: m.Kind, ID: m.ID, Number: latest + 1, YAML: m.Text, Actor: actor, Reason: why, On: now, Doc: e.storedDoc(m.Kind, docs[key])}
			if err := tx.PutVersion(ctx, v); err != nil {
				return fmt.Errorf("commit %s: %w", key, err)
			}
			if err := e.recordSeries(ctx, tx, v, docs[key]); err != nil {
				return fmt.Errorf("commit %s: %w", key, err)
			}
			if err := tx.IndexReferences(ctx, m.Kind, m.ID, toStoreRefs(extractRefs(docs[key], e.refRules[m.Kind]))); err != nil {
				return err
			}
			saved = append(saved, v)
		}
		return nil
	})
	if err != nil {
		release()
		return nil, err
	}
	for _, v := range saved {
		e.afterVersion(ctx, v)
	}
	for _, it := range in {
		_ = s.DeleteChangeItem(ctx, id, it.Kind, it.ID)
	}
	rest, _ := s.ListChangeItems(ctx, id)
	to := store.ChangeSetMerged
	if len(rest) > 0 {
		// What was trimmed goes back to being worked on.
		to = store.ChangeSetOpen
	}
	if _, err := s.MoveChangeSet(ctx, id, store.ChangeSetMerging, to, actor, reason, now); err != nil {
		return saved, err
	}
	sort.Slice(saved, func(i, j int) bool { return saved[i].Kind+saved[i].ID < saved[j].Kind+saved[j].ID })
	return saved, nil
}

// CloseChangeSet ends a change set without saving it: its person, or who
// works in it.
func (e *Engine) CloseChangeSet(ctx context.Context, id, reason string) (store.ChangeSet, error) {
	s, err := e.changeSetStore()
	if err != nil {
		return store.ChangeSet{}, err
	}
	cs, err := s.GetChangeSet(ctx, id)
	if errors.Is(err, store.ErrNoChangeSet) {
		return store.ChangeSet{}, fmt.Errorf("%w: change set %s", ErrNotFound, id)
	}
	if err != nil {
		return store.ChangeSet{}, err
	}
	p := identity.PrincipalFrom(ctx)
	if cs.For != personKey(p) && !e.mayWorkIn(p, cs) {
		return store.ChangeSet{}, ErrNotTheirChangeSet
	}
	actor := p.Actor(e.operator(ctx))
	for _, from := range []string{store.ChangeSetOpen, store.ChangeSetProposed} {
		out, err := s.MoveChangeSet(ctx, id, from, store.ChangeSetClosed, actor, reason, timeNow().UTC())
		if !errors.Is(err, store.ErrChangeSetMoved) {
			return out, err
		}
	}
	return store.ChangeSet{}, ErrNotOpen
}

// ReopenChangeSet takes a proposed change set back to work, as its person
// asking for changes, or who works in it withdrawing it.
func (e *Engine) ReopenChangeSet(ctx context.Context, id, reason string) (store.ChangeSet, error) {
	s, err := e.changeSetStore()
	if err != nil {
		return store.ChangeSet{}, err
	}
	cs, err := s.GetChangeSet(ctx, id)
	if err != nil {
		return store.ChangeSet{}, err
	}
	p := identity.PrincipalFrom(ctx)
	if cs.For != personKey(p) && !e.mayWorkIn(p, cs) {
		return store.ChangeSet{}, ErrNotTheirChangeSet
	}
	return s.MoveChangeSet(ctx, id, store.ChangeSetProposed, store.ChangeSetOpen, p.Actor(e.operator(ctx)), reason, timeNow().UTC())
}

// changeSetKey carries a change set's drafts on a context, so whatever
// reads a manifest as it stands (checks, the work around it, the order of
// work) reads the change set's draft where it has one.
type changeSetKey struct{}

// InChangeSet returns ctx reading the change set's drafts first.
func (e *Engine) InChangeSet(ctx context.Context, set string) (context.Context, error) {
	s, err := e.changeSetStore()
	if err != nil {
		return ctx, err
	}
	items, err := s.ListChangeItems(ctx, set)
	if err != nil {
		return ctx, err
	}
	texts := make(map[string][]byte, len(items))
	for _, it := range items {
		texts[it.Kind+"/"+it.ID] = it.Text
	}
	return context.WithValue(ctx, changeSetKey{}, texts), nil
}

// inPlay is a manifest's text in the change set on ctx, if it has one.
func inPlay(ctx context.Context, kind, id string) ([]byte, bool) {
	texts, _ := ctx.Value(changeSetKey{}).(map[string][]byte)
	t, ok := texts[kind+"/"+id]
	return t, ok
}

// inPlayRefs is every manifest the change set on ctx holds.
func inPlayRefs(ctx context.Context) []Ref {
	texts, _ := ctx.Value(changeSetKey{}).(map[string][]byte)
	out := make([]Ref, 0, len(texts))
	for key := range texts {
		k, i, _ := strings.Cut(key, "/")
		out = append(out, Ref{Kind: k, ID: i})
	}
	return out
}

// CurrentChangeSet is the change set the principal on ctx works in now,
// without opening one: found is false when they have none open.
func (e *Engine) CurrentChangeSet(ctx context.Context, id string) (store.ChangeSet, bool, error) {
	if id != "" {
		cs, err := e.WorkingChangeSet(ctx, id)
		return cs, err == nil, err
	}
	s, err := e.changeSetStore()
	if err != nil {
		return store.ChangeSet{}, false, err
	}
	open, err := s.ListChangeSets(ctx, store.ChangeSetFilter{Owner: ownerOf(identity.PrincipalFrom(ctx)), Status: store.ChangeSetOpen})
	if err != nil || len(open) == 0 {
		return store.ChangeSet{}, false, err
	}
	return open[0], true, nil
}

// MergeInChangeSet applies to a change set's draft what changed between
// the text a person's editor last had (previous) and what it has now
// (next), field by field, a list being one field, so whatever the agent
// changed meanwhile in other fields is kept. With no previous, next
// replaces the draft.
func (e *Engine) MergeInChangeSet(ctx context.Context, set, kind, id string, previous, next []byte) ([]byte, error) {
	if previous == nil {
		if err := e.SaveInChangeSet(ctx, set, kind, id, next); err != nil {
			return nil, err
		}
		text, _, err := e.ChangeSetText(ctx, set, kind, id)
		return text, err
	}
	var before, after map[string]any
	if err := e.codec.DecodeInto(previous, &before); err != nil {
		return nil, fmt.Errorf("previous: %w", err)
	}
	if err := e.codec.DecodeInto(next, &after); err != nil {
		return nil, err
	}
	put := map[string]any{}
	var unset []string
	fieldChanges("", before, after, put, &unset)
	if len(put) == 0 && len(unset) == 0 {
		text, _, err := e.ChangeSetText(ctx, set, kind, id)
		return text, err
	}
	return e.EditInChangeSet(ctx, set, kind, id, put, unset)
}

// fieldChanges collects, by JSON pointer, every field that differs between
// two documents: an object is walked into, anything else (a list, a
// value) is set whole or cleared.
func fieldChanges(path string, a, b map[string]any, put map[string]any, unset *[]string) {
	esc := func(k string) string { return strings.ReplaceAll(strings.ReplaceAll(k, "~", "~0"), "/", "~1") }
	for k, bv := range b {
		p := path + "/" + esc(k)
		av, had := a[k]
		am, aObj := av.(map[string]any)
		bm, bObj := bv.(map[string]any)
		switch {
		case had && aObj && bObj:
			fieldChanges(p, am, bm, put, unset)
		case !had || !equalValues(av, bv):
			put[p] = bv
		}
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			*unset = append(*unset, path+"/"+esc(k))
		}
	}
}

func equalValues(a, b any) bool { return len(diffValues("", a, b)) == 0 }
