package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/identity"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/kinds"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

// An agent reads, edits drafts, and proposes what would make the record;
// the person it acts for decides (docs/adr/0016). A proposal keeps
// exactly what was proposed and the version it was proposed against, is
// checked when proposed as a save would be, and is checked again when
// accepted, as the person, under their own authority.

var (
	// ErrProposalStale is a proposal whose manifest changed since: its
	// person reviews it again rather than overwrite what changed.
	ErrProposalStale = errors.New("the manifest has changed since this was proposed")
	// ErrNotTheirs refuses a decision by anyone but the person the
	// proposal was made for.
	ErrNotTheirs = fmt.Errorf("%w: only the person an agent acts for decides its proposals", identity.ErrForbidden)
	// ErrNotAnAgent refuses a proposal from a person, who saves instead.
	ErrNotAnAgent = errors.New("an agent proposes; a person saves")
	// ErrNoProposals is a store that keeps no proposals.
	ErrNoProposals = fmt.Errorf("%w: this store keeps no proposals", ErrNotFound)
)

func (e *Engine) proposalStore() (store.ProposalStore, error) {
	ps, ok := e.manifests.(store.ProposalStore)
	if !ok {
		return nil, ErrNoProposals
	}
	return ps, nil
}

// personKey is how a proposal names its person: their address, else
// their subject; "" for an anonymous operator.
func personKey(p identity.Principal) string {
	if p.Anonymous {
		return ""
	}
	if email := emailOf(p); email != "" {
		return email
	}
	return p.Subject
}

func newProposalID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// propose keeps a proposal from the agent on ctx.
func (e *Engine) propose(ctx context.Context, p store.Proposal) (store.Proposal, error) {
	ps, err := e.proposalStore()
	if err != nil {
		return store.Proposal{}, err
	}
	who := identity.PrincipalFrom(ctx)
	if who.Agent == "" {
		return store.Proposal{}, ErrNotAnAgent
	}
	p.ID, p.Agent, p.For, p.At, p.Status = newProposalID(), who.Agent, personKey(who), timeNow().UTC(), store.ProposalOpen
	if err := ps.PutProposal(ctx, p); err != nil {
		return store.Proposal{}, err
	}
	return p, nil
}

// OpenChecksError refuses a proposal whose checks are not all met and not
// all waived: the agent asks its person for what is missing, or names
// each check it leaves open and why (docs/adr/0016).
type OpenChecksError struct {
	Open []OpenCheck
}

// OpenCheck is a check still open on one manifest of a proposal.
type OpenCheck struct {
	Kind, ManifestID string
	Check
	// Left is the reason it is left for the person, where the agent said
	// so as it went.
	Left string
}

func (e *OpenChecksError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d check%s still open; meet each, or waive it with a reason:", len(e.Open), plural(len(e.Open)))
	for _, c := range e.Open {
		fmt.Fprintf(&b, " %s/%s %s (%s)", c.Kind, c.ManifestID, c.ID, strings.TrimSuffix(c.Message, "."))
	}
	return b.String()
}

// SetMember is one manifest of a proposal: its kind, id and text.
type SetMember struct {
	Kind, ID string
	Text     []byte
}

// ProposeSave proposes committing text as the manifest's next version:
// ProposeSet with one member, its waivers keyed by check.
func (e *Engine) ProposeSave(ctx context.Context, kind, id string, text []byte, reason string, waive map[string]string) (store.Proposal, error) {
	ps, err := e.proposeManifests(ctx, []SetMember{{Kind: kind, ID: id, Text: text}}, reason, map[string]map[string]string{kind + "/" + id: waive}, false)
	if err != nil {
		return store.Proposal{}, err
	}
	return ps[0], nil
}

// ProposeSet proposes several manifests that stand or fall together, such
// as a KPI, the gap it measures and the outcome that closes it: they are
// validated against each other, so one may reference another; accepted
// together in one transaction, in the order their references need; or
// declined together. waive is keyed by "Kind/id", then by check.
func (e *Engine) ProposeSet(ctx context.Context, members []SetMember, reason string, waive map[string]map[string]string) ([]store.Proposal, error) {
	return e.proposeManifests(ctx, members, reason, waive, true)
}

