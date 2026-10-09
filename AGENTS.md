# Working in cartograph-engine

Read this before touching anything. It is the whole brief; the
documents it names hold the detail.

## What this is

Cartograph captures what an organisation has decided to do (goals,
programmes, projects, operations, KPIs, data sources) as declarative
manifests, validates them against a contract and a set of discipline
rules, and renders documents. One Go binary, interface embedded. This
repository is the engine, the contract and the command line;
interfaces live in `cartograph-ui`.

## Commands

Every command is a `just` recipe, and every recipe runs inside the
flake (`nix develop`), so what you run is what CI runs on x86_64 and
aarch64. There is no Makefile and there will not be one. Nix is
required (`docs/SETUP.md`: Linux, macOS, Windows through WSL2).

| Do | Run |
|---|---|
| The gate, after every edit: unit tests with fake adapters (CI runs them as the binary's check phase, `just check`) | `just test` (a few seconds) |
| The real adapters (SQLite, the vault, Postgres, the CRDT, sockets), locally | `just test-integration` |
| Everything CI runs, in CI's order | `just ci` |
| Regenerate from the contract | `just generate` |
| Format, lint, dependency rule | `just fmt`, `just lint`, `just arch` |
| Serve a copy of the example | `just serve` (`just serve laya` with the decision model) |
| Release binaries for every platform, cross-compiled here | `just release` |
| The container image, from the flake | `just image` |
| How agents used the MCP server: defects, sigma, waste, variance | `cartograph traces [-agent name] <trace file>` |

## Rules that are not negotiable

- **Everything runs through the flake, agents included.** Every command is a `just` recipe or runs inside `nix develop` (or is a `nix build` or `nix run` of the flake): never a system binary, never a tool installed on the machine, never a Makefile (`just` is this repository's one task runner). This holds for an agent as much as a person, and for anything an agent starts: a server under evaluation is a `nix build` of a pinned commit, served from the Nix store, so a run is never touched by code changing beside it, and every environment is the same as every other.
- **Build, test and serve in the test environment; change on the host.** Locally, every command that builds, tests or serves runs in the container through `scripts/dev` (`scripts/dev just test`), which mounts the workspace read-only, runs in its own synced copy, and shares one Nix store volume across every instance (`docs/CONTAINERS.md`). The environment never makes a change: edits, `jj` and pushes happen on the host, through the flake. CI runs the same recipes on the runner itself, through the flake, with its store cached by path (`docs/CONTAINERS.md`, "CI").
- **Version control is jujutsu, colocated with git.** Every clone is a colocated `jj` repository (`jj git init --colocate` once in a plain git clone), and an agent makes every change with `jj` from the flake: `jj new -m` before a piece of work, `jj describe`, `jj bookmark set`, `jj git push`. Never `git commit`, `git push`, `git rebase` or `git checkout`. The read-only git queries a recipe shares with CI (whose checkouts are plain git) stay as they are.
- **Contract first.** `contract/openapi.yaml`, `contract/schemas/*.schema.json` and `contract/flows/*.flow.json` are the truth. Change them, `just generate`, commit the generated Go with the change. Never edit `internal/api/gen`, `internal/contract/schemas` or `internal/contract/flows` by hand.
- **Clean architecture, enforced.** Dependencies point inward: entities (the kinds, the contract) know nothing of the engine; the engine knows ports, never an adapter, never a driver, never a syntax; adapters know the ports; `cmd` is the only package that knows everything and the only place an adapter is chosen, from configuration. `internal/arch` is the test; `docs/ARCHITECTURE.md` section 3 is the picture. A new capability that needs a file path, SQL, an HTTP header, a browser or a syntax is a new port plus an adapter.
- **An adapter passes its conformance suite or it is not done.** `internal/store/conformance`, `internal/codec/conformance`, `internal/crdt/conformance`, `internal/fanout/conformance`, `pkg/uiconformance`.
- **Shared drafts are Automerge documents** (`docs/adr/0007`). `crdt/` is the Rust crate behind `internal/crdt/automerge/automerge.wasm`, rebuilt by `just generate` on x86_64 Linux (its bytes are defined there), never edited by hand.
- **Stateless by design.** Nothing in the engine survives a request except through a port. A change that keeps state in the process (a cache is fine; a fact is not) is wrong; `docs/SERVERLESS.md` says what holds state today and which adapter replaces it.
- **Style is Google's, enforced.** `STYLE.md`: the Go style guide, gofmt, vet, staticcheck; commit messages in Google's Angular format (`<type>(<scope>): <summary>`, blank line, a body that says what and why, sign-off; `CONTRIBUTING.md` has the types and scopes), checked by `just commit-check`.
- **Upstream is the product and nothing else.** No hand-off notes, session logs, task cards, plans, transcripts, screenshots, scratch files or editor and agent state are ever committed (`just clean-tree` fails the build on them). What you did and what you ran goes in the pull request description.
- **Meaning in text is a System-1 question, never a pattern** (`docs/adr/0030`). Whether a text is an aim or a target, names a person, or says which part of a charter it is, is a named question to the decision model through `internal/decide`, its wording kept only as it measures against examples whose answer is known (`internal/engine/testdata/decide`, `just decide-measure`). Its answer advises; only an exact rule (a titled name, a code, a date in digits) refuses. Without a model the question is not asked: its check is left out, and the decision model's status lists it as off. The gate tests the engine's use of answers with `internal/decide/fake`.
- **No person appears in Cartograph.** Definitions name roles. There is no Person kind; do not add one.
- **Ids are generated, never derived from names.** Names are unique under their parent, not globally.
- **A check never blocks a save.** Only a version save validates; a handoff refuses while a blocking check stands.
- **No organisation's words anywhere here.** `just words` fails the build on them, and on em dashes in what a person reads. The example is a fictional produce cooperative; keep it so.
- **Before adding a kind, a field that links two kinds, or a rule that refuses a link, read `docs/TAXONOMY.md`.** If the discipline has a word for it, use that word with that meaning; if Cartograph must depart, add a decision there first.
- **The gate is fakes only, and takes seconds.** `just test`, which CI runs as the binary's check phase (`just check`, so the tested binary is cached), tests through fake adapters (the hexagon's point: a port's in-memory adapter stands in for the real one) and must finish within five seconds on two cores. A real adapter's tests, and anything needing a model, a database, a browser or a socket, carry the `integration` build tag and run locally in `just test-integration`; Nix makes them the same on every machine, so they need not run in CI.
- **Never run `git` or `jj` write commands** (commit, push, rebase) unless the person asks for that in so many words.

## Changing what agents use

A change to anything an agent touches through MCP (a tool, an answer,
the instructions, the port, a rule the engine enforces) is run as Lean
Six Sigma's DMAIC loop, and `docs/EVALUATING.md` is the procedure:

- **Define** the criteria first, as a script scores them from the
  server: the change set, its records, the trace. Never from what the
  agent reports.
- **Measure** fresh runs, each on its own vault and trace, the agent
  given a minimal prompt; the evaluator's own calls left out of the
  analysis (`cartograph traces -agent`).
- **Analyse** the trace as a value stream: defects, waste (a record
  read again unchanged, a write undone, a call repeated after a
  refusal), and variance across runs and models.
- **Improve** the cause in the engine, deterministically, with a test
  that fails without it; a line in the instructions is the weakest fix.
- **Control** on one frozen build: the counted runs one at a time, the
  bar (three passing in a row by default) met before the feature is
  closed, and the figures in the pull request.

Run one agent at a time. Several at once only with the person's leave.

## Where things are

- `docs/ARCHITECTURE.md`: the system, the hexagon, the flows, the extension points, the clean-architecture mapping. Read sections 3 and 7 before any structural change.
- `docs/DEPLOYMENT.md`: configuration (`CARTOGRAPH_*`), the image, identity, backups. `docs/SERVERLESS.md`: stateless operation.
- `docs/EXTENDING.md`: adding a kind, a store, a codec, an authenticator, a fan-out, a CRDT engine.
- `docs/DISTRIBUTIONS.md`: shipping a distribution, worked through with a Laravel interface and Entra ID.
- `docs/UI_CONTRACT.md`, `docs/MULTIPLAYER.md`: what interfaces are built from, and shared editing.
- `docs/DESIGN_RULES.md`: how Cartograph behaves. `docs/TAXONOMY.md`: what the nouns mean.
- `docs/EVALUATING.md`: judging a change agents use, by DMAIC, from the server and the trace.
- `docs/CONTAINERS.md`: the test environment every command builds, tests and serves in.
- `docs/CROSS.md`: every platform's binary and image, built on one Linux machine.
- `STYLE.md`: code and commits. `CONTRIBUTING.md`: the loop.
- `pkg/`: the public surface other repositories import (`client`, `uiconformance`), under the module path `github.com/ProjectCartograph/cartograph-engine/v2`. Changing a signature there is a breaking change; say so.

## How to write

Code reads like the surrounding code: comments say why, in sentences,
no banners. Prose in docs and messages: short sentences, plain words,
standard terms, no em dashes. Say what you ran and what it printed;
never claim a check you did not run.
