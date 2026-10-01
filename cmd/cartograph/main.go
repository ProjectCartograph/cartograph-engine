// Command cartograph is the one binary: serve, validate, import, export, diff,
// list. Every subcommand except validate opens the same kind of store (a
// SQLite file) and drives it through the same engine the API and the SPA
// use; nothing here duplicates the engine's rules.
package main

import (
	"fmt"
	"os"
)

// version is set at build time (-ldflags "-X main.version=..."): the
// justfile and the flake both pass VERSION. A plain `go build` says dev.
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	var err error
	switch os.Args[1] {
	case "serve":
		err = runServe(os.Args[2:])
	case "validate":
		err = runValidate(os.Args[2:])
	case "import":
		err = runImport(os.Args[2:])
	case "export":
		err = runExport(os.Args[2:])
	case "diff":
		err = runDiff(os.Args[2:])
	case "list":
		err = runList(os.Args[2:])
	case "snapshot":
		err = runSnapshot(os.Args[2:])
	case "check":
		err = runCheck(os.Args[2:])
	case "handoff":
		err = runHandoff(os.Args[2:])
	case "render":
		err = runRender(os.Args[2:])
	case "apply":
		err = runApply(os.Args[2:])
	case "exclude":
		err = runExclude(os.Args[2:])
	case "recover":
		err = runRecover(os.Args[2:])
	case "unapplied":
		err = runUnapplied(os.Args[2:])
	case "excluded":
		err = runExcluded(os.Args[2:])
	case "ready":
		err = runReady(os.Args[2:])
	case "-v", "--version", "version":
		fmt.Println("cartograph", version)
		return
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "cartograph: unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "cartograph:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `cartograph: a declarative data capture engine

Every command's output is a function of the vault's files and the snapshot log only.

Commands: serve, ready, validate, check, list, snapshot, render, handoff, export, diff, import, apply, exclude, recover, unapplied, excluded

Usage:
  cartograph serve [<vault-dir>] [-addr :8080] [-import <dir>]   (every flag also reads CARTOGRAPH_*; see serve -h)
  cartograph ready [<addr or url>]                                (exit 0 when /readyz answers; for health checks)
  cartograph validate <dir|file>
  cartograph check <dir|file> [<project id>]
  cartograph list <kind> [--q text] [--ref Kind/id ...] -db cartograph.db
  cartograph snapshot <kind> <id> -db cartograph.db --reason "..."
  cartograph render charter <project id> -db cartograph.db [--format html|pdf|json] [--out <dir>]
  cartograph handoff <project id> -db cartograph.db --reason "..."
  cartograph export <out-dir> [<vault-or-db>] [--codec yaml|json]
  cartograph diff <kind> <id> --from N --to M -db cartograph.db
  cartograph import <dir> -db cartograph.db --actor <person id> --reason "..."
  cartograph apply <Kind/id> <vault-dir>
  cartograph exclude <Kind/id> <vault-dir> --reason "..."
  cartograph recover <Kind/id> <vault-dir> --reason "..."
  cartograph unapplied <vault-dir>
  cartograph excluded <vault-dir>
  cartograph version
`)
}
