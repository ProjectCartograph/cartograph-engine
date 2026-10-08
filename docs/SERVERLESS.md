# Stateless operation and serverless deployment

2026-10-02, engine 2.5.0. What holds state in the process, what the
ports allow, and how a deployment runs where any replica can serve any
request and scale to zero.

In short, the engine is stateless by design, and every piece of state
lives behind a port. The vault adapters keep state on one volume; the
Postgres adapters keep it in one database. With `CARTOGRAPH_STORE` set
to a Postgres URL, everything a person does is stateless: the stores,
the shared drafts, the fan-out between replicas and presence (ADR 0007,
ADR 0008). What is left is printing without Chromium.

## 1. What holds state

| State | Where it lives with a vault | Port | With a Postgres store |
|---|---|---|---|
| Manifest versions, working copies, references, exclusions | Files under the vault directory plus `.cartograph/index.sqlite` | `store.ManifestStore`, `store.VaultIndex` | A row per manifest with its current document (`jsonb`), and a row per version and working copy (`store/postgres`, ADR 0012); the database is the store and there is no apply gate |
| Project state history | The same SQLite file | `store.OperationalStore` | Rows |
| The access list (people, their roles and teams) | The same SQLite file (`people`) | `store.AccessStore` | Rows (`people`) |
| The team tree, for team checks | Memory, inside the engine, per replica, for 30 seconds | none: a cache | The same cache; a team change reaches every replica within 30 seconds |
| The shared drafts, as Automerge documents | The same SQLite file (`documents`, `document_chunks`) | `store.DocStore` | A snapshot per document plus the chunks appended since |
| Open documents | Memory, inside `engine.Shared`, per replica | none: a cache | The same cache, refreshed from the `DocStore` before every use, so a replica never serves an older document than the store holds |
| Sync state per connection | Memory, inside the sync socket, per connection | none: a cache | The same; a new connection starts from heads and arrives at the same place |
| Conflict notes | Not held: derived on read from the document (`Shared.Conflicts`) | `crdt.Doc` | Nothing to store |
| Fan-out between replicas | Memory (`fanout/memory`): one replica | `fanout.Bus` | Postgres `LISTEN/NOTIFY` (`fanout/postgres`); a broker such as NATS in a distribution |
| Presence | Never stored: relayed on the sync socket | `fanout.Bus` | Relayed over the fan-out, never stored |
| Events (a version saved, a state changed) | Memory (`engine.Bus`), per process | `engine.Bus` | The same; only in-process subscribers hear them, since the events endpoint is not built |
| Handoff bundles | Files under `.cartograph/handoff` | `store.BundleStore` | Rows; object storage remains an option behind the same port |
| The embedded interface | The binary | none | none needed |
| The file watcher | A goroutine over the vault directory | none | Absent: a database has nothing to watch |
| PDF printing | A Chromium subprocess | `printer.Printer` | The same, or `printer.None` and a separate renderer |

The engine itself keeps nothing between requests except what the
adapters give it, and `cmd/cartograph` composes adapters from
configuration. A replica holds no identity. The authenticator reads the
request and the authorizer decides per request.

## 2. What stateless means here

- Any replica can serve any request. There are no sticky sessions and
  no local files the next request needs.
- A replica can be killed at any moment. The two-phase apply journal
  makes a kill safe for the vault adapter; the Postgres adapters get
  the same from a transaction. A change the replica had accepted but
  not stored was never acknowledged, and the interface sends it again.
- A replica can start from nothing, with no index to rebuild (the
  database is the index) and no vault to scan.
- A deployment can scale to zero. A cold start pays for compiling the
  schemas (under 100 ms) and the Automerge module (about a quarter of a
  second), once per process. An open window does not keep a replica:
  the interface closes its sync socket when nobody is at it, and the
  server closes one that has changed nothing for `CARTOGRAPH_SYNC_IDLE`
  (ADR 0015).
- A fan-out message is a hint. A replica that misses one still
  converges, because the sync protocol compares heads on every
  exchange and every connection offers its documents again every 15
  seconds. A lost message costs latency, never an edit.

A container platform that scales to zero (Cloud Run, Fly Machines,
Knative, Azure Container Apps) runs this as it is. A
request-per-invocation platform (Lambda, Cloud Functions) can serve
every request of the HTTP contract, but it cannot hold a long-lived
connection, so interfaces cannot hold the sync socket on it. Live
editing and presence are what such a platform cannot host.

## 3. What was built, and what is left

Each step is one adapter proven by a conformance suite, then selected
by configuration. The Postgres suites run against a real database
locally (`just test-integration`), and the gate stays database-free.

1. `store/postgres`, `ManifestStore` and `OperationalStore`. Pass
   `conformance.RunManifestStore` and `RunOperationalStore`. No
   `StateStore`: every row is live, and the apply gate answers "no
   state manifest", which the interfaces already handle. Selected by
   `CARTOGRAPH_STORE=postgres://...`. Migrations are embedded and
   applied when a replica starts, under an advisory lock.
2. `store/postgres`, `DocStore`. Passes `conformance.RunDocStore`,
   which races two creates of one document, concurrent appends, and an
   append during compaction. The SQLite index and the memory adapter
   pass the same suite.
3. `fanout/postgres`, `fanout.Bus` over `LISTEN/NOTIFY`. One
   listening connection per replica, one channel, the topic in the
   payload; it reconnects when the connection drops. Passes
   `fanout/conformance.Run` with two Buses on one database. Selected by
   `CARTOGRAPH_FANOUT`, which defaults to it with a Postgres store.
4. `store/postgres`, `BundleStore`. Handoff bundles as rows. An
   object store adapter can replace it behind the same port.
5. Shared drafts over `DocStore` and `fanout.Bus`. `engine.Shared`
   and the sync socket (`/api/v1/sync`), with Automerge as the CRDT
   (ADR 0007, ADR 0008).
6. Presence, relayed over the fan-out and never stored.

Left to do:

7. A `printer` adapter that calls a rendering service, for images
   without Chromium; or accept `printer.None` and render PDFs out of
   band.

The deployment is stateless for everything a person does:
`cartograph serve` with a Postgres URL and no volume, on any container
platform, on either architecture, with the image the flake builds.

## 4. What does not change

The contract, the engine, the kinds, the codecs, the interfaces, the
command line. `cartograph import` into a Postgres store is the same
command as into a vault. A vault on disk remains the right deployment
for one person, a small team, or anyone who wants their definitions in
git. The two are the same engine with different adapters, which is
what the ports are for.
