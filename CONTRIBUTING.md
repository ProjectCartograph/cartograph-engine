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
4. To try an interface change against the engine before it is
   released, build it in `cartograph-ui` and `just ui
   ../cartograph-ui/dist`; every recipe then embeds that build until
   `just ui` puts the pinned release back. Never commit a change to
   `UI_VERSION` without the matching `UI_SHA256` (see `VERSIONING.md`).
5. `git config core.hooksPath .githooks` once, so every commit message
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
- A structural decision (a new port, a new ring, a change to how the
  binary is built or composed) gets an architecture decision record in
  `docs/adr/` in the same change.

## Commit messages

Every commit follows Google's Angular commit message format
([Angular: commit message guidelines](https://github.com/angular/angular/blob/main/contributing-docs/commit-message-guidelines.md)),
strictly. `just commit-check` checks the parts a script can check, in
CI on every pull request and in the commit-msg hook.

**One change per commit.** A commit does one thing, and the tree builds
and passes its tests after it. If the summary needs "and" to say what the
commit does, it is two commits. A bug found while doing something else
is its own commit, before or after, never folded in.

**The header** is `<type>(<scope>): <summary>`, 72 characters at most.

The type is one of Angular's:

- `build`: the build, the release, or an external dependency
- `ci`: the CI configuration and its scripts
- `docs`: documentation only
- `feat`: a new feature
- `fix`: a bug fix
- `perf`: a change that makes something faster
- `refactor`: a change that neither fixes a bug nor adds a feature
- `test`: a missing test added, or a test corrected

The scope names the area the change is in; a change across several
has none. In this repository:

- `adr`: docs/adr
- `api`: internal/api, the HTTP adapter
- `arch`: internal/arch, the dependency rule
- `auth`: internal/auth, the identity ports and their adapters
- `chart`: deploy/helm
- `cli`: cmd/cartograph
- `client`: pkg/client
- `codec`: internal/codec
- `commits`: the commit message check
- `compose`: compose*.yaml
- `config`: internal/config
- `contract`: contract/
- `contributing`: CONTRIBUTING.md, STYLE.md, AGENTS.md
- `crdt`: crdt/ and internal/crdt
- `deployment`: docs/DEPLOYMENT.md
- `deps`: go.mod and the flake's inputs
- `engine`: internal/engine
- `fanout`: internal/fanout
- `flake`: flake.nix
- `kinds`: internal/kinds
- `layout`: internal/layout, the graph layout port and its adapters
- `mcp`: internal/mcp, the MCP adapter for agents
- `oauth`: internal/oauth, the authorization server for agents
- `release`: VERSION and the pinned interface
- `render`: internal/render
- `spa`: internal/spa
- `store`: internal/store
- `syncserver`: internal/syncserver
- `ui`: fetching the interface release
- `vault`: internal/store/vault

The summary says what the commit does, in the imperative, present
tense ("drop the stale index", not "dropped" or "drops"), starting
lower-case and with no period at the end. It stands on its own:
someone skimming the history learns what changed without reading the
body.

**A blank line**, then **the body**, required for every type but
`docs`, for the reader who was not there:

- what the problem was, and why it mattered;
- why this approach, and what was considered and rejected;
- any shortcoming the change leaves, and what would remove it;
- what you ran and what it printed, where that is the evidence.

Write prose. Wrap at 72 characters. Name files, functions and
settings exactly. A link is fine, with enough said that the commit
still makes sense if the link stops working.

**The footer** says what a change breaks or deprecates, each in its own
paragraph after the body:

```
BREAKING CHANGE: <what breaks>

<what to do instead>
```

```
DEPRECATED: <what is deprecated>

<what to use instead>
```

A revert's header is `revert: ` and the reverted header, and its body
says `This reverts commit <SHA>.` and why.

**Trailers** go last, one per line: `Fixes #12` or `Refs #12` where an
issue exists, then `Signed-off-by:` (required: `git commit -s`, the
Developer Certificate of Origin), then any `Co-authored-by:`.

A good message:

```
fix(syncserver): close sync peers concurrently on shutdown

Shutdown closed each peer's WebSocket in turn, and each close waited up
to five seconds for the peer's reply, so a replica with a few hundred
peers spent its whole grace period closing them. The closes now run
together, and Shutdown waits at most one second; a peer that has not
answered by then is cut off and reconnects all the same.

TestShutdownTellsPeersToGoElsewhere: 5.01 s before, 0.01 s after.

Signed-off-by: A Contributor <contributor@example.org>
```

Not: "fix: bug", "fix: build", "chore: update files", "WIP", "Phase 1",
"Address review comments", or a summary that only names the files.

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
