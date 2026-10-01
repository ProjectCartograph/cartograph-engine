# 0002. Embed a pinned, checksummed interface release

**Status:** Accepted

## Context

The binary serves the web interface from `internal/spa`, which embeds
a `dist` directory with `go:embed`. The interface is built in a
separate repository, `cartograph-ui`, with its own toolchain (Node,
Vite) and its own release cycle. `UI_VERSION` names which interface
release an engine release embeds (`VERSIONING.md`).

Nothing fetched that release. `internal/spa/dist` is ignored by git,
and `go:embed` refuses a pattern that matches no files, so a clean
checkout could not compile `cmd/cartograph` or `internal/spa`. CI
failed at `go vet`, and the release workflow could not build a binary.

Any fix has to hold in four places that each build the binary: a
contributor's `just` recipes, CI, `just release`, and `nix build`
(which builds the container image and runs in a sandbox with network
access only for fixed-output fetches).

## Decision

The engine embeds a released interface build, identified by version and
pinned by checksum:

- `UI_VERSION` names the `cartograph-ui` release; `UI_SHA256` is the
  SHA-256 of that release's `dist.tar.gz`.
- `scripts/fetch-ui` downloads the asset, refuses it unless the
  checksum matches, and unpacks it into `internal/spa/dist`. A stamp
  beside the directory makes a second run free.
- Every `just` recipe that compiles depends on it. `just ui <path>`
  embeds a local build (a `dist` directory or tarball) for trying an
  interface change before release, and later recipes keep it until
  `just ui` restores the pinned release.
- `flake.nix` fetches the same asset with `fetchurl`, from the same two
  files, and unpacks it in `preBuild`. `just` and `nix build` embed the
  same bytes.

## Options considered

**Commit the built interface to the engine repository.** A clean
checkout builds offline. But the tree would carry minified build output
that no one reviews, every interface release would be a large opaque
diff, and `just clean-tree` forbids tracked `dist/` directories for
exactly that reason.

**Make the embed optional (a placeholder page, or a build tag).** Tests
and `go vet` pass without the interface. But a release built without
the fetch step would ship a binary with no interface, and nothing would
fail. A missing interface should stop the build, not degrade it.

**Build the interface from source inside the engine build.** The
engine would need the interface's source (a submodule or a second
checkout) and the Node toolchain in its flake. That couples the two
repositories' toolchains and release cycles, which is what the split
exists to avoid.

**Fetch a pinned, checksummed release (chosen).** The engine depends on
an artifact the same way it depends on a Go module, by version and
hash. The interface repository's release workflow already publishes
`dist.tar.gz`.

## Consequences

- A fresh checkout needs network access once, for the fetch, before it
  compiles. After that the stamp makes it a no-op.
- An engine commit always embeds the same interface bytes, whoever
  builds it. A tampered or re-uploaded asset fails the checksum instead
  of shipping.
- Embedding a new interface release is a two-file change (`UI_VERSION`,
  `UI_SHA256`) followed by `just ci`; `VERSIONING.md` gives the
  commands.
- The interface's build must stay reproducible enough that its release
  asset is the build CI tested. Today a local `npm run build` at the
  release tag produces byte-identical output.
