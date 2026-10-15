# Building for every platform

Every platform Cartograph ships for is built on one Linux machine, by
the flake. Nothing is built on the platform it is for; each platform
only runs what was built for it, to prove it works.

## The binary

The binary is pure Go: SQLite, Postgres and the Automerge runtime need
no C compiler (`CGO_ENABLED=0`). So Go builds it for any platform from
any other, and the flake has a package for each:

| Package | Platform |
|---|---|
| `cartograph-linux-amd64`, `cartograph-linux-arm64`, `cartograph-linux-riscv64` | Linux |
| `cartograph-darwin-amd64`, `cartograph-darwin-arm64` | macOS |
| `cartograph-windows-amd64`, `cartograph-windows-arm64` | Windows |
| `cartograph-freebsd-amd64` | FreeBSD |

    nix build .#cartograph-windows-amd64
    nix build .#release        # all of them, named as a release names them, with SHA256SUMS
    just release               # the same, into dist/

The tests run in the native build (`.#cartograph`) only: a test binary
for another platform cannot run here.

Code that differs by platform sits in files with a build constraint
(`cmd/cartograph/detach_unix.go`, `detach_windows.go`), never behind a
check at run time.

## The images

A container image is files laid out in layers. The flake lays out each
architecture's from that architecture's own packages, which
cache.nixos.org holds already built, so an x86_64 machine makes an arm64
image without running any arm64 code:

    nix build .#image-linux-arm64
    nix build .#image-chromium-linux-arm64
    nix build .#image-laya-linux-arm64

`image`, `image-chromium` and `image-laya` stay as the native ones.

The Laya sidecar is Node and the ONNX runtime's native library.
`laya-linux-<arch>` builds it for an architecture with that
architecture's shell, Node (`nodejs-slim`, which the cache holds) and
C++ runtime. It is written so that nothing of the target runs while it
is built.

## What an image holds

Each image holds what its program runs on and nothing else: no shell, no
package manager, no build tools, and it runs as user 65532.

| Image | Holds | Size |
|---|---|---|
| `cartograph` | the static binary (stripped, no C library), CA certificates, and the time zone, service and MIME tables Go reads | about 44 MB |
| `cartograph-laya` | Node (the binary alone), the libraries it and the ONNX runtime load, the server and its modules, the model | about 1.9 GB, the model 1.6 GB of it |
| `cartograph-chromium` | the binary and Chromium with what Chromium loads | Chromium's size |

Node's package names the build headers of its libraries (the `-dev`
outputs, and ICU's carries a shell and coreutils) in its build
configuration, though nothing reads them at run time; the Laya image
copies the `node` binary with those references blanked
(`nodeRuntimeFor`). The modules' scripts keep `#!/usr/bin/env node`
rather than the build's Node. The image starts Node on the server
directly, the model's directory set in its environment; the wrapper with
a shell is for running it on a machine (`nix run .#laya`).

## Proof

A release builds every binary and image on one x86_64 runner. Then a
runner of each architecture pulls that architecture's images and runs
them: the binary must answer `ready`, and the decision model must give
every answer recorded on x86_64 within 0.005 (`just decide-measure
<url>`). Only then are the multi-platform tags made.

macOS, Windows and FreeBSD binaries are built and named in a release,
but no runner of theirs runs them yet.
