# Stateless operation and serverless deployment

**2026-10-01. An audit of what holds state in the process today, what
the ports already allow, and the plan to a deployment where any replica
can serve any request and scale to zero.**

The short answer: the engine is stateless by design and stateful by its
reference adapters. Every piece of state lives behind a port, so a
serverless deployment is a set of adapters, not a redesign. None of
those adapters exists yet beyond memory and SQLite, so a serverless
deployment is a plan today, not a flag.

## 1. What holds state today

| State | Where it lives in the reference build | Port | Stateless adapter |
|---|---|---|---|
| Manifest versions, working copies, references, exclusions | Files under the vault directory plus `.cartograph/index.sqlite` | `store.ManifestStore`, `store.VaultIndex` | Postgres (rows for versions and working copies; the "vault" is then the database and there is no apply gate) |
| Project state history | The same SQLite file | `store.OperationalStore` | Postgres |
| The shared draft's edits | Memory (`memory.OpLog`) | `store.OpLog` | Postgres (one table, dense sequence per manifest) |
| Draft state per open manifest | Memory, inside `engine.Drafts` | none needed: it is a cache rebuilt from the version and the log on first touch | Keep as a per-replica cache; correctness does not depend on it |
| Conflict notes | Memory, inside `engine.Drafts` | none yet | Either derive from the log on read or persist beside the ops; small |
| Event fan-out | Memory (`engine.MemoryBus`) | `engine.Bus` | Postgres `LISTEN/NOTIFY`, or a broker (NATS, Redis streams) |
| Presence | Not built | the `Bus` plus a TTL store | Redis or a Postgres table with expiry |
| Handoff bundles | Files under `.cartograph/handoff` | `store.BundleStore` | Object storage (S3, GCS, R2) |
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

Each step is one adapter proven by an existing conformance suite, then
selected by configuration. The order follows what each unlocks.

1. **`store/postgres`: `ManifestStore` and `OperationalStore`.** Passes
   `conformance.RunManifestStore` and `RunOperationalStore` against a
   real Postgres in the test. No `StateStore`: every row is live, and
   the apply gate answers "no state manifest", which the interfaces
   already handle. Selected by `CARTOGRAPH_STORE=postgres://...`
   (reserved in `internal/config`; a directory or a SQLite file is
   `CARTOGRAPH_VAULT` as today).
2. **`store/postgres`: `OpLog`.** Passes `RunOpLog`. With 1, the shared
   draft survives replicas and restarts.
3. **`bus/postgres`: `engine.Bus` over `LISTEN/NOTIFY`.** Events reach
   every replica's subscribers. A broker adapter later if one database
   connection per replica is not enough.
4. **`store/objectstore`: `BundleStore`.** Handoff bundles in S3-style
   storage, location recorded in the state history as today.
5. **Presence** with a TTL in Postgres (`presence` table, expiry on
   read) or Redis.
6. **Conflict notes** persisted beside the ops, so a note survives the
   replica that recorded it.
7. **A `printer` adapter** that calls a rendering service, for images
   without Chromium; or accept `printer.None` and render PDFs out of
   band.

After 1 to 4 the deployment is stateless: `cartograph serve` with a
Postgres URL, an object store bucket and no volume, on any container
platform, on either architecture, with the image the flake builds.

## 4. What does not change

The contract, the engine, the kinds, the codecs, the interfaces, the
command line. `cartograph import` into a Postgres store is the same
command as into a vault. A vault on disk remains the right deployment
for one person, a small team, or anyone who wants their definitions in
git; the two are the same engine with different adapters, which is the
whole point of the ports.
