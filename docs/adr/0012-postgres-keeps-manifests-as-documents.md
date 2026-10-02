# 0012. Postgres keeps manifests as documents; store ports ask for sets

**Status:** Accepted

## Why documents, and not tables

A manifest is a document by nature. Each of the 18 kinds is defined by a
JSON Schema, nested several levels deep (a goal's key results, a
project's problems and the changes that answer them), and validated
whole. The schemas change with the discipline (TAXONOMY.md), and a new
kind is meant to be a schema file and nothing else, the way Kubernetes
takes a CRD without a migration and keeps every object in etcd as one
value. Tables per kind would turn every schema change into a database
migration for every deployment, spread one manifest over a dozen
tables, and leave the rules that read it whole (every rule does)
reassembling it on each read. So the record is the document, as
`jsonb`.

What is relational in Cartograph stays relational. References between
manifests, the team a piece of work belongs to, project state history,
the access list and the shared drafts' chunks are facts with their own
identity and their own queries. They are rows, with keys and indexes,
and `manifests` puts one row per manifest beside them so that a join
can reach the current state of anything in one step. Postgres is the
right engine for this mix: `jsonb` with GIN and expression indexes for
the documents, transactions across the documents and the rows, and
`LISTEN/NOTIFY` for the fan-out, in the one service a deployment
already runs. A document database (MongoDB, or FerretDB on Postgres)
would hold the documents as well and the relations less well, and
would add a second service and, in MongoDB's case, a licence that is
not open source.

The cost is that Postgres keeps no author's formatting. YAML comments
and key order live in a vault, which keeps files; a database-backed
deployment returns each manifest in its codec's canonical text. During
this release both are stored (see "Upgrade"); the text goes in the next.

## Context

The Postgres adapter (0008) was written to give the same answers as
the SQLite index under the vault, and took that index's shape: the
manifest as opaque text, and "current" worked out from the history on
every read. The store ports took the vault's costs too. They asked for
one manifest at a time, which is free in process and a network round
trip against a database, and the engine composed them in loops.

Measured on Postgres 17, ten versions per manifest, 1.5 KB of text
each, through a served replica:

| Goals (manifests) | 50 (1,000) | 200 (4,000) | 800 (16,000) | 2,000 (40,000) |
|---|---|---|---|---|
| Goal tree, before | 1.8 s | 23.5 s | 453 s | not run |
| Goal tree, after | 0.010 s | 0.039 s | 0.153 s | 0.368 s |
| Every Project, before | 63 ms | 258 ms | 1.0 s | 1.7 s |
| Every Project, after | 2 ms | 6 ms | 27 ms | 66 ms |
| A page of 50 Projects with specs, after | 13 ms | 13 ms | 13 ms | 13 ms |
| Search by name, after | 1.0 ms | 1.1 ms | 1.7 ms | 1.7 ms |

The goal tree was quadratic: it asked who referenced each goal, twice,
and each ask sorted the whole history. It is now linear in the goals it
returns, and no read the interface makes grows with the history.

## Decision

### The adapter

Migration `0003_documents.sql`:

- `manifests`, one row per manifest: current version, name, document,
  working copy's document, and derived, indexed columns (title, labels,
  team). Primary key (kind, id); trigram indexes on title and id for
  search, where the database may create `pg_trgm`; GIN on the document.
- Every version and working copy keeps its document beside its text.
  History stays append-only and compresses with `lz4` where available.
- Triggers keep `manifests` right for every writer, and decide a race
  for a version number on that one locked row instead of a `max()` over
  the history.
- References are replaced in one round trip, and belong to a manifest
  that exists (a foreign key with cascade).

### The ports

Optional interfaces, so an adapter without them still works and the
engine falls back to the loop it ran before:

- `store.SetReader`: every manifest of a kind, a page of ids, and who
  references every manifest of a kind, each in one call;
- `store.SummaryPager`: one page of a list, by keyset;
- `store.VersionCounter`: a manifest's latest number without its history;
- `store.StateSetReader`: the current state of many projects;
- `store.DocWorkingStore` and `store.DocRepairer`: documents for working
  copies, and for rows written without one.

The engine fills `Version.Doc` at every save. The goal tree, the rules'
lookup, the project list and the expanded list use the sets. The
conformance suite checks that every set answers as the per-manifest
calls do, and an engine test fails if the goal tree's reads grow with
the number of goals.

### Upgrade

Expand, then contract. This release adds the tables and keeps writing
the text the previous one reads; its triggers keep `manifests` right
whichever release wrote. Documents for YAML a 2.2 replica wrote are
decoded by the new replicas in the background, at start and every
minute (400,000 versions took 26 s). The next release drops the text.
The Postgres layout is now a versioned surface (`VERSIONING.md`).

## Options considered

**Tables per kind.** Strongest integrity, plainest SQL; rejected for
the reasons above.

**A document database.** Rejected above: no gain over `jsonb` for this
shape, a second service, and fan-out over change streams.

**Text plus a derived `jsonb` copy.** The first draft of this record.
It kept the unindexed text as the record, which is the problem.

**A materialised view of current state.** Refreshed whole, O(N) per
refresh. Rejected.

**Rules in SQL.** Every rule once per adapter. Rejected; the rules stay
in Go and are fed in one read.

## Consequences

- Reads cost what they return. Reporting in SQL works on the documents
  whatever the codec (`DEPLOYMENT.md`).
- The text columns double storage until the contract release.
- A Postgres deployment no longer returns a manifest byte for byte as
  written once the text is dropped; a vault still does.

## Since

ADR 0013 settles what this record left open. Earlier versions are kept
as keyframes and patches, and lose their text once older than a grace
period; the engine writes a compacted version's text from its document
with the codec. A KPI's readings are kept one row per reading besides
the series, so a reading costs O(1) to record and history no longer
grows with the square of the series.

Still to come: dropping the latest version's and the working copies'
text, once no release that reads it is supported.
