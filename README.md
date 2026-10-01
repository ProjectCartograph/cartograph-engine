<p align="center"><img src="docs/logo.svg" width="128" alt="Cartograph"></p>

# Cartograph

[![ci](https://github.com/ProjectCartograph/cartograph-engine/actions/workflows/ci.yml/badge.svg)](https://github.com/ProjectCartograph/cartograph-engine/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/ProjectCartograph/cartograph-engine.svg)](https://pkg.go.dev/github.com/ProjectCartograph/cartograph-engine)
[![Go version](https://img.shields.io/github/go-mod/go-version/ProjectCartograph/cartograph-engine)](go.mod)
[![Licence](https://img.shields.io/badge/licence-Apache--2.0-blue.svg)](LICENSE)
[![Release](https://img.shields.io/github/v/release/ProjectCartograph/cartograph-engine?include_prereleases)](https://github.com/ProjectCartograph/cartograph-engine/releases)

A declarative capture engine for what an organisation has decided to
do: goals, programmes, projects, operations, KPIs and the data sources
behind them, as manifests in a directory, validated against a contract,
rendered as documents. One Go binary with its interface embedded.
Apache License 2.0.

This repository is the engine: the contract, the Go core, the command
line, the public packages other repositories build on. The web and
terminal interfaces are in [cartograph-ui](https://github.com/ProjectCartograph/cartograph-ui).

## Run it

```
just serve        # a copy of examples/minimal on 127.0.0.1:8080
```

Open http://localhost:8080. `cartograph serve -h` lists every setting
(`CARTOGRAPH_ADDR`, `CARTOGRAPH_VAULT`, `CARTOGRAPH_CODEC`, `CARTOGRAPH_AUTH`, ...);
`docs/DEPLOYMENT.md` explains them, the container image and identity.

Or, with nothing checked out: `nix run github:ProjectCartograph/cartograph-engine -- serve ./my-vault`.

## Build and test it

Nix is the toolchain (`docs/SETUP.md`: Linux, macOS, Windows through
WSL2): install it once and every `just` recipe enters the flake itself,
on x86_64 or aarch64.

```
just test     # the gate, under ten seconds
just ci       # everything CI runs, in CI's order
just release  # static binaries for linux/amd64 and linux/arm64
just image    # the container image, from the flake, loaded into docker
```

The binary embeds the web interface. Every recipe that compiles fetches
the `cartograph-ui` release named in `UI_VERSION` first and refuses it
unless it matches `UI_SHA256`; `just ui ../cartograph-ui/dist` embeds a
local interface build instead, and `just ui` puts the release back.

## Layout

```
contract/        openapi.yaml, one JSON Schema per kind, one flow per stepped kind: the truth
cmd/cartograph/       the binary: serve, ready, validate, import, export, diff, list, snapshot,
                 check, render, handoff, apply, exclude, recover
internal/engine/ the core: validate, commit, version, diff, check, hand off, shared drafts
internal/kinds/  the registry of kinds and each kind's rules
internal/store/  ports and adapters: vault (files), sqlite, memory
internal/codec/  manifest syntax: yaml, json
internal/auth/   identity and policy: none, proxy; allow, roles
internal/printer/ PDF: chromium, none
internal/api/    the HTTP driving adapter; gen/ is generated from the contract
pkg/client/      the port every interface uses, in-process and remote
pkg/merge/       the CRDTs under shared editing
pkg/uiconformance/ the suite every interface passes
examples/minimal a fictional produce cooperative
internal/spa/    the embedded web interface: a cartograph-ui release, fetched, never committed
docs/            SETUP, ARCHITECTURE, DEPLOYMENT, SERVERLESS, EXTENDING, UI_CONTRACT, MULTIPLAYER, DESIGN_RULES, TAXONOMY
docs/adr/        architecture decision records, one decision per file
UI_VERSION       the cartograph-ui release the binary embeds; UI_SHA256 pins its bytes
justfile         every command; flake.nix pins what they run with
```

## Read next

`docs/ARCHITECTURE.md` for the system in diagrams, the extension
points and the clean-architecture mapping; `docs/adr/` for the
decisions behind it; `docs/DEPLOYMENT.md` and
`docs/SERVERLESS.md` to run it; `docs/EXTENDING.md` to add a kind, a
store, a codec, an authenticator or a transport; `CONTRIBUTING.md` for
the loop; `STYLE.md` for code and commits; `VERSIONING.md` for what a
release promises; `AGENTS.md` for the short
brief; `SECURITY.md` to report a vulnerability; `CODE_OF_CONDUCT.md`
for how we treat each other.
