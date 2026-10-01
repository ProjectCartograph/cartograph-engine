# Contributing to cartograph-engine

Thank you for working on Cartograph. This page is the loop; `AGENTS.md`
is the short brief every contributor (human or agent) reads first;
`STYLE.md` is the code and commit style; the design records are under
`docs/`.

## Getting the toolchain

Nix, through the Determinate installer: `docs/SETUP.md` has the steps
for Linux, macOS and Windows (WSL2). Then every `just` recipe enters
the flake itself; `nix develop` or direnv with the `.envrc` saves
the second of start-up per recipe. The flake pins Go, staticcheck, just
and Chromium (Linux) for x86_64 and aarch64, Linux and macOS. There is
no other supported way to build: one environment is the point.

## The loop

1. `just test` after every edit. It is the gate and it must stay under
   ten seconds.
2. If you changed the contract (`contract/`), `just generate`, and
   commit what it produced with your change. CI fails on drift.
3. `just ci` before you open a pull request: it is exactly what CI
   runs, on both architectures.
4. `git config core.hooksPath .githooks` once, so every commit message
   is checked against `STYLE.md` as you write it.

## What a change looks like

- A new adapter lives in its own package under the port's directory,
  passes the port's conformance suite, is selected in `cmd/cartograph`
  from a configuration value, and respects the dependency rule
  (`just arch`). `docs/EXTENDING.md` walks through each port.
- A new kind is a schema file, one line in
  `internal/kinds/registry.go`, optional rules under
  `internal/kinds/<kind>/`, and an entry in `examples/minimal`. Read
  `docs/TAXONOMY.md` first.
- A new rule is a function from document to problems, with a JSON
  pointer path and a message a person can act on.
- A change to `pkg/` is a change to what other repositories import:
  say so in the summary line, and bump `VERSION` in the release.
- Nothing that keeps a fact in the process between requests: see
  `docs/SERVERLESS.md`.

## Pull requests

- One change per pull request, titled as its first commit's summary
  line. The description is the commit body: what it does for the
  person using Cartograph, why this way, what you ran and what it
  printed. A pull request that claims a check it did not run is sent
  back.
- Commits follow `STYLE.md` and are signed off (`git commit -s`): the
  Developer Certificate of Origin (developercertificate.org) is how you
  say the contribution is yours to give under the Apache License 2.0.
- Nothing but the product is committed: no notes, logs, plans, scratch
  files or editor and agent state. `just clean-tree` checks.

## Reporting a problem

Open an issue with: what you did, what you expected, what happened,
the version (`cartograph --version` or the commit), and a copy of the
manifest or the vault that shows it, with any organisation-specific
content removed. A problem you can reproduce on `examples/minimal` is
the easiest to fix. Security problems go through `SECURITY.md`.

## Licence

Apache License 2.0 (`LICENSE`). By contributing you agree your
contribution is licensed under it.
