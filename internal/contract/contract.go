// Package contract embeds the JSON Schema files from cartograph/contract/schemas
// so the compiled binary is self-contained. This is a generated copy: `make
// generate` refreshes it from the source of truth at cartograph/contract/schemas,
// and `make ci` fails on drift (see the repository root Makefile). Go's
// embed directive cannot reach outside its module, and contract/ sits
// beside the server module rather than inside it, so a synced copy is the
// simplest way to keep the schemas both single-sourced and embeddable.
package contract

import "embed"

//go:embed schemas/*.json
var Schemas embed.FS

// Flows embeds cartograph/contract/flows the same way: the stepped definition
// of each kind that has one, which every interface renders from.
//
//go:embed flows/*.json
var Flows embed.FS
