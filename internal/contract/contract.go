// Package contract embeds the JSON Schema files from contract/schemas so
// the compiled binary is self-contained. This is a generated copy: `just
// generate` refreshes it from the source of truth at contract/schemas,
// and `just ci` fails on drift. Go's embed directive cannot reach above
// the package's own directory, so a synced copy is the simplest way to
// keep the schemas both single-sourced and embeddable.
package contract

import "embed"

//go:embed schemas/*.json
var Schemas embed.FS

// Flows embeds contract/flows the same way: the stepped definition
// of each kind that has one, which every interface renders from.
//
//go:embed flows/*.json
var Flows embed.FS

// Guidance embeds contract/guidance the same way: how to define each kind
// well, in each language, which people and agents read.
//
//go:embed guidance
var Guidance embed.FS
