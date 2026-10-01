// Package engine is the kind-agnostic manifest engine: it validates
// manifests against their schema, references and kind rules, stores
// immutable versions, computes diffs, and indexes cross-manifest references.
// Adding a kind never changes this package; it changes only
// contract/schemas and internal/kinds.
package engine

import (
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

// Version and Summary are exactly the records store.ManifestStore
// and store.OperationalStore persist; the engine re-exports them by alias
// so callers only need one set of names, and so store adapters (which must
// be usable without importing the engine) stay the source of truth for
// their own shapes.
type (
	Version = store.Version
	Summary = store.Summary
)

// Problem is one validation failure. Path is a JSON pointer into the
// manifest.
type Problem struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// Change is one difference between two versions of a manifest, at Path (a
// JSON pointer, or a pointer using an array element's own id in place of
// its index for arrays of objects that carry one, see diff.go).
type Change struct {
	Path string `json:"path"`
	Op   string `json:"op"` // add, remove, replace
	From any    `json:"from,omitempty"`
	To   any    `json:"to,omitempty"`
}

// KindInfo is one row of GET /api/v1/kinds.
type KindInfo struct {
	Kind  string `json:"kind"`
	Count int    `json:"count"`
}

// Ref is one reference between manifests. Used both as a filter (only Kind
// and ID are read) and as an outgoing reference (Path populated too).
type Ref struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Path string `json:"path,omitempty"`
}

// Filter narrows List: Q is a case-insensitive substring over id and name;
// every entry in Refs must match one of the manifest's outgoing references
// (logical AND).
type Filter struct {
	Q    string
	Refs []Ref
}

// Refs is the result of References: what a manifest points to, and what
// points back at it.
type Refs struct {
	Outgoing []Ref     `json:"outgoing"`
	Incoming []Summary `json:"incoming"`
}

// Report is the result of ImportDir. When any file has problems, nothing in
// the batch is written: Imported is empty and Problems lists what to fix.
type Report struct {
	Imported []ImportedItem `json:"imported"`
	Problems []FileProblems `json:"problems"`
	Notes    []FileProblems `json:"notes,omitempty"` // one per file that needed a legacy-field rewrite (see rewriteLegacyFields)
}

type ImportedItem struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Version int    `json:"version"`
}

type FileProblems struct {
	File     string    `json:"file"`
	Problems []Problem `json:"problems"`
}

// timeNow is a seam for tests; production code always uses time.Now.
var timeNow = time.Now
