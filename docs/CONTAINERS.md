# Working in containers

Everything that builds, tests or serves Cartograph runs in the test
environment: a container with Nix and nothing else, where every tool
comes from the repository's flake. A person, CI and an agent work in the
same environment, the host keeps nothing but a container runtime, and
one run is never touched by another or by the code changing beside it.

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

## In CI

CI runs the same environment: the `ci` and `commits` jobs are
`scripts/dev just ci` and `scripts/dev just commit-check`, on both
architectures. There, `CARTOGRAPH_NIX_STORE` names a directory that is
`/nix` instead of the volume, seeded from the image's own on first use,
and the Actions cache saves and restores it; a run whose flake changed
roots the development shell and collects the rest before it is saved.
From an empty store the gate takes under three minutes, nearly all of
it fetching the toolchain. The jobs that build and start the image, and
the Helm chart on kind, need the runner's own Docker and stay on it.

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
