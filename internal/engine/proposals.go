package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

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

// ProposeSave proposes committing text as the manifest's next version.
// It is checked as a save is, against the schema, the rules and the
// person's authority, so what the person accepts would save.
func (e *Engine) ProposeSave(ctx context.Context, kind, id string, text []byte, reason string) (store.Proposal, error) {
	if _, ok := kinds.ByName(kind); !ok {
		return store.Proposal{}, fmt.Errorf("%w: %s", ErrUnknownKind, kind)
	}
	doc, problems, err := e.validate(ctx, kind, text, nil)
	if err != nil {
		return store.Proposal{}, err
	}
	if metaID, ok := docID(doc); !ok || metaID != id {
		return store.Proposal{}, fmt.Errorf("%w: manifest metadata.id %q does not match %q", ErrConflict, metaID, id)
	}
	if len(problems) > 0 {
		return store.Proposal{}, &ValidationError{Problems: problems}
	}
	if err := e.guardDoc(ctx, kind, id, doc); err != nil {
		return store.Proposal{}, err
	}
	base, err := latestNumber(ctx, e.manifests, kind, id)
	if err != nil {
		return store.Proposal{}, err
	}
	return e.propose(ctx, store.Proposal{Kind: kind, ManifestID: id, Op: store.ProposeSave, Text: text, Base: base, Reason: reason})
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
	if _, err := e.decidable(ctx, id); err != nil {
		return store.Proposal{}, err
	}
	ps, _ := e.proposalStore()
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
