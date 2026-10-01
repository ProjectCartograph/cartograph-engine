# 0007. Automerge is the one CRDT

**Status:** Accepted. Supersedes the replicated types in `pkg/merge`.

## Context

People define work together. Two or more of them open the same
manifest, from a browser or a terminal, on one replica of the engine or
on several, sometimes over a connection that drops. Each must see the
others' edits as they happen, keep working while disconnected, and
converge with everyone else when the connection returns, without a
coordinator deciding whose edit counts. That is optimistic replication
with strong eventual consistency, and conflict-free replicated data
types (CRDTs) are the standard way to get it.

Cartograph had its own CRDTs in `pkg/merge`: a last-writer-wins map
over leaf paths, a Replicated Growable Array for sentences, and hybrid
logical clocks. Reading it against the requirements above found six
defects, each enough on its own to lose an edit:

1. Clocks were keyed by the person, not the replica. Two tabs, or two
   engine replicas acting for one person, could issue the same clock.
   Equal clocks on different values diverge replicas; equal character
   ids drop characters.
2. Saving a version reseeded every sentence with new character ids. An
   edit made offline and anchored to the old ids could never apply
   after reconnecting.
3. Replaying `State.Ops()` into a new state duplicated every sentence.
4. Deleting an item from a keyed list while someone edited a field
   inside it brought back a partial item holding only that field.
5. Fractional ranks (`Between`) stopped ordering correctly after a few
   inserts at the same place.
6. Each replica wrote the materialised working copy from its own cache,
   so an older state could overwrite a newer one.

Fixing those would leave a hand-made CRDT in two languages, Go and a
TypeScript port the web interface still needed, each held to the other
by test vectors. The research behind production CRDTs (Kleppmann and
others on interleaving anomalies, move operations, and Byzantine and
partition behaviour) would have to be re-derived in both.

## Decision

[Automerge](https://automerge.org) (Kleppmann, Beresford, and the
Ink & Switch contributors) is the one CRDT, in the engine and in every
interface.

**Data model.** One Automerge document per manifest, holding the
manifest's JSON: maps for objects, lists for arrays, `Text` for the
fields the schema marks as sentences, and scalars elsewhere. Lists are
Automerge's RGA-based sequences, so an item has an identity of its own
and a concurrent edit inside a deleted item cannot resurrect it. A map
key written concurrently keeps every concurrent value, which Automerge
exposes as conflicts. That is the multi-value register of the CRDT
literature, and it is where Cartograph's conflict notes now come from,
derived on read rather than stored.

**Engine.** The `automerge` Rust crate, pinned in `crdt/Cargo.lock`, is
compiled to `wasm32-wasip1` from a small in-tree crate (`crdt/`) that
exports the operations the engine needs: load, save, incremental
save, apply a reconciled document, splice text, heads, conflicts, and
the sync protocol. The flake builds it reproducibly; the module is
committed, like other generated code, and `just generate` rebuilds it
so `just drift` refuses a module that does not match its source. It
runs on [wazero](https://wazero.io), a WebAssembly runtime written in
Go, so the binary stays `CGO_ENABLED=0`, static, and cross-compiled
for both architectures as before. The engine reaches it through a port
(`internal/crdt`), with this as its adapter.

**Wire.** The engine speaks the automerge-repo network protocol,
version 1 (CBOR messages over a WebSocket: `join`, `peer`, `request`,
`sync`, `doc-unavailable`, `ephemeral`), at `/api/v1/sync`. An
interface uses the stock `@automerge/automerge-repo` with its WebSocket
adapter; nothing about the sync protocol is Cartograph's own. A
document's id is looked up through the HTTP contract
(`GET /manifests/{kind}/{id}/document`), never derived by a client.

**Genesis.** A manifest's document is created once, from its working
copy or current version, by a change whose actor and timestamp are
derived from the manifest, not chosen at random. Two replicas creating
it at the same moment produce byte-identical changes with the same
hash, which Automerge treats as one.

**Versions.** A version still means what it meant: an immutable,
attributed save that `diff`, `export`, the charter and the handoff
read. Saving one materialises the document and validates it. The
document carries on across versions instead of being reseeded, so an
edit made offline before a version was saved still merges after it.

**Writes that are not edits.** A whole document arriving through
`SaveWorking`, an import, or a file changed in a vault is reconciled
into the CRDT document as one change. Objects are diffed key by key,
lists of identified items are matched by their `x-cartograph-list-key`
so an item keeps its identity, and sentences are spliced, not
replaced. Every path into a draft ends in the same document.

**Presence.** Who is looking at what, their pointer, their focused
field and their caret travel as automerge-repo ephemeral messages. They
are relayed, never stored. A caret is an Automerge cursor, so it stays
on the right character while others type. Presence names the
authenticated principal of a live session. It is not a record of a
person, and nothing about it outlives the session.

**Versioning.** `pkg/merge` and `client.Client`'s `Edit` and `OpsSince`
are deprecated in 1.1.0 and removed in 2.0.0, which ships everything
above. `VERSIONING.md` requires the deprecation to come one minor
release first.

## Options considered

**Harden `pkg/merge` and port it to TypeScript.** This keeps full
control and the smallest binary. But it means two hand-written CRDTs
held together by test vectors, and every result in the CRDT
literature has to be re-derived in both. The six defects above were
found by reading, not by tests, which says how hard that is to get
right.

**`automerge-go` (cgo).** This is the official Go binding, but it has
no tagged release and has not changed since 2024. It links prebuilt
static libraries through cgo, which ends `CGO_ENABLED=0`, complicates
the arm64 cross-build, and pins an old core.

**A third-party wazero build of Automerge.** One exists and it proves
the technique. But it has one maintainer and no releases, and it would
sit under the engine's core. The same technique is adopted in-tree
instead, with the crate pinned and the module rebuilt and checked by
CI.

**Yjs (YATA), through `yrs`.** Yjs is equally mature, with the same
WebAssembly route. It was not chosen: Automerge's JSON document model
matches a manifest directly, and its sync protocol, repo layer and
ephemeral messages cover the transport and presence without
Cartograph inventing any.

## Consequences

- One CRDT, one sync protocol, one presence channel, from the engine to
  every interface. Convergence, partition behaviour and offline editing
  are Automerge's properties, tested there and tested again here
  through the port's conformance suite. That suite drives replicas
  through random partitions, with messages lost, duplicated and
  reordered.
- The flake gains a Rust toolchain for one purpose, regenerating the
  module. Contributors who never touch `crdt/` never build it.
- The binary grows by about a megabyte. Calls into the module cost a
  copy across the WebAssembly boundary. The engine batches: one call
  per sync message or reconcile, never one per field.
- An Automerge document keeps its history. Storage keeps a compacted
  snapshot plus the incremental changes since, and compacts
  periodically. History is pruned only by starting a new document at a
  major version of the contract.
- Version 2.0.0 of the engine and of `cartograph-ui` go together.
