package store

import (
	"context"
	"errors"
	"time"
)

// Proposal is something an agent proposed for its person to confirm
// (docs/adr/0016): what would make the record, kept exactly as proposed,
// with the version it was proposed against.
type Proposal struct {
	ID         string
	Kind       string
	ManifestID string
	// Op is ProposeSave, ProposeAppend or ProposeState.
	Op string
	// Text is the manifest to commit, for ProposeSave.
	Text []byte
	// Series and Item are the series and the item to record, as JSON,
	// for ProposeAppend.
	Series string
	Item   []byte
	// State is where to move the project, for ProposeState.
	State string
	// Base is the manifest's latest version when it was proposed.
	Base   int
	Reason string
	// Waivers are the checks the agent left open, each with its reason,
	// for its person to see before deciding.
	Waivers []Waiver
	// Set groups proposals that stand or fall together (manifests that
	// reference each other, such as a KPI, the gap it measures and the
	// outcome that closes it); SetIndex is the order they are saved in.
	// "" for a proposal on its own.
	Set      string
	SetIndex int
	// Agent is the agent that proposed it; For is the person it acts for,
	// who alone may decide it (their address, or "" for an anonymous
	// operator).
	Agent string
	For   string
	At    time.Time
	// Status is ProposalOpen, ProposalAccepted or ProposalDeclined.
	Status         string
	DecidedBy      string
	DecidedAt      time.Time
	DecisionReason string
	// Version is the version accepting it made, when it made one.
	Version int
}

// Waiver is a check an agent proposed without meeting: the check, what
// it said, and why the agent left it.
type Waiver struct {
	// On is the manifest it is on, as Kind/id, in a change set; empty on
	// a proposal, which is on one manifest.
	On      string `json:"on,omitempty"`
	Check   string `json:"check"`
	Message string `json:"message"`
	Reason  string `json:"reason"`
}

// What a proposal does.
const (
	ProposeSave   = "save"
	ProposeAppend = "append"
	ProposeState  = "state"
)

// Where a proposal stands.
const (
	ProposalOpen     = "open"
	ProposalAccepted = "accepted"
	ProposalDeclined = "declined"
)

// ProposalFilter narrows a list. An empty field matches anything; Status
// "" is every status.
type ProposalFilter struct {
	For, Kind, ManifestID, Status, Set string
}

var (
	// ErrNoProposal is a proposal that does not exist.
	ErrNoProposal = errors.New("no such proposal")
	// ErrProposalDecided is a proposal decided already.
	ErrProposalDecided = errors.New("this proposal has been decided already")
)

// ProposalStore keeps proposals. Deciding one is atomic: of two
// decisions at once, one wins and the other is ErrProposalDecided.
type ProposalStore interface {
	PutProposal(ctx context.Context, p Proposal) error
	// GetProposal returns one, or ErrNoProposal.
	GetProposal(ctx context.Context, id string) (Proposal, error)
	// ListProposals returns those the filter matches, newest first.
	ListProposals(ctx context.Context, f ProposalFilter) ([]Proposal, error)
	// DecideProposal moves an open proposal to status, recording who,
	// when, why and the version made; ErrProposalDecided when it is not
	// open, ErrNoProposal when there is none.
	DecideProposal(ctx context.Context, id, status, by, reason string, at time.Time, version int) (Proposal, error)
	// DecideProposalSet decides every proposal of a set at once, or none:
	// ErrProposalDecided when any is not open, ErrNoProposal when the set
	// has none. versions gives the version each made, by proposal id.
	DecideProposalSet(ctx context.Context, set, status, by, reason string, at time.Time, versions map[string]int) ([]Proposal, error)
}
