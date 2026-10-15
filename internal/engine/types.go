// Package engine is the kind-agnostic manifest engine: it validates
// manifests against their schema, references and kind rules, stores
// immutable versions, computes diffs, and indexes cross-manifest references.
// Adding a kind never changes this package; it changes only
// contract/schemas and internal/kinds.
package engine

import (
	"strings"
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
	// Keyword is the JSON Schema keyword that failed, for a schema
	// problem: what tells a draft not finished from one built wrong.
	Keyword string `json:"-"`
	// inForm is set for a schema problem found inside one of a choice of
	// forms (oneOf, anyOf): a property missing there means the value has
	// the wrong form, not a field left to fill.
	inForm bool
}

// Incomplete says whether the problem is only a field not filled in yet,
// in a record otherwise well formed (docs/adr/0035): a property the schema
// requires and the record lacks, or one given empty (no text, no items)
// where the schema wants some. A version may be saved with it; a check
// says it, and blocks a handoff. The envelope (apiVersion, kind,
// metadata, spec) and a record's id and name are never left to fill.
func (p Problem) Incomplete() bool {
	if p.inForm || p.Path == "" || p.Path == "/metadata" || strings.HasPrefix(p.Path, "/metadata/") {
		return false
	}
	switch p.Keyword {
	case "required":
		return true
	case "minLength", "minItems":
		return strings.HasPrefix(p.Message, p.Keyword+": got 0,")
	}
	return false
}

// wellFormed is what of problems refuses a version: everything but the
// fields not filled in yet.
func wellFormed(problems []Problem) []Problem {
	var out []Problem
	for _, p := range problems {
		if !p.Incomplete() {
			out = append(out, p)
		}
	}
	return out
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
	// Summary is the kind in one sentence, from its guidance.
	Summary string `json:"summary,omitempty"`
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
	// Uses is every incoming reference with where it sits in the manifest
	// that makes it, and what that place means for the target: a
	// governance body or a role confirms a success criterion, receives an
	// escalation or decided a mandate (TAXONOMY.md D43).
	Uses []Use `json:"uses,omitempty"`
}

// Use is one place another manifest references this one.
type Use struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
	// As is what the place means: confirms, receives or decided; empty
	// for any other.
	As string `json:"as,omitempty"`
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
