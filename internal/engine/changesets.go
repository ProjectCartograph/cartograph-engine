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
		// Live like every draft: any person may work in an open change
		// set; an agent only in its own (docs/adr/0024).
		if !e.mayWorkIn(p, cs) && !(p.Agent == "" && cs.Status == store.ChangeSetOpen) {
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
		problems := e.agentOrderProblems(ctx, set, kind, id, after)
		problems = append(problems, roleRefProblems(after, e.refRules[kind])...)
		if len(problems) > 0 {
			return &ValidationError{Problems: problems}
		}
	}
	it.Text, it.By, it.At = text, p.Actor(e.operator(ctx)), timeNow().UTC()
	if err := s.PutChangeItem(ctx, it); err != nil {
		return err
	}
	if e.shared != nil {
		e.shared.reconcileItem(ctx, cs.ID, kind, id, text)
	}
	cs.Updated = it.At
	return s.PutChangeSet(ctx, cs)
}

// ChangeSetItem is one item as a reviewer reads it: what it changes from
// the version it started from, its checks with the rest of the change set,
// and whether the record has moved on since.
type ChangeSetItem struct {
	Item store.ChangeItem
	Name string
	// Proposed is "new" when the change set creates the record, else
	// "changed".
	Proposed string
	Changes  []Change
	Checks   []Check
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
	overlay := map[string]map[string]map[string]any{}
	for key, doc := range docs {
		kind, id, _ := strings.Cut(key, "/")
		if overlay[kind] == nil {
			overlay[kind] = map[string]map[string]any{}
		}
		overlay[kind][id] = doc
	}
	for _, it := range items {
		doc := docs[it.Kind+"/"+it.ID]
		item := ChangeSetItem{Item: it, Changes: []Change{}, Checks: []Check{}, Proposed: "new"}
		if _, found, err := e.manifests.GetCurrent(ctx, it.Kind, it.ID); err == nil && found {
			item.Proposed = "changed"
		}
		if meta, ok := doc["metadata"].(map[string]any); ok {
			item.Name, _ = meta["name"].(string)
		}
		before := map[string]any{}
		if it.Base > 0 {
			if v, err := e.GetVersion(ctx, it.Kind, it.ID, it.Base); err == nil {
				_ = e.codec.DecodeInto(v.YAML, &before)
			}
		}
		switch it.Op {
		case store.ItemDelete:
			// The whole record goes: nothing in it to diff or check.
			item.Changes = []Change{{Path: "", Op: "remove"}}
		case store.ItemState:
			item.Changes = append(diffValues("", before, doc), Change{Path: "/state", Op: "replace", To: it.State})
		default:
			item.Changes = diffValues("", before, doc)
			if checks, err := e.ChecksOf(checkCtx, it.Kind, it.ID, it.Text); err == nil {
				item.Checks = checks
			}
			// What the schema requires is not advice: the record cannot
			// be merged without it, so the review says so before anyone
			// tries.
			item.Checks = append(item.Checks, e.requiredChecks(ctx, it.Kind, it.Text, overlay)...)
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

// registerKinds are the registers a definition names from: a draft of one
// that nothing names was declared for nothing.
var registerKinds = map[string]bool{
	"Team": true, "Resource": true, "Unit": true, "ReportingCycle": true, "Segment": true,
	"DataSource": true, "FundingSource": true, "BeneficiaryGroup": true, "Assumption": true,
}

// UnnamedInChangeSet lists the register drafts in a change set that no
// record and no other draft names: declared, as a document listed them,
// and then never used. Not wrong in itself, but worth a look before
// proposing: either something should name it, or it should not be there.
func (e *Engine) UnnamedInChangeSet(ctx context.Context, set string) ([]Ref, error) {
	s, err := e.changeSetStore()
	if err != nil {
		return nil, err
	}
	items, err := s.ListChangeItems(ctx, set)
	if err != nil {
		return nil, err
	}
	var out []Ref
	for _, it := range items {
		if !it.Included || !registerKinds[it.Kind] {
			continue
		}
		refs, err := e.referencing(ctx, it.Kind, it.ID)
		if err != nil {
			return nil, err
		}
		if len(refs) == 0 {
			out = append(out, Ref{Kind: it.Kind, ID: it.ID})
		}
	}
	return out, nil
}

// LeaveOpen records that a check on a draft is left for the person the
// change set is for, with the reason they will read: something only they
// can settle (a figure no document gives, a score nobody has made). The
// order of work then passes it by, and proposing waives it with that
// reason, so an agent says why once, as it goes, rather than all at the
// end. An empty reason takes it back; a second reason is added to the
// first unless correct puts it in its place.
func (e *Engine) LeaveOpen(ctx context.Context, set, kind, id, check, reason string, correct bool) error {
	s, err := e.changeSetStore()
	if err != nil {
		return err
	}
	cs, err := e.WorkingChangeSet(ctx, set)
	if err != nil {
		return err
	}
	on := kind + "/" + id
	reason = strings.TrimSpace(reason)
	kept := cs.Waivers[:0:0]
	for _, w := range cs.Waivers {
		if w.On != on || w.Check != check {
			kept = append(kept, w)
			continue
		}
		// A check two missing facts leave open keeps both reasons: the
		// person reads every one. No reason takes the check back.
		if reason != "" && !correct && !strings.Contains(w.Reason, reason) {
			reason = w.Reason + " Also: " + reason
		} else if reason != "" && !correct {
			reason = w.Reason
		}
	}
	if reason != "" {
		kept = append(kept, store.Waiver{On: on, Check: check, Reason: reason})
	}
	cs.Waivers, cs.Updated = kept, timeNow().UTC()
	return s.PutChangeSet(ctx, cs)
}

// leftKey carries the checks a change set leaves for its person, as
// "Kind/id#check" to the reason.
type leftKey struct{}

// LeftFor is the reason a check on a manifest is left for the person, in
// the change set on ctx, or "".
func LeftFor(ctx context.Context, kind, id, check string) string {
	return leftFor(ctx, kind, id, check)
}

func leftFor(ctx context.Context, kind, id, check string) string {
	left, _ := ctx.Value(leftKey{}).(map[string]string)
	if why := left[kind+"/"+id+"#"+check]; why != "" || kind != "Goal" {
		return why
	}
	if check == "smart-relevant" {
		// A goal is judged relevant against the vision and mission: left
		// for the person there, it waits on them here.
		doc := proposedDocs(ctx)["Goal/"+id]
		spec, _ := doc["spec"].(map[string]any)
		// Only once it says why it matters: that is the agent's to write.
		if why, _ := spec["whyItMatters"].(string); spec["level"] == "goal" && strings.TrimSpace(why) != "" {
			for _, c := range []string{"purpose-vision", "purpose-mission"} {
				if why := left["Purpose/default#"+c]; why != "" {
					return why
				}
			}
		}
		return ""
	}
	if !aimMeasureChecks[check] {
		return ""
	}
	// An aim's measures fail while a KPI aligned to it waits on a figure
	// left for the person: the same missing fact, so the same reason,
	// word for word, so the person reads it once with every check it
	// holds open, without the agent having to foresee every aim it
	// reaches.
	for key, doc := range proposedDocs(ctx) {
		if !strings.HasPrefix(key, "KPI/") {
			continue
		}
		spec, _ := doc["spec"].(map[string]any)
		goals, _ := spec["goals"].([]any)
		named := false
		for _, g := range goals {
			named = named || g == id
		}
		if !named {
			continue
		}
		for _, c := range []string{"kpi-target", "kpi-baseline"} {
			if why := left[key+"#"+c]; why != "" {
				return why
			}
		}
	}
	return ""
}

// DiscardChangeItem drops a draft from a change set the principal works
// in, as if it had never been drafted there: a draft saved under the wrong
// id, or one the work no longer needs. Refused while another draft in the
// set names it, listing them, since those would then name nothing.
func (e *Engine) DiscardChangeItem(ctx context.Context, set, kind, id string) error {
	s, err := e.changeSetStore()
	if err != nil {
		return err
	}
	cs, err := e.WorkingChangeSet(ctx, set)
	if err != nil {
		return err
	}
	items, err := s.ListChangeItems(ctx, cs.ID)
	if err != nil {
		return err
	}
	found := false
	var naming []Problem
	for _, it := range items {
		if it.Kind == kind && it.ID == id {
			found = true
			continue
		}
		var doc map[string]any
		if e.codec.DecodeInto(it.Text, &doc) != nil {
			continue
		}
		for _, r := range extractRefs(doc, e.refRules[it.Kind]) {
			if r.kind == kind && r.id == id {
				naming = append(naming, Problem{Path: it.Kind + "/" + it.ID + r.path,
					Message: fmt.Sprintf("names %s/%s: change it, or discard it first", kind, id)})
				break
			}
		}
	}
	if !found {
		return fmt.Errorf("%w: %s/%s is not in the change set", ErrNotFound, kind, id)
	}
	if len(naming) > 0 {
		return &ValidationError{Problems: naming}
	}
	return s.DeleteChangeItem(ctx, cs.ID, kind, id)
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
		if !it.Included {
			continue
		}
		in = append(in, it)
		// Only a save is a new version; a delete or a state change acts
		// on the record as it stands once the saves are in.
		if it.Op == store.ItemSave {
			members = append(members, SetMember{Kind: it.Kind, ID: it.ID, Text: it.Text})
		}
	}
	if len(in) == 0 {
		return nil, nil, nil, fmt.Errorf("%w: nothing in the change set is included", ErrConflict)
	}
	if len(members) == 0 {
		return in, nil, map[string]map[string]any{}, nil
	}
	ordered, docs, err := e.checkMembers(ctx, members)
	return in, ordered, docs, err
}

// HandoffFunc hands a project off: renders its charter and records the
// hand-off (Engine.Handoff). The caller that can render supplies it, on
// the context a change set is rolled in with (WithHandoff); without one,
// a change set's hand-off item is kept for later.
type HandoffFunc func(ctx context.Context, projectID, actor, reason string) error

type handoffKey struct{}

// WithHandoff returns ctx carrying how to hand a project off.
func WithHandoff(ctx context.Context, f HandoffFunc) context.Context {
	return context.WithValue(ctx, handoffKey{}, f)
}

// MarkInChangeSet makes a change set's item of a manifest delete it
// (store.ItemDelete), or move a project to another state
// (store.ItemState, to). The item starts from the record as it stands,
// which must exist; an item the change set already holds keeps its text.
// Rolling the change set in applies it after the saves (docs/adr/0024).
func (e *Engine) MarkInChangeSet(ctx context.Context, set, kind, id, op, to string) error {
	switch {
	case op == store.ItemDelete:
	case op == store.ItemState && kind == "Project" && to != "":
	default:
		return fmt.Errorf("%w: %s on %s/%s", ErrBadEdit, op, kind, id)
	}
	s, err := e.changeSetStore()
	if err != nil {
		return err
	}
	cs, err := e.WorkingChangeSet(ctx, set)
	if err != nil {
		return err
	}
	v, found, err := e.manifests.GetCurrent(ctx, kind, id)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%w: %s/%s", ErrNotFound, kind, id)
	}
	if op == store.ItemDelete {
		if err := e.guardDoc(ctx, kind, id, nil); err != nil {
			return err
		}
	}
	it, held, err := s.GetChangeItem(ctx, cs.ID, kind, id)
	if err != nil {
		return err
	}
	if !held {
		it = store.ChangeItem{Set: cs.ID, Kind: kind, ID: id, Text: e.normalizeLegacy(kind, v.YAML), Base: v.Number, Included: true}
	}
	p := identity.PrincipalFrom(ctx)
	it.Op, it.State, it.By, it.At = op, to, p.Actor(e.operator(ctx)), timeNow().UTC()
	if op == store.ItemDelete {
		it.State = ""
	}
	if err := s.PutChangeItem(ctx, it); err != nil {
		return err
	}
	cs.Updated = it.At
	return s.PutChangeSet(ctx, cs)
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
	left := map[string]string{}
	for _, w := range cs.Waivers {
		left[w.On+"#"+w.Check] = w.Reason
	}
	open, err := e.openIn(context.WithValue(ctx, leftKey{}, left), s, cs.ID)
	if err != nil {
		return store.ChangeSet{}, err
	}
	// The checks left for the person as the work went, with their
	// reasons; one given now as well wins.
	recorded := map[string]map[string]string{}
	for _, w := range cs.Waivers {
		if recorded[w.On] == nil {
			recorded[w.On] = map[string]string{}
		}
		recorded[w.On][w.Check] = w.Reason
	}
	for on, checks := range waive {
		for check, why := range checks {
			if recorded[on] == nil {
				recorded[on] = map[string]string{}
			}
			if strings.TrimSpace(why) != "" {
				recorded[on][check] = why
			}
		}
	}
	waive = recorded
	var unmet []OpenCheck
	var waivers []store.Waiver
	for _, c := range open {
		why := strings.TrimSpace(waive[c.Kind+"/"+c.ManifestID][c.ID])
		if why == "" {
			// Left by way of another draft's check (an aim waiting on a
			// KPI's figure).
			why = c.Left
		}
		if why != "" {
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
	// A set with nothing to propose has nothing open.
	items, err := s.ListChangeItems(ctx, id)
	if err != nil {
		return nil, err
	}
	any := false
	for _, it := range items {
		any = any || it.Included
	}
	if !any {
		return nil, nil
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
				open = append(open, OpenCheck{Kind: m.Kind, ManifestID: m.ID, Check: c, Left: leftFor(checkCtx, m.Kind, m.ID, c.ID)})
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
// AcceptResult is what rolling a change set in did: the versions it
// saved, the records it deleted and the projects it moved, and what it
// could not apply, which stays in the change set.
type AcceptResult struct {
	Saved   []Version
	Deleted []Ref
	Moved   []Ref
	Kept    []Problem
}

func (e *Engine) AcceptChangeSet(ctx context.Context, id, reason string) (AcceptResult, error) {
	saved, done, kept, err := e.acceptChangeSet(ctx, id, reason)
	out := AcceptResult{Saved: saved, Kept: kept}
	for _, it := range done {
		switch it.Op {
		case store.ItemDelete:
			out.Deleted = append(out.Deleted, Ref{Kind: it.Kind, ID: it.ID})
		case store.ItemState:
			out.Moved = append(out.Moved, Ref{Kind: it.Kind, ID: it.ID})
		}
	}
	return out, err
}

func (e *Engine) acceptChangeSet(ctx context.Context, id, reason string) ([]Version, []store.ChangeItem, []Problem, error) {
	if err := refuseAgent(ctx); err != nil {
		return nil, nil, nil, err
	}
	s, err := e.changeSetStore()
	if err != nil {
		return nil, nil, nil, err
	}
	cs, err := s.GetChangeSet(ctx, id)
	if errors.Is(err, store.ErrNoChangeSet) {
		return nil, nil, nil, fmt.Errorf("%w: change set %s", ErrNotFound, id)
	}
	if err != nil {
		return nil, nil, nil, err
	}
	p := identity.PrincipalFrom(ctx)
	if cs.For != personKey(p) {
		return nil, nil, nil, ErrNotTheirChangeSet
	}
	actor := p.Actor(e.operator(ctx))
	now := timeNow().UTC()
	// Claimed, so nobody else accepts or closes it meanwhile.
	if _, err := s.MoveChangeSet(ctx, id, store.ChangeSetProposed, store.ChangeSetMerging, actor, reason, now); err != nil {
		if errors.Is(err, store.ErrChangeSetMoved) {
			return nil, nil, nil, fmt.Errorf("%w: the change set is not proposed", ErrConflict)
		}
		return nil, nil, nil, err
	}
	release := func() { _, _ = s.MoveChangeSet(ctx, id, store.ChangeSetMerging, store.ChangeSetProposed, "", "", now) }
	in, ordered, docs, err := e.included(ctx, s, id)
	if err != nil {
		release()
		return nil, nil, nil, err
	}
	base := map[string]int{}
	for _, it := range in {
		latest, err := latestNumber(ctx, e.manifests, it.Kind, it.ID)
		if err != nil {
			release()
			return nil, nil, nil, err
		}
		if latest != it.Base {
			release()
			return nil, nil, nil, fmt.Errorf("%w: %s/%s started from version %d, and version %d is saved now", ErrProposalStale, it.Kind, it.ID, it.Base, latest)
		}
		base[it.Kind+"/"+it.ID] = it.Base
		// A state change that cannot be made is refused before anything
		// is saved, not after.
		if it.Op == store.ItemState && it.State != "handed off" {
			if _, err := e.checkTransition(ctx, it.ID, it.State, actor, reason); err != nil {
				release()
				return nil, nil, nil, err
			}
		}
	}
	if p, err := e.checkActor(ctx, actor); err != nil {
		release()
		return nil, nil, nil, err
	} else if p != nil {
		release()
		return nil, nil, nil, &ValidationError{Problems: []Problem{*p}}
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
		return nil, nil, nil, err
	}
	for _, v := range saved {
		e.afterVersion(ctx, v)
	}
	// Deletes, then state changes, on the record as the saves left it;
	// one that cannot be applied stays in the change set, with why.
	var done []store.ChangeItem
	var kept []Problem
	for _, op := range []string{store.ItemSave, store.ItemDelete, store.ItemState} {
		for _, it := range in {
			if it.Op != op {
				continue
			}
			var err error
			switch op {
			case store.ItemDelete:
				err = e.Delete(ctx, it.Kind, it.ID, actor, why)
			case store.ItemState:
				if it.State == "handed off" {
					handoff, _ := ctx.Value(handoffKey{}).(HandoffFunc)
					if handoff == nil {
						err = fmt.Errorf("a hand-off is made where the charter can be rendered")
					} else {
						err = handoff(ctx, it.ID, actor, why)
					}
				} else {
					_, err = e.TransitionProjectState(ctx, it.ID, it.State, actor, why)
				}
			}
			if err != nil {
				kept = append(kept, Problem{Path: it.Kind + "/" + it.ID, Message: err.Error()})
				continue
			}
			done = append(done, it)
			_ = s.DeleteChangeItem(ctx, id, it.Kind, it.ID)
		}
	}
	rest, _ := s.ListChangeItems(ctx, id)
	to := store.ChangeSetMerged
	if len(rest) > 0 {
		// What was trimmed, or could not be applied, goes back to being
		// worked on.
		to = store.ChangeSetOpen
	}
	if _, err := s.MoveChangeSet(ctx, id, store.ChangeSetMerging, to, actor, reason, now); err != nil {
		return saved, done, kept, err
	}
	sort.Slice(saved, func(i, j int) bool { return saved[i].Kind+saved[i].ID < saved[j].Kind+saved[j].ID })
	return saved, done, kept, nil
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
	// Whether each item's record exists: a record in a vault may have no
	// numbered version, so a base of 0 does not mean the change set
	// creates it.
	exists := make(map[string]bool, len(items))
	removed := map[string]bool{}
	for _, it := range items {
		// A record the change set removes reads as gone, not as its text.
		if it.Op == store.ItemDelete {
			if it.Included {
				removed[it.Kind+"/"+it.ID] = true
			}
			continue
		}
		texts[it.Kind+"/"+it.ID] = it.Text
		_, found, err := e.manifests.GetCurrent(ctx, it.Kind, it.ID)
		exists[it.Kind+"/"+it.ID] = err == nil && found
	}
	ctx = context.WithValue(ctx, basesKey{}, exists)
	ctx = context.WithValue(ctx, removedKey{}, removed)
	// What it leaves for its person, so the order of work passes it by.
	left := map[string]string{}
	if cs, err := s.GetChangeSet(ctx, set); err == nil {
		for _, w := range cs.Waivers {
			left[w.On+"#"+w.Check] = w.Reason
		}
	}
	ctx = context.WithValue(ctx, leftKey{}, left)
	// The drafts parsed too, so whatever reads the record on this context
	// (a lookup, a check, a guide's plan, relevance) reads them as if
	// saved.
	return e.withInPlay(context.WithValue(ctx, changeSetKey{}, texts)), nil
}

// Preview reads as if a change set were accepted: every read on the
// context it returns sees the change set's drafts in place of the
// records they change, and the records it creates (docs/adr/0024).
// ErrNotFound when there is no such change set.
func (e *Engine) Preview(ctx context.Context, set string) (context.Context, error) {
	s, err := e.changeSetStore()
	if err != nil {
		return ctx, err
	}
	if _, err := s.GetChangeSet(ctx, set); errors.Is(err, store.ErrNoChangeSet) {
		return ctx, fmt.Errorf("%w: change set %s", ErrNotFound, set)
	} else if err != nil {
		return ctx, err
	}
	return e.InChangeSet(ctx, set)
}

// basesKey carries whether the record behind each of a change set's
// items exists, "Kind/id" to true, false for one the change set creates.
type basesKey struct{}

// Proposed says how a manifest stands in the change set read on ctx:
// "new" when the change set creates it, "changed" when it changes one
// that exists, "" when it is not in the change set or none is read.
func Proposed(ctx context.Context, kind, id string) string {
	exists, _ := ctx.Value(basesKey{}).(map[string]bool)
	found, ok := exists[kind+"/"+id]
	switch {
	case !ok:
		return ""
	case !found:
		return "new"
	default:
		return "changed"
	}
}

// removedKey carries the records the change set on ctx removes.
type removedKey struct{}

// removedInPlay reports whether the change set on ctx removes kind/id.
func removedInPlay(ctx context.Context, kind, id string) bool {
	removed, _ := ctx.Value(removedKey{}).(map[string]bool)
	return removed[kind+"/"+id]
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

// CurrentChangeSet is the change set the principal on ctx reads in now,
// without opening one: the one named, open or proposed; else their
// latest open one; else the one they proposed last, whose drafts still
// stand until it is accepted, so an agent that has proposed can follow
// its work. found is false when there is none.
func (e *Engine) CurrentChangeSet(ctx context.Context, id string) (store.ChangeSet, bool, error) {
	s, err := e.changeSetStore()
	if err != nil {
		return store.ChangeSet{}, false, err
	}
	p := identity.PrincipalFrom(ctx)
	if id != "" {
		cs, err := s.GetChangeSet(ctx, id)
		if errors.Is(err, store.ErrNoChangeSet) {
			return store.ChangeSet{}, false, fmt.Errorf("%w: change set %s", ErrNotFound, id)
		}
		if err != nil {
			return store.ChangeSet{}, false, err
		}
		if !e.mayWorkIn(p, cs) {
			return store.ChangeSet{}, false, ErrNotTheirChangeSet
		}
		if cs.Status != store.ChangeSetOpen && cs.Status != store.ChangeSetProposed {
			return store.ChangeSet{}, false, ErrNotOpen
		}
		return cs, true, nil
	}
	for _, status := range []string{store.ChangeSetOpen, store.ChangeSetProposed} {
		sets, err := s.ListChangeSets(ctx, store.ChangeSetFilter{Owner: ownerOf(p), Status: status})
		if err != nil {
			return store.ChangeSet{}, false, err
		}
		if len(sets) > 0 {
			return sets[0], true, nil
		}
	}
	return store.ChangeSet{}, false, nil
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

// roleFields are where a definition names who: who verifies, owns,
// confirms, decides, does a task or issued a mandate.
var roleFields = []string{"/by", "/owner", "/confirmedBy", "/escalate/to", "/issuer", "/role"}

// roleRefProblems holds an agent's draft to naming a role where a role is
// asked for: one of the work's roles, a Resource from the catalogue (a
// governance body or a unit among them) or a party outside the workspace,
// never a Team. A team carries work and scopes who may edit it; who owns
// or decides is a role in it, or the unit as a Resource. People may have
// saved a team there before; an agent is told what to write instead.
func roleRefProblems(doc map[string]any, rules []refRule) []Problem {
	var out []Problem
	for _, r := range extractRefs(doc, rules) {
		if r.kind != "Team" {
			continue
		}
		for _, f := range roleFields {
			if strings.HasSuffix(strings.TrimSuffix(r.path, "/id"), f) {
				out = append(out, Problem{Path: r.path, Message: "names a team where a role is asked for: name the role that answers for it, as one of this work's roles ({\"local\":\"resources\",\"id\":...}) or a Resource from the catalogue ({\"kind\":\"Resource\",\"id\":...}, a unit of category orgUnit where the unit as a whole answers), or a party outside the workspace ({\"external\":...})"})
				break
			}
		}
	}
	return out
}

// requiredChecks turns what stops a record being saved as a version into
// blocking checks, one per field, so a change set's review names them
// before it is merged rather than refusing the merge.
func (e *Engine) requiredChecks(ctx context.Context, kind string, text []byte, overlay map[string]map[string]map[string]any) []Check {
	// The change set's own records count as there: a link to a record it
	// creates is not a link to nothing.
	_, problems, err := e.validate(ctx, kind, text, overlay)
	if err != nil {
		return nil
	}
	out := make([]Check, 0, len(problems))
	for _, p := range problems {
		path := p.Path
		message := p.Message
		// A missing property is reported on the object that lacks it;
		// the field is the property.
		// A value outside a list is said without the list, which for a
		// currency is every code there is.
		switch {
		case strings.HasPrefix(p.Message, "value must be one of"):
			message = "Choose one of the values offered."
		case strings.HasPrefix(p.Message, "minItems:"):
			message = "Add at least one."
		case strings.HasPrefix(p.Message, "maxItems:"):
			message = "There are more than this allows; remove some."
		case strings.HasPrefix(p.Message, "maxLength:"):
			message = "This is longer than allowed; shorten it."
		case strings.HasPrefix(p.Message, "minLength:"):
			message = "Needed before this can be merged."
		}
		if i := strings.Index(p.Message, "missing property '"); i >= 0 {
			message = "Needed before this can be merged."
			name := strings.TrimSuffix(p.Message[i+len("missing property '"):], "'")
			if j := strings.Index(name, "'"); j >= 0 {
				name = name[:j]
			}
			path = strings.TrimSuffix(path, "/") + "/" + name
		}
		out = append(out, Check{ID: "required:" + path, State: checkBlock, Message: message, Path: path})
	}
	return out
}
