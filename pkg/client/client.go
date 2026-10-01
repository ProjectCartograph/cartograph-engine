// Package client is the port an interface uses to work with Cartograph: the
// web interface, the terminal interface, a bot, a test. It is a Go
// interface, not a wire protocol. In-process it is the engine itself;
// over a network it is whatever transport the deployment runs (HTTP and
// Server-Sent Events over TCP or a UNIX socket today), and the
// interface never knows which, which is what lets a terminal interface
// run in the same binary as the engine with no server in between, or
// across an SSH-forwarded socket, with the same code.
//
// Everything an interface shows comes through this port: the schemas and
// flows it renders from, the manifests it reads and saves, the checks and
// problems it marks, the events it listens to. Nothing in an interface
// may reach for an HTTP path.
//
// Live editing is not on this port. A manifest's shared draft is an
// Automerge document (docs/adr/0007): SharedDocument names it, and the
// interface syncs it with the automerge-repo protocol, the same way the
// web interface does.
package client

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound is returned by Get and Working for a manifest that is not
// there (or has no working copy).
var ErrNotFound = errors.New("not found")

// ErrUnsupported is returned by a transport for an operation it does
// not carry yet.
var ErrUnsupported = errors.New("not supported by this transport")

// Problem is one refusal or check at a field, the same shape the engine
// produces everywhere.
type Problem struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// Refused is the error a validating write returns when the engine said
// no: the problems, landed on their fields. It is a normal outcome an
// interface shows, not a failure.
type Refused struct{ Problems []Problem }

func (r *Refused) Error() string { return "refused" }

// Version is one saved version of a manifest.
type Version struct {
	Kind   string    `json:"kind"`
	ID     string    `json:"id"`
	Number int       `json:"number"`
	Actor  string    `json:"actor"`
	Reason string    `json:"reason"`
	On     time.Time `json:"on"`
}

// Manifest is a decoded manifest with the text it came from.
type Manifest struct {
	Kind    string
	ID      string
	Version int // 0 for a working copy
	Doc     map[string]any
	Text    []byte
}

// Summary is one row of a listing.
type Summary struct {
	Kind      string
	ID        string
	Name      string
	Version   int
	UpdatedOn time.Time
	Labels    map[string]string
}

// Check is one line of a manifest's checks: advice, a warning, or a
// block, at a field, with the flow step that fixes it.
type Check struct {
	Key     string `json:"key"`
	State   string `json:"state"` // ok, warn, block, note
	Path    string `json:"path"`
	Message string `json:"message"`
	Fix     string `json:"fix,omitempty"` // flow step key
}

// Flow is a kind's flow document as the contract defines it
// (contract/flows/flow.schema.json), decoded.
type Flow = map[string]any

// Settings are the organisation's words and defaults.
type Settings struct {
	GoalLevels       []string
	ProjectLevelName string
	Operator         string
	Examples         map[string][]string
}

// Event is what Subscribe delivers: a version saved, a state changed.
// Seq is the version number for a version event.
type Event struct {
	Type  string
	Kind  string
	ID    string
	On    time.Time
	Seq   int64
	Actor string
	Field string
}

// Client is the whole port. A transport implements all of it or returns
// ErrUnsupported for what it cannot carry yet; the conformance suite in
// pkg/uiconformance runs over any Client, which is how transports are
// proven interchangeable.
type Client interface {
	// Contract: what an interface renders from.
	Kinds(ctx context.Context) ([]string, error)
	Schema(ctx context.Context, kind string) ([]byte, error)
	Flow(ctx context.Context, kind string) (Flow, bool, error)
	Settings(ctx context.Context) (Settings, error)

	// Manifests.
	List(ctx context.Context, kind, query string) ([]Summary, error)
	Get(ctx context.Context, kind, id string) (Manifest, error)
	Working(ctx context.Context, kind, id string) (Manifest, error)
	SaveWorking(ctx context.Context, kind, id string, doc map[string]any) error
	SaveVersion(ctx context.Context, kind, id string, doc map[string]any, reason string) (Version, error)
	Validate(ctx context.Context, kind string, doc map[string]any) ([]Problem, error)
	Checks(ctx context.Context, kind, id string) ([]Check, error)

	// The shared draft: the id of the Automerge document a manifest's
	// draft lives in, created on first use, for an interface to sync.
	SharedDocument(ctx context.Context, kind, id string) (string, error)
	Subscribe(ctx context.Context, kind, id string) (<-chan Event, error)

	// Identity: who this client acts as, as the engine will record it.
	Actor(ctx context.Context) (string, error)

	Close() error
}