// proposeManifests checks members as a save of all of them would be:
// against the schema, the rules and the person's authority, and against
// every check each kind has, which a person sees in the editor. A check
// still open refuses the proposal (OpenChecksError) unless waive names it
// with a reason; waived checks go on the proposal for the person to see.
// Each member becomes its manifest's draft, so the person opens it in the
// editor and works on it with the agent.
func (e *Engine) proposeManifests(ctx context.Context, members []SetMember, reason string, waive map[string]map[string]string, asSet bool) ([]store.Proposal, error) {
	if identity.PrincipalFrom(ctx).Agent == "" {
		return nil, ErrNotAnAgent
	}
	if len(members) == 0 {
		return nil, fmt.Errorf("%w: nothing to propose", ErrConflict)
	}
	ordered, docs, err := e.checkMembers(ctx, members)
	if err != nil {
		return nil, err
	}
	checkCtx := withProposed(ctx, docs)
	var unmet []OpenCheck
	waivers := map[string][]store.Waiver{}
	for _, m := range ordered {
		key := m.Kind + "/" + m.ID
		checks, err := e.ChecksOf(checkCtx, m.Kind, m.ID, m.Text)
		if err != nil {
			return nil, err
		}
		for _, c := range checks {
			if !c.Open() {
				continue
			}
			if why := strings.TrimSpace(waive[key][c.ID]); why != "" {
				waivers[key] = append(waivers[key], store.Waiver{Check: c.ID, Message: c.Message, Reason: why})
				continue
			}
			unmet = append(unmet, OpenCheck{Kind: m.Kind, ManifestID: m.ID, Check: c})
		}
	}
	if len(unmet) > 0 {
		return nil, &OpenChecksError{Open: unmet}
	}
	actor := identity.PrincipalFrom(ctx).Actor(e.operator(ctx))
	set := ""
	if asSet && len(ordered) > 1 {
		set = newProposalID()
	}
	out := make([]store.Proposal, 0, len(ordered))
	for i, m := range ordered {
		base, err := latestNumber(ctx, e.manifests, m.Kind, m.ID)
		if err != nil {
			return nil, err
		}
		if err := e.SaveWorking(ctx, m.Kind, m.ID, m.Text, actor); err != nil {
			return nil, err
		}
		p, err := e.propose(ctx, store.Proposal{Kind: m.Kind, ManifestID: m.ID, Op: store.ProposeSave, Text: m.Text, Base: base,
			Reason: reason, Waivers: waivers[m.Kind+"/"+m.ID], Set: set, SetIndex: i})
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// checkMembers validates members against the store and each other, checks
// the person's authority over each, and orders them so each comes after
// the members it references.
func (e *Engine) checkMembers(ctx context.Context, members []SetMember) ([]SetMember, map[string]map[string]any, error) {
	overlay := map[string]map[string]map[string]any{}
	docs := map[string]map[string]any{}
	for _, m := range members {
		if _, ok := kinds.ByName(m.Kind); !ok {
			return nil, nil, fmt.Errorf("%w: %s", ErrUnknownKind, m.Kind)
		}
		var doc map[string]any
		if err := e.codec.DecodeInto(m.Text, &doc); err != nil {
			return nil, nil, &ValidationError{Problems: []Problem{{Path: m.Kind + "/" + m.ID, Message: "not a manifest: " + err.Error()}}}
		}
		if metaID, ok := docID(doc); !ok || metaID != m.ID {
			return nil, nil, fmt.Errorf("%w: %s manifest metadata.id %q does not match %q", ErrConflict, m.Kind, metaID, m.ID)
		}
		if _, dup := docs[m.Kind+"/"+m.ID]; dup {
			return nil, nil, fmt.Errorf("%w: %s/%s is proposed twice", ErrConflict, m.Kind, m.ID)
		}
		if overlay[m.Kind] == nil {
			overlay[m.Kind] = map[string]map[string]any{}
		}
		overlay[m.Kind][m.ID] = doc
		docs[m.Kind+"/"+m.ID] = doc
	}
	var problems []Problem
	for _, m := range members {
		doc, ps, err := e.validate(ctx, m.Kind, m.Text, overlay)
		if err != nil {
			return nil, nil, err
		}
		more, err := e.introducedProblems(ctx, m.Kind, doc, m.Text)
		if err != nil {
			return nil, nil, err
		}
		ps = append(ps, more...)
		for _, p := range ps {
			if len(members) > 1 {
				p.Path = m.Kind + "/" + m.ID + p.Path
			}
			problems = append(problems, p)
		}
		if len(ps) == 0 {
			if err := e.guardDoc(ctx, m.Kind, m.ID, doc); err != nil {
				return nil, nil, err
			}
		}
	}
	if len(problems) > 0 {
		return nil, nil, &ValidationError{Problems: problems}
	}
	// Each member after those it references: a depth-first walk over the
	// references between members.
	placed := map[string]bool{}
	visiting := map[string]bool{}
	byKey := map[string]SetMember{}
	for _, m := range members {
		byKey[m.Kind+"/"+m.ID] = m
	}
	var ordered []SetMember
	var visit func(key string)
	visit = func(key string) {
		if placed[key] || visiting[key] {
			return
		}
		visiting[key] = true
		m := byKey[key]
		for _, r := range extractRefs(docs[key], e.refRules[m.Kind]) {
			if _, in := byKey[r.kind+"/"+r.id]; in {
				visit(r.kind + "/" + r.id)
			}
		}
		visiting[key] = false
		placed[key] = true
		ordered = append(ordered, m)
	}
	for _, m := range members {
		visit(m.Kind + "/" + m.ID)
	}
	return ordered, docs, nil
}

// operator is who a write records when nobody is signed in.
func (e *Engine) operator(ctx context.Context) string {
	if s, err := e.GetSettings(ctx); err == nil && s.Operator != "" {
		return s.Operator
	}
	return "local"
}

// ProposeAppend proposes recording one item of a series, checked as the
// manifest with it would be.
func (e *Engine) ProposeAppend(ctx context.Context, kind, id, series string, item map[string]any, reason string) (store.Proposal, error) {
	text, base, err := e.withSeriesItem(ctx, kind, id, series, item)
	if err != nil {
		return store.Proposal{}, err
	}
	doc, problems, err := e.validate(ctx, kind, text, nil)
	if err != nil {
		return store.Proposal{}, err
	}
	if more, err := e.introducedProblems(ctx, kind, doc, text); err != nil {
		return store.Proposal{}, err
	} else {
		problems = append(problems, more...)
	}
	if len(problems) > 0 {
		return store.Proposal{}, &ValidationError{Problems: problems}
	}
	if err := e.guardDoc(ctx, kind, id, doc); err != nil {
		return store.Proposal{}, err
	}
	b, err := json.Marshal(item)
	if err != nil {
		return store.Proposal{}, err
	}
	return e.propose(ctx, store.Proposal{Kind: kind, ManifestID: id, Op: store.ProposeAppend, Series: series, Item: b, Base: base, Reason: reason})
}

// ProposeState proposes moving a project, checked against the state
// machine and, for a handoff, its blocking checks.
func (e *Engine) ProposeState(ctx context.Context, id, to, reason string) (store.Proposal, error) {
	if _, found, err := e.manifests.GetCurrent(ctx, "Project", id); err != nil {
		return store.Proposal{}, err
	} else if !found {
		return store.Proposal{}, fmt.Errorf("%w: Project/%s", ErrNotFound, id)
	}
	if err := e.guardStored(ctx, "Project", id); err != nil {
		return store.Proposal{}, err
	}
	if _, err := e.checkTransition(ctx, id, to, personKey(identity.PrincipalFrom(ctx)), reason); err != nil {
		return store.Proposal{}, err
	}
	base, err := latestNumber(ctx, e.manifests, "Project", id)
	if err != nil {
		return store.Proposal{}, err
	}
	return e.propose(ctx, store.Proposal{Kind: "Project", ManifestID: id, Op: store.ProposeState, State: to, Base: base, Reason: reason})
}

// Proposals lists proposals. With no manifest named, only the caller's:
// those made for them, by their agents. With one, every proposal on it,
// for anyone who may read it.
func (e *Engine) Proposals(ctx context.Context, f store.ProposalFilter) ([]store.Proposal, error) {
	ps, err := e.proposalStore()
	if err != nil {
		return []store.Proposal{}, nil
	}
	if f.ManifestID == "" {
		f.For = personKey(identity.PrincipalFrom(ctx))
	}
	return ps.ListProposals(ctx, f)
}

// Handoff is how the caller of AcceptProposal hands a project off, which
// renders and prints a charter the engine does not: it returns the
// version handed off.
type Handoff func(ctx context.Context, projectID, reason string) (int, error)

// AcceptProposal makes the record a proposal proposed, as the person on
// ctx, who must be the one it was made for and not an agent. It refuses
// one whose manifest changed since (ErrProposalStale). handoff does a
// handoff when the proposal is one.
func (e *Engine) AcceptProposal(ctx context.Context, id, actor, reason string, handoff Handoff) (store.Proposal, error) {
	prop, err := e.decidable(ctx, id)
	if err != nil {
		return store.Proposal{}, err
	}
	if prop.Set != "" {
		return e.acceptSet(ctx, prop, actor, reason)
	}
	latest, err := latestNumber(ctx, e.manifests, prop.Kind, prop.ManifestID)
	if err != nil {
		return store.Proposal{}, err
	}
	if prop.Op != store.ProposeState && latest != prop.Base {
		return store.Proposal{}, fmt.Errorf("%w: it was proposed against version %d, and version %d is saved now", ErrProposalStale, prop.Base, latest)
	}
	why := prop.Reason
	if reason != "" {
		why += "; " + reason
	}
	why += " (proposed by " + prop.Agent + ")"
	version := 0
	switch prop.Op {
	case store.ProposeSave:
		var v Version
		if prop.Kind == "Project" {
			v, err = e.CommitProject(ctx, prop.ManifestID, prop.Text, actor, why)
		} else {
			v, err = e.Commit(ctx, prop.Kind, prop.ManifestID, prop.Text, actor, why)
		}
		version = v.Number
	case store.ProposeAppend:
		var item map[string]any
		if err = json.Unmarshal(prop.Item, &item); err == nil {
			var v Version
			v, err = e.AppendSeriesItem(ctx, prop.Kind, prop.ManifestID, prop.Series, item, actor, why)
			version = v.Number
		}
	case store.ProposeState:
		if prop.State == ProjectStateHandedOff {
			if handoff == nil {
				return store.Proposal{}, fmt.Errorf("%w: a handoff is accepted where charters are rendered", ErrConflict)
			}
			version, err = handoff(ctx, prop.ManifestID, why)
		} else {
			_, err = e.TransitionProjectState(ctx, prop.ManifestID, prop.State, actor, why)
		}
	default:
		err = fmt.Errorf("a proposal to %q is not one this release accepts", prop.Op)
	}
	if err != nil {
		return store.Proposal{}, err
	}
	ps, _ := e.proposalStore()
	return ps.DecideProposal(ctx, id, store.ProposalAccepted, actor, reason, timeNow().UTC(), version)
}

// DeclineProposal declines a proposal, as the person it was made for.
func (e *Engine) DeclineProposal(ctx context.Context, id, actor, reason string) (store.Proposal, error) {
	prop, err := e.decidable(ctx, id)
	if err != nil {
		return store.Proposal{}, err
	}
	ps, _ := e.proposalStore()
	if prop.Set != "" {
		decided, err := ps.DecideProposalSet(ctx, prop.Set, store.ProposalDeclined, actor, reason, timeNow().UTC(), nil)
		return memberOf(decided, id), err
	}
	return ps.DecideProposal(ctx, id, store.ProposalDeclined, actor, reason, timeNow().UTC(), 0)
}

// decidable returns an open proposal the person on ctx may decide.
func (e *Engine) decidable(ctx context.Context, id string) (store.Proposal, error) {
	who := identity.PrincipalFrom(ctx)
	if who.Agent != "" {
		return store.Proposal{}, identity.ErrAgentProposes
	}
	ps, err := e.proposalStore()
	if err != nil {
		return store.Proposal{}, err
	}
	prop, err := ps.GetProposal(ctx, id)
	if errors.Is(err, store.ErrNoProposal) {
		return store.Proposal{}, fmt.Errorf("%w: proposal %s", ErrNotFound, id)
	}
	if err != nil {
		return store.Proposal{}, err
	}
	if prop.Status != store.ProposalOpen {
		return store.Proposal{}, store.ErrProposalDecided
	}
	if prop.For != personKey(who) {
		return store.Proposal{}, ErrNotTheirs
	}
	return prop, nil
}

// ProposalPart is one manifest of a proposal as its person reviews it:
// what it would change against the manifest's latest version, field by
// field, and the checks on what it proposes.
type ProposalPart struct {
	Proposal store.Proposal
	Changes  []Change
	Checks   []Check
}

// ProposalView is a proposal and every part of it, in the order they
// would be saved: one part, or one per manifest of its set.
type ProposalView struct {
	Proposal store.Proposal
	Parts    []ProposalPart
}

// ViewProposal returns a proposal with what each part would change and
// its checks, the checks seeing the rest of its set. A proposal is read by
// whoever may read its manifest.
func (e *Engine) ViewProposal(ctx context.Context, id string) (ProposalView, error) {
	ps, err := e.proposalStore()
	if err != nil {
		return ProposalView{}, err
	}
	p, err := ps.GetProposal(ctx, id)
	if errors.Is(err, store.ErrNoProposal) {
		return ProposalView{}, fmt.Errorf("%w: proposal %s", ErrNotFound, id)
	}
	if err != nil {
		return ProposalView{}, err
	}
	members := []store.Proposal{p}
	if p.Set != "" {
		if members, err = ps.ListProposals(ctx, store.ProposalFilter{Set: p.Set}); err != nil {
			return ProposalView{}, err
		}
		sort.Slice(members, func(i, j int) bool { return members[i].SetIndex < members[j].SetIndex })
	}
	docs := map[string]map[string]any{}
	for _, m := range members {
		var doc map[string]any
		if m.Text != nil && e.codec.DecodeInto(m.Text, &doc) == nil {
			docs[m.Kind+"/"+m.ManifestID] = doc
		}
	}
	checkCtx := withProposed(ctx, docs)
	view := ProposalView{Proposal: p}
	for _, m := range members {
		part := ProposalPart{Proposal: m, Changes: []Change{}, Checks: []Check{}}
		if m.Op == store.ProposeSave {
			before := map[string]any{}
			if m.Base > 0 {
				v, err := e.GetVersion(ctx, m.Kind, m.ManifestID, m.Base)
				if err != nil {
					return ProposalView{}, err
				}
				if err := e.codec.DecodeInto(v.YAML, &before); err != nil {
					return ProposalView{}, err
				}
			}
			part.Changes = diffValues("", before, docs[m.Kind+"/"+m.ManifestID])
			if part.Checks, err = e.ChecksOf(checkCtx, m.Kind, m.ManifestID, m.Text); err != nil {
				return ProposalView{}, err
			}
		}
		view.Parts = append(view.Parts, part)
	}
	return view, nil
}

// acceptSet saves every manifest of a proposal set as the person on ctx,
// in one transaction, in the order their references need: all of them or
// none. Each must still be open, theirs, and proposed against the version
// saved now.
func (e *Engine) acceptSet(ctx context.Context, prop store.Proposal, actor, reason string) (store.Proposal, error) {
	ps, _ := e.proposalStore()
	members, err := ps.ListProposals(ctx, store.ProposalFilter{Set: prop.Set})
	if err != nil {
		return store.Proposal{}, err
	}
	who := personKey(identity.PrincipalFrom(ctx))
	toSave := make([]SetMember, 0, len(members))
	byKey := map[string]store.Proposal{}
	for _, m := range members {
		if m.Status != store.ProposalOpen {
			return store.Proposal{}, store.ErrProposalDecided
		}
		if m.For != who {
			return store.Proposal{}, ErrNotTheirs
		}
		latest, err := latestNumber(ctx, e.manifests, m.Kind, m.ManifestID)
		if err != nil {
			return store.Proposal{}, err
		}
		if latest != m.Base {
			return store.Proposal{}, fmt.Errorf("%w: %s/%s was proposed against version %d, and version %d is saved now", ErrProposalStale, m.Kind, m.ManifestID, m.Base, latest)
		}
		toSave = append(toSave, SetMember{Kind: m.Kind, ID: m.ManifestID, Text: m.Text})
		byKey[m.Kind+"/"+m.ManifestID] = m
	}
	ordered, docs, err := e.checkMembers(ctx, toSave)
	if err != nil {
		return store.Proposal{}, err
	}
	if p, err := e.checkActor(ctx, actor); err != nil {
		return store.Proposal{}, err
	} else if p != nil {
		return store.Proposal{}, &ValidationError{Problems: []Problem{*p}}
	}
	var saved []Version
	versions := map[string]int{}
	err = e.manifests.WithinTransaction(ctx, func(ctx context.Context, tx store.ManifestStore) error {
		for _, m := range ordered {
			key := m.Kind + "/" + m.ID
			why := byKey[key].Reason
			if reason != "" {
				why += "; " + reason
			}
			why += " (proposed by " + byKey[key].Agent + ")"
			latest, err := latestNumber(ctx, tx, m.Kind, m.ID)
			if err != nil {
				return err
			}
			v := Version{Kind: m.Kind, ID: m.ID, Number: latest + 1, YAML: m.Text, Actor: actor, Reason: why, On: timeNow().UTC(), Doc: e.storedDoc(m.Kind, docs[key])}
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
			versions[byKey[key].ID] = v.Number
		}
		return nil
	})
	if err != nil {
		return store.Proposal{}, err
	}
	for _, v := range saved {
		e.afterVersion(ctx, v)
	}
	decided, err := ps.DecideProposalSet(ctx, prop.Set, store.ProposalAccepted, actor, reason, timeNow().UTC(), versions)
	return memberOf(decided, prop.ID), err
}

// memberOf is the proposal id among a decided set.
func memberOf(decided []store.Proposal, id string) store.Proposal {
	for _, p := range decided {
		if p.ID == id {
			return p
		}
	}
	return store.Proposal{}
}
