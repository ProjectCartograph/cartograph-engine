# Setting up a machine

One toolchain, pinned by `flake.nix`, on every platform. Install Nix,
clone, run `just test`. This page is the per-platform part of that.
Nix is the only supported way to build; every `just` recipe enters the
flake itself, so nothing else is installed by hand.

Use the [Determinate Nix installer](https://determinate.systems/nix-installer/)
everywhere: one command, flakes on by default, a clean uninstall, the
same Nix on Linux, macOS and WSL.

## Linux (x86_64 or aarch64)

```
curl -fsSL https://install.determinate.systems/nix | sh -s -- install
```

Open a new shell, then:

```
git clone git@github.com:ProjectCartograph/cartograph-engine.git
cd cartograph-engine
git config core.hooksPath .githooks
just test
```

The first `just` evaluates the flake and fetches the pinned toolchain
(Go, staticcheck, just, Chromium, a Python for the compatibility check).
That takes a few minutes once and seconds after. `direnv` with
`direnv allow` is optional; it puts the shell inside the flake already,
so recipes skip the second of start-up.

Docker is needed only for `just image` (loading the flake-built image)
and `compose.yaml`; `just release` builds the binaries without it.

## macOS (Apple silicon)

```
curl -fsSL https://install.determinate.systems/nix | sh -s -- install
```

Open a new terminal, then the same clone and `just test` as on Linux.

Nixpkgs no longer builds for Intel Macs, so the flake has no shell for
them: work in the test environment instead (`scripts/dev`,
docs/CONTAINERS.md), which needs only Docker. The release still ships
an Intel macOS binary, cross-compiled on Linux (docs/CROSS.md).

Chromium is not packaged for macOS in nixpkgs, so the flake leaves it
out there. PDF printing and the browser flows then use whatever browser
you point `CHROMIUM` at:

```
export CHROMIUM="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
```

Everything else (the gate, `just ci`, `just release`) runs the same.
Container images are Linux root filesystems; build them on Linux or in
CI, or run `just image` inside a Linux VM.

On a machine you keep, [nix-darwin](https://github.com/nix-darwin/nix-darwin)
is optional and recommended. It manages the macOS system itself (shells,
services, the Nix daemon's settings) from a flake, the way NixOS does on
Linux. Determinate's installer and nix-darwin work together; follow
nix-darwin's README with the "Determinate" option. It is not required to
build Cartograph.

## Windows (WSL2)

Nix does not run on Windows itself; it runs in WSL2, which is a Linux
machine. Everything below happens inside it.

1. In PowerShell as administrator, run `wsl --install` (Ubuntu by
   default), restart, open Ubuntu from the Start menu and create your
   user.
2. Inside Ubuntu, follow the Linux steps above, including the
   Determinate installer.
3. Clone into the Linux filesystem (`~/git/...`), never under `/mnt/c`.
   File watching, permissions and speed are all wrong across that
   boundary.
4. VS Code with the "WSL" extension opens the WSL folder directly, and
   JetBrains IDEs can do the same.
5. For `just image`, use Docker Desktop with the WSL integration turned
   on for your distribution.

Git for Windows may convert line endings. Inside WSL, use the Linux Git
(`git` from Ubuntu) and leave `core.autocrlf` unset.

## Verify

```
just ci
```

Green here is green in CI, which runs the same flake on both
architectures.

## Without Nix

Not supported. The recipes refuse to run without it on purpose. A build
with whatever Go or Node was on the PATH is a different build, and the
flake exists so that there is only one. `CARTOGRAPH_TOOLCHAIN=1`
makes the recipes run with your own tools anyway, and you own the
result.
