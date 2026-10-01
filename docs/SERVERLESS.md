# Stateless operation and serverless deployment

**2026-10-01. An audit of what holds state in the process, what the
ports allow, and the plan to a deployment where any replica can serve
any request and scale to zero.**

The short answer: the engine is stateless by design, and every piece
of state lives behind a port. The vault adapters keep state on one
volume; the Postgres adapters keep it in one database. With
`CARTOGRAPH_STORE` set to a Postgres URL, the stores and the fan-out a
stateless deployment needs are built. What is left is wiring the shared
drafts to them (ADR 0007), presence, and printing without Chromium.

## 1. What holds state today

| State | Where it lives in the reference build | Port | Stateless adapter |
|---|---|---|---|
| Manifest versions, working copies, references, exclusions | Files under the vault directory plus `.cartograph/index.sqlite` | `store.ManifestStore`, `store.VaultIndex` | Postgres, built (`store/postgres`: rows for versions and working copies; the database is the store and there is no apply gate) |
| Project state history | The same SQLite file | `store.OperationalStore` | Postgres, built |
| The shared drafts, as CRDT documents | The same SQLite file (`documents`, `document_chunks`) | `store.DocStore` | Postgres, built: a snapshot per document plus the chunks appended since |
| The shared draft's edits under 1.x | Memory (`memory.OpLog`) | `store.OpLog` | None: the op log goes in 2.0.0, replaced by the documents above |
| Draft state per open manifest | Memory, inside `engine.Drafts` | none needed: it is a cache rebuilt on first touch | Keep as a per-replica cache; correctness does not depend on it |
| Conflict notes | Memory, inside `engine.Drafts` | none | Derived from the document on read in 2.0.0 (ADR 0007), so nothing to store |
| Fan-out between replicas | Memory (`fanout/memory`) | `fanout.Bus` | Postgres `LISTEN/NOTIFY`, built (`fanout/postgres`); a broker such as NATS in a distribution |
| Presence | Not built | `fanout.Bus` | Relayed over the fan-out, never stored |
| Handoff bundles | Files under `.cartograph/handoff` | `store.BundleStore` | Postgres rows, built; object storage remains an option behind the same port |
| The embedded interface | The binary | none | none needed |
| The file watcher | A goroutine over the vault directory | none | Absent: a database has nothing to watch |
| PDF printing | A Chromium subprocess | `printer.Printer` | A remote printer service, or `printer.None` and a separate renderer |

The engine itself keeps nothing between requests except what the
adapters give it, and `cmd/cartograph` already composes adapters from
configuration. A replica holds no identity: the authenticator reads the
request, the authorizer decides per request.

## 2. What stateless means here

- Any replica can serve any request: no sticky sessions, no local
  files the next request needs.
- A replica can be killed at any moment: the two-phase apply journal
  already makes a kill safe for the vault adapter; a database adapter
  gets the same from a transaction.
- A replica can start from nothing: no index to rebuild (the database
  is the index), no vault to scan.
- Scale to zero: the first request after idle pays the engine's start
  (schema compilation, under 100 ms) and nothing else.

Two things do not fit a request-per-invocation platform (Lambda, Cloud
Functions) and fit a container platform that scales to zero (Cloud Run,
Fly Machines, Knative, Azure Container Apps) well: the Server-Sent Events
stream, and the draft service's in-memory state. The stream is a
long-lived connection; where a platform forbids one, an interface falls
back to polling `OpsSince`, which the client port already carries. The
draft cache is per replica and harmless, since every replica rebuilds
it from the same log.

## 3. The plan

Each step is one adapter proven by a conformance suite, then selected
by configuration. Steps 1 to 4 are built. Their suites run against a
real Postgres in CI (`just test-postgres`), and the ten-second gate
stays database-free.

1. **Built: `store/postgres`, `ManifestStore` and `OperationalStore`.**
   Passes `conformance.RunManifestStore` and `RunOperationalStore`. No
   `StateStore`: every row is live, and the apply gate answers "no
   state manifest", which the interfaces already handle. Selected by
   `CARTOGRAPH_STORE=postgres://...`. Migrations are embedded and
   applied when a replica starts, under an advisory lock.
2. **Built: `store/postgres`, `DocStore`.** Passes
   `conformance.RunDocStore`, which races two creates of one document,
   concurrent appends, and an append during compaction. The SQLite
   index and the memory adapter pass the same suite.
3. **Built: `fanout/postgres`, `fanout.Bus` over `LISTEN/NOTIFY`.** One
   listening connection per replica, one channel, the topic in the
   payload; it reconnects when the connection drops. Passes
   `fanout/conformance.Run` with two Buses on one database. Selected by
   `CARTOGRAPH_FANOUT`, which defaults to it with a Postgres store.
4. **Built: `store/postgres`, `BundleStore`.** Handoff bundles as rows.
   An object store adapter can replace it behind the same port.
5. **Wire the shared drafts** to `DocStore` and `fanout.Bus` in the
   engine and the sync endpoint (ADR 0007, ADR 0008). The composition
   root already hands both over.
6. **Presence**, relayed over the fan-out and never stored.
7. **A `printer` adapter** that calls a rendering service, for images
   without Chromium; or accept `printer.None` and render PDFs out of
   band.

After 5 the deployment is stateless for everything a person does:
`cartograph serve` with a Postgres URL and no volume, on any container
platform, on either architecture, with the image the flake builds.

## 4. What does not change

The contract, the engine, the kinds, the codecs, the interfaces, the
command line. `cartograph import` into a Postgres store is the same
command as into a vault. A vault on disk remains the right deployment
for one person, a small team, or anyone who wants their definitions in
git; the two are the same engine with different adapters, which is the
whole point of the ports.
