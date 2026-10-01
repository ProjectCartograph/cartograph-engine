# 0004. The composition root chooses the vault's index

**Status:** Accepted

## Context

The vault adapter keeps manifests as files and keeps versions,
references, summaries and its journal in an index behind the
`store.VaultIndex` port. `ARCHITECTURE.md` presents that index as
replaceable ("SQLite today, Postgres tomorrow") and `EXTENDING.md`
tells an implementer to pass their index in.

In practice `vault.New` opened `.cartograph/index.sqlite` with the
SQLite adapter whenever no index was passed, which was always. So the
vault package imported the SQLite adapter. Every build that included
the vault also compiled in SQLite, and the choice of index adapter was
made inside another adapter instead of in `cmd`, the one place adapters
are chosen.

## Decision

`vault.Options.OpenIndex` is a function the caller supplies:

```go
OpenIndex func(ctx context.Context, dir string) (store.VaultIndex, error)
```

The vault creates its `.cartograph` directory and calls `OpenIndex`
with it. It still owns its layout; it no longer names an adapter.
`vault.New` refuses to open without one, the way `engine.New` refuses
without a codec. The SQLite adapter provides `OpenVaultIndexIn`, which
fits that type, and `cmd/cartograph/store.go` passes it.

## Options considered

**Keep the default, document it.** No call site changes, but the vault
keeps a compile-time dependency on SQLite, and the dependency rule
must make an exception for one adapter importing another.

**Pass an opened `store.VaultIndex`.** This is simpler to type, but the
caller then needs to know the vault's directory layout to find
`.cartograph/`, and the vault's layout leaks into `cmd`.

**Pass an opener (chosen).** The vault still decides where its cache
lives and the SQLite adapter still names its own file. Only the choice
of adapter moves, and it moves to `cmd`, where every other adapter is
chosen.

## Consequences

- `internal/store/vault` no longer depends on `internal/store/sqlite`,
  and `internal/arch` forbids it from doing so again.
- A Postgres or other index is one opener passed in `cmd`, with no
  change to the vault.
- Every caller of `vault.New` passes an opener. The package is internal,
  so this changes no public API. Tests pass `sqlite.OpenVaultIndexIn`.
