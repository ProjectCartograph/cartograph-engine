# 0008. Postgres holds the state; fan-out is a port

**Status:** Accepted

## Context

The engine is meant to keep nothing between requests except through a
port (`SERVERLESS.md`). It runs as twelve-factor processes: any replica
serves any request, a replica can be killed at any moment, and the
deployment scales out and in, down to zero, without coordination.

The reference adapters do not allow that. The vault is a directory on
one volume. The op log, conflict notes and event bus lived in process
memory. Collaborative editing (0007) adds two more needs: CRDT documents
that every replica can read and append to, and a way for a change
accepted by one replica to reach the people connected to another.

## Decision

**Postgres is the backing service for a stateless deployment.** One
database holds everything that must outlive a request:

| State | Port | Postgres adapter |
|---|---|---|
| Manifest versions, working copies, references, summaries, exclusions | `store.ManifestStore` | rows per version and per working copy |
| Project state history | `store.OperationalStore` | rows |
| CRDT documents | `store.DocStore` | a compacted snapshot plus append-only change chunks per document |
| Handoff bundles | `store.BundleStore` | rows (bytea); object storage remains an option behind the same port |

A deployment selects it with `CARTOGRAPH_STORE=postgres://...`.
`CARTOGRAPH_VAULT` keeps the vault on disk for one person or a small
team, and the two are the same engine with different adapters.

**Fan-out is a port of its own** (`fanout.Bus`): publish a small message
on a topic, and every subscriber on every replica receives it. The
engine uses it for two things: "document D changed" after a replica
stores a CRDT change, and relaying presence. The Postgres adapter uses
`LISTEN/NOTIFY`, so a deployment needs no second service. A
distribution that outgrows one database connection per replica, or
wants fan-out across regions, supplies another adapter (NATS, Redis
streams, a cloud pub/sub) that passes the same conformance suite, and
selects it with `CARTOGRAPH_FANOUT`. Nothing else changes.

**Correctness never depends on fan-out.** The port promises at-most-once,
best-effort delivery, and the engine treats it as a hint. A replica
that misses "document D changed" still converges, because the
Automerge sync protocol compares heads every time a peer sends a
message. Connections also send a periodic heads check, and a client
that reconnects resynchronises from heads. A lost notification costs
latency, never an edit. That is what makes a network partition between
replicas, or between a replica and the database's notification
channel, safe.

**Nothing is kept per replica that a request needs.** Each connection
keeps an Automerge sync state for its own lifetime, and a replica may
cache documents keyed by their heads. Both are caches: a new replica,
or a reconnecting client, starts from heads and arrives at the same
place.

## Options considered

**Postgres and NATS from the start.** Fan-out would scale further. But
every deployment would run two services to get what one database
gives a small or medium team. The port keeps NATS one adapter away for
those who need it.

**Redis for presence and fan-out.** Presence is ephemeral and needs no
store, only fan-out, so Redis would add a service for a job
`LISTEN/NOTIFY` does.

**Sticky sessions, one replica per document.** This would avoid
cross-replica fan-out. But it makes the load balancer part of
correctness, breaks scale-to-zero, and fails the twelve-factor process
model.

## Consequences

- `LISTEN/NOTIFY` payloads are limited to 8000 bytes. Change
  notifications carry only a document id and its new heads. A presence
  message over the limit is dropped, because presence is lossy by
  design and the next one replaces it.
- Each replica holds one listening database connection, plus a pool.
  That fits deployments up to the size where a distribution would
  bring its own fan-out adapter anyway.
- Adapter conformance suites run against a real Postgres in a CI job
  of their own (`just test-postgres`). The ten-second gate stays
  database-free, using the memory adapters.
- A test starts two engines on one database, edits on both, and checks
  that both converge with nothing kept between requests.
