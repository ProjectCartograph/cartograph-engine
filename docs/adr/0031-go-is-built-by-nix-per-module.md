# 0031. Go modules and their compiled packages come from the Nix store

**Status:** Accepted.

## Context

The flake built the binary with buildGoModule, whose modules are one
fixed-output derivation, but every recipe runs `go` in the development
shell, and there Go downloaded modules into its own module cache and
compiled every dependency into its own build cache, neither of them in
the store. A fresh CI runner did both again on every job: in one
measured run, `go generate` spent 20 s building oapi-codegen through
`go tool`, `go vet` 35 s compiling dependencies, and entering the shell
29 s fetching a browser, a database and a cluster `just ci` never uses,
of a 172 s step.

## Decision

Go is built the way Nix builds it, through gomod2nix's overlay:

- `gomod2nix.toml` has one entry per module, each fetched as its own
  derivation, and `cachePackages`, the dependencies' packages.
  `scripts/gomod2nix`, run by `just generate`, keeps it in step with
  go.mod; it downloads only when go.mod's modules change, and it leaves
  programs out of `cachePackages`, which gomod2nix itself does not.
- The binary is `buildGoApplication`, and its `go-cache-env` derivation
  compiles every dependency once into a Go build cache in the store.
- The development shell links `vendor/` to the module tree in the store
  and unpacks that build cache into GOCACHE, with the build's flags
  (`-mod=vendor -trimpath`, `CGO_ENABLED=0`), so its entries are hits.
  Only this repository's packages compile.
- Tools are packages, not `go tool` or `go run ...@latest`:
  oapi-codegen from nixpkgs, apidiff from golang.org/x/exp at one
  commit.
- CI enters `.#ci`, the shell with only what `just ci` runs.

Every one of these is a store path, so Cachix holds it and a runner
fetches it instead of building it.

## Consequences

- Adding a dependency: `GOFLAGS=-mod=mod go get ...`, then `just
  generate`, then enter the shell again for the new vendor tree.
- A new import of an existing module's package is compiled in the shell
  until `just generate` adds it to `cachePackages`; it is never wrong,
  only slower.
- `just compat` compares pkg/ with the base release's tree from git, its
  modules the same store tree, so no recipe downloads modules. The base
  is read with today's dependency versions; pkg/'s own API is what is
  compared.
