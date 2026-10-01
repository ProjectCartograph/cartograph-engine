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
| The gate, after every edit | `just test` (under ten seconds) |
| Everything CI runs, in CI's order | `just ci` |
| Regenerate from the contract | `just generate` |
| Format, lint, dependency rule | `just fmt`, `just lint`, `just arch` |
| Serve a copy of the example | `just serve` |
| Release binaries for both architectures | `just release` |
| The container image, from the flake | `just image` |

## Rules that are not negotiable

- **Contract first.** `contract/openapi.yaml`, `contract/schemas/*.schema.json` and `contract/flows/*.flow.json` are the truth. Change them, `just generate`, commit the generated Go with the change. Never edit `internal/api/gen`, `internal/contract/schemas` or `internal/contract/flows` by hand.
- **Clean architecture, enforced.** Dependencies point inward: entities (the kinds, the contract) know nothing of the engine; the engine knows ports, never an adapter, never a driver, never a syntax; adapters know the ports; `cmd` is the only package that knows everything and the only place an adapter is chosen, from configuration. `internal/arch` is the test; `docs/ARCHITECTURE.md` section 3 is the picture. A new capability that needs a file path, SQL, an HTTP header, a browser or a syntax is a new port plus an adapter.
- **An adapter passes its conformance suite or it is not done.** `internal/store/conformance`, `internal/codec/conformance`, `internal/crdt/conformance`, `internal/fanout/conformance`, `pkg/uiconformance`.
- **Shared drafts are Automerge documents** (`docs/adr/0007`). `crdt/` is the Rust crate behind `internal/crdt/automerge/automerge.wasm`, rebuilt by `just generate`, never edited by hand.
- **Stateless by design.** Nothing in the engine survives a request except through a port. A change that keeps state in the process (a cache is fine; a fact is not) is wrong; `docs/SERVERLESS.md` says what holds state today and which adapter replaces it.
- **Style is Google's, enforced.** `STYLE.md`: the Go style guide, gofmt, vet, staticcheck; commit messages as Google CL descriptions (imperative summary, blank line, a body that says what and why, sign-off), checked by `just commit-check`. No Conventional Commits prefixes.
- **Upstream is the product and nothing else.** No hand-off notes, session logs, task cards, plans, transcripts, screenshots, scratch files or editor and agent state are ever committed (`just clean-tree` fails the build on them). What you did and what you ran goes in the pull request description.
- **No person appears in Cartograph.** Definitions name roles. There is no Person kind; do not add one.
- **Ids are generated, never derived from names.** Names are unique under their parent, not globally.
- **A check never blocks a save.** Only a version save validates; a handoff refuses while a blocking check stands.
- **No organisation's words anywhere here.** `just words` fails the build on them, and on em dashes in what a person reads. The example is a fictional produce cooperative; keep it so.
- **Before adding a kind, a field that links two kinds, or a rule that refuses a link, read `docs/TAXONOMY.md`.** If the discipline has a word for it, use that word with that meaning; if Cartograph must depart, add a decision there first.
- **Tests under ten seconds.** Browser flows and container runs are not in the gate.
- **Never run `git` or `jj` write commands** (commit, push, rebase) unless the person asks for that in so many words.

## Where things are

- `docs/ARCHITECTURE.md`: the system, the hexagon, the flows, the extension points, the clean-architecture mapping. Read sections 3 and 7 before any structural change.
- `docs/DEPLOYMENT.md`: configuration (`CARTOGRAPH_*`), the image, identity, backups. `docs/SERVERLESS.md`: stateless operation.
- `docs/EXTENDING.md`: adding a kind, a store, a codec, an authenticator, a fan-out, a CRDT engine.
- `docs/UI_CONTRACT.md`, `docs/MULTIPLAYER.md`: what interfaces are built from, and shared editing.
- `docs/DESIGN_RULES.md`: how Cartograph behaves. `docs/TAXONOMY.md`: what the nouns mean.
- `STYLE.md`: code and commits. `CONTRIBUTING.md`: the loop.
- `pkg/`: the public surface other repositories import (`client`, `uiconformance`), under the module path `github.com/ProjectCartograph/cartograph-engine/v2`. Changing a signature there is a breaking change; say so.

## How to write

Code reads like the surrounding code: comments say why, in sentences,
no banners. Prose in docs and messages: short sentences, plain words,
standard terms, no em dashes. Say what you ran and what it printed;
never claim a check you did not run.
