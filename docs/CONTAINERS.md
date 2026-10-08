# Working in containers

Everything that builds, tests or serves Cartograph on your machine runs
in the test environment: a container with Nix and nothing else, where
every tool comes from the repository's flake. A person and an agent work
in the same environment as CI (which runs the same flake on its runner),
the host keeps nothing but a container runtime, and one run is never
touched by another or by the code changing beside it.

The environment is for testing. It never makes a change: changes are
made, described and pushed on the host, with `jj` from the flake.

## What you need

- Docker, or Podman with its compose (set `CARTOGRAPH_RUNTIME=podman`
  when both are installed).
- Nix on the host, for `jj` and the recipes that make changes
  (`docs/SETUP.md`).
- Both repositories side by side in one directory, the workspace:

      workspace/
        cartograph-engine/
        cartograph-ui/

## Run anything

`scripts/dev` runs a command in the environment, inside the flake of the
repository you are in:

    scripts/dev just test           # the gate
    scripts/dev just ci             # everything CI runs
    scripts/dev just serve          # a copy of the example on :8080
    scripts/dev just serve laya     # the same, with the decision model
    scripts/dev                     # a shell inside
    scripts/dev --down              # stop the environment

The interface has an environment of its own, `scripts/dev` in
`cartograph-ui`, on the same `cartograph-nix` volume: what either
fetches, both use. This one still runs the interface's recipes too,
when both repositories are in the workspace.

The first command builds the image and fetches the toolchain, a few
minutes; later ones start in about two seconds. The environment keeps
running between commands, so a server a command starts (`just serve`,
`just eval-serve`) keeps serving. Host networking means a browser on the
host reaches it at the address it prints.

## How it is built

| Part | What it is |
|---|---|
| `deploy/dev/Containerfile` | The image: `nixos/nix`, pinned by digest, with flakes on and Nix single-user. Nothing else is installed. |
| `compose.dev.yaml` | The one service, `dev`, and its volumes. |
| `scripts/dev` | Starts it, gives its volumes to your user once, syncs the workspace in, and runs the command in the nearest flake. |

| Mount | Holds |
|---|---|
| `/src` | The workspace, read-only. The environment cannot change it. |
| `/workspace` (volume `cartograph-workspace`) | The copy each command runs in, synced from `/src` before it: builds, generated files and evaluation runs stay here. |
| `/nix` (volume `cartograph-nix`) | The Nix store, shared by every instance and every run. The toolchain, the decision model and every build are fetched or built once. |
| `/home/dev` (volume `cartograph-home`) | Caches the tools keep (Go's build cache). |
| `/tmp` | A tmpfs: the tests' temporary files. |

Commands run as your user, so the volumes are yours and nothing the
environment does needs root on the host. Nix runs without its build
sandbox inside, which needs privileges a container is not given; every
fixed-output fetch is still checked against its hash.

To start again from nothing: `scripts/dev --down`, then
`docker volume rm cartograph-workspace cartograph-home` (keep
`cartograph-nix` unless you want to fetch everything again).

## CI

CI does not use this environment. A GitHub runner is a clean machine
already, and Nix makes it the same environment as this one: every job
runs `nix develop --command just ...` on the runner, on both
architectures.

Its store is cached by store path, so a run fetches what an earlier one
built and builds only what changed. There are two caches, each behind
its own switch, so both may be on at once:

| Cache | Switch (repository variable) | Default |
|---|---|---|
| The Actions cache, through magic-nix-cache: no account, and pull requests from forks read it too | `CARTOGRAPH_CACHE_GHA` | `on`; `off` turns it off |
| Cachix, readable by any machine | `CARTOGRAPH_CACHIX`, a cache name, with the secret `CACHIX_AUTH_TOKEN` to push | off |

Cachix is not sent image tarballs: the images live in the registry.
Paths cache.nixos.org holds, which are most of the development shell,
come from there in either case.

Locally neither is needed: the `cartograph-nix` volume keeps every
path once fetched or built.

## Deploying

Deployment uses images the flake builds, never a Containerfile:
`just image` builds Cartograph's (`cartograph:local`), `just image-laya`
the decision model's, its model inside (`cartograph-laya:local`).
`compose.yaml` runs them; `docs/DEPLOYMENT.md` has the settings.

## For agents

An agent works the same way, and `AGENTS.md` makes it a rule: every
command that builds, tests or serves runs through `scripts/dev`, and an
agent makes changes only on the host, with `jj` from the flake. An agent
under evaluation is given the exact commands by `just eval-serve`
(`docs/EVALUATING.md`).
