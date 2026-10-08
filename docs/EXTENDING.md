# Extending Cartograph

Every extension follows one shape: write an adapter for a port, prove it
with the port's conformance suite, select it in the composition root
(`cmd/cartograph`). The engine, the contract and the web interface do
not change. `ARCHITECTURE.md` section 7 lists the ports; this page says
how to extend them.

Before adding a kind, a field that links two kinds, or a rule that
refuses a link, read `docs/TAXONOMY.md`. If the discipline has a word
for what you are building, use that word with that meaning. If
Cartograph must depart from it, add a decision to that file saying why.

## A new kind

1. Write `contract/schemas/<kind>.schema.json` (JSON Schema 2020-12).
   Reference another kind with `"x-cartograph-ref": "Team"` on the field;
   `"*"` accepts any kind. Mark the properties the sheet should show in
   the order you write them; the server serves the file bytes so that
   order survives.
2. Add one line to `server/internal/kinds/registry.go`:
   `{Name: "Widget", SchemaFile: "widget.schema.json"}`. Order in that
   list is the listing order everywhere.
3. If the kind has rules its schema cannot say (two fields that depend on
   each other, a check against another manifest), add
   `server/internal/kinds/widget/rules.go` with
   `func Rules(doc map[string]any, ctx kit.RuleContext) []kit.Problem`
   and set `Rules: widget.Rules` in the registry. Rules see other
   manifests through `ctx.Lookup`; they never import the engine.
4. Run `just generate` (schemas are copied into the Go module and the
   TypeScript types regenerate), then `just ci`.
5. Give the kind its words in `cartograph-ui's src/copy.ts` and, if it
   has a fixed vocabulary, marks in
   `cartograph-ui's src/components/vocab.tsx`. The directory sheet
   renders any kind from its schema; a kind with its own flow (as
   Project and Goal have) gets its own route under
   `cartograph-ui's src/routes`.
6. Add the kind to `examples/minimal` with fictional, organisation-free
   content, and to the example-import test's list of kinds the flow
   reaches for. `just words` must stay clean.

A kind cannot name a person. Definitions name roles
(`Resource`); mapping roles to people lives in the delivery tool.

## A new rule on an existing kind

Add it to that kind's `Rules`. A rule returns problems with a JSON
pointer path (`/spec/keyResults/0/unit`) so the interface can land it on
the field, and a message a person can act on. Rules shared by several
kinds live in `kinds/kit` (see `KeyResultUnitProblem`). A rule must never
block a save; the engine already treats rule problems as a 422 on a
validating write and as advice on a working save. A check that should
block a handoff is a project check in `engine/projectchecks.go` with
state `block`, not a rule.

## A new manifest store

Implement `store.ManifestStore` in `internal/store/<name>/`. The
interface is in `store/store.go`; read the comments on `PutVersion`
(numbers must be exactly current plus one) and `WithinTransaction`
(all writes in `fn` commit together or not at all).

Prove it:

```go
func TestConformance(t *testing.T) {
    conformance.RunManifestStore(t, func(t *testing.T) store.ManifestStore {
        return newYourStore(t)
    })
}
```

The same suite runs against SQLite, memory and Postgres, which is what
makes them interchangeable. An adapter that also keeps project state
implements `store.OperationalStore` and runs `RunOperationalStore`. One
that keeps the shared drafts implements `store.DocStore` and runs
`RunDocStore`, which races creates, appends and compaction the way two
replicas would.

A store that reads across a network should also answer for sets
(`store.SetReader`: every manifest of a kind, and who references every
manifest of a kind, in one call each) and know a manifest's latest
version number without reading its history (`store.VersionCounter`).
The engine uses them where it would otherwise loop, and loops when they
are missing, so they are a matter of speed, never of correctness
(ADR 0012). The same conformance suite checks that their answers match
the per-manifest ones.

Select it in `compose` in `cmd/cartograph/store.go`. That function is
the one place a path or a URL becomes an adapter. The Postgres adapter
(`internal/store/postgres`) is the worked example. `CARTOGRAPH_STORE`
holds its URL, and `compose` builds every store from one pool. Tests
that need a server skip unless `CARTOGRAPH_TEST_POSTGRES` is set, and
`just test-integration` starts a throwaway one, so `just test` stays fast.

A store where every row is live does not implement `store.StateStore`,
and the state endpoints answer "this store has no state manifest". A
store that keeps handoff bundles implements `store.BundleStore`; one that
does not can be paired with another through `engine.WithBundles`.

## A different index behind the vault

The vault keeps versions, references, summaries, the journal, file
hashes and a little metadata in a `store.VaultIndex`. SQLite implements
it. To put that in Postgres, implement the interface in
`store/postgres`, pass `conformance.RunVaultIndex`, and construct the
vault with `vault.Options{OpenIndex: yourOpener}` in
`cmd/cartograph/store.go`. The files stay where they are; only the cache
moves. For several replicas, the Postgres store above is the simpler
path, because it needs no shared volume.

## A manifest syntax

Implement `codec.Codec` in `server/internal/codec/<name>/`:

```go
type Codec interface {
    Name() string
    Extension() string                              // ".toml"
    Decode(text []byte) (map[string]any, error)     // the document a schema validates
    DecodeInto(text []byte, v any) error            // typed views carry `yaml` tags
    Encode(v any) ([]byte, error)                   // canonical, stable text
}
```

Pass `codec/conformance.Run`, which checks a stable encode, a lossless
round trip, the typed decode and a refusal of malformed text. Add the
name to `config.Config.Codec`'s validation and to `codecFor` in
`cmd/cartograph/store.go`. The vault names its files by `Extension()`;
`cartograph export --codec <name>` is how an existing vault moves. The
JSON adapter is 90 lines and shows the one trick: typed views are
decoded through YAML's field mapping so a second syntax needs no second
set of struct tags.

## Authentication

Implement `auth.Authenticator`:

```go
type Authenticator interface {
    Authenticate(r *http.Request) (auth.Principal, error)
}
```

Return `auth.Anonymous` for a request with no identity when that is
allowed, `auth.ErrUnauthenticated` to refuse it, or a `Principal` with a
stable `Subject` (what versions record as the actor) and `Roles`. Put the
adapter under `server/internal/auth/<name>/`, add a value to
`config.Config.Auth` and select it in `cmd/cartograph/serve.go`. The
middleware, the context plumbing and the handlers' use of the principal
are already there; `auth/proxy` is a 60-line example.

An OIDC adapter would verify a bearer token against the provider's keys
and build the principal from its claims. Nothing else changes. The
shipped route to OIDC needs no adapter. An OIDC proxy (oauth2-proxy)
sits in front of `auth/proxy`, with an identity broker (Dex) in front
of the provider, which is what `cartograph-oidc` does.

## Authorization and user types

Implement `auth.Authorizer`:

```go
type Authorizer interface {
    Authorize(ctx context.Context, p auth.Principal, a auth.Action) error
}
```

`Action` carries a verb (`read` for safe methods and validation, `write`
for the rest), the kind and id when the request names one manifest, and
the resource when it concerns the access list, the vault or the
session. The policy is asked twice. `auth.Authorize` wraps the whole
API after authentication, classifies every request with
`auth.ActionFor`, and answers 403 in the contract's problem shape when
the policy refuses; it cannot see the manifest, so `Action.Change` is
nil there. The engine then asks again at every write, through
`engine.WithAuthorizer`, with `Change` set: the chains of teams the
manifest sits under before and after the write, and the spec fields it
changes. A policy that decides by content decides at the second call
and lets the first through. Every operation in `openapi.yaml` declares
401 and 403.

Three adapters ship, selected by `CARTOGRAPH_AUTHZ`: `auth.AllowAll`,
`auth/roles` (read roles, write roles, anonymous refused) and
`auth/access` (four roles and teams over an access list, ADR 0011). A
policy that also implements `auth.Scoper` tells the session, and so the
interface, how much of each kind a principal may write. The types an
authorizer sees live in `internal/identity`, which the engine may
import; `auth` re-exports them. Roles from the authenticator
(`Principal.Roles`) are directory groups; `auth/access` maps them onto
its roles through `CARTOGRAPH_ACCESS_FILE`, and keeps people in the
`store.AccessStore` port, never as manifests (TAXONOMY.md D27).

A deployment that wants a fifth role, or a team rule of its own,
writes another policy over the same `Action` and `Change`, and selects
it in `cmd/cartograph/serve.go`. A new store holds the access list by
passing `conformance.RunAccessStore`.

## A PDF printer

Implement `printer.Printer` (`Print(ctx, html) ([]byte, error)`), return
`printer.ErrUnavailable` when the machine cannot print, and construct it
in `cmd/cartograph/serve.go` and `cmd/cartograph/printer.go`. A remote
printing service, or a Go PDF library, slots in without the API knowing.

## A bundle store

Implement `store.BundleStore`. `PutBundle` must be all-or-nothing and
return a location the state history can record. The vault writes
files, Postgres writes rows; object storage is the obvious next
adapter.

## A fan-out adapter

`fanout.Bus` (`internal/fanout`) carries a small message from one
replica to every other: "this document changed", and presence. The
port promises little, on purpose:

- `Publish` sends to every current subscriber of a topic, on every
  replica, the publisher's own included. It returns once the adapter
  has accepted the message, not once it is delivered.
- Delivery is at most once and best effort. A subscriber that falls
  behind loses messages; it never slows a publisher.
- A message is at most `fanout.MaxPayload` bytes of any value, on a
  topic of at most `fanout.MaxTopic` bytes. Larger is `ErrTooLarge`,
  never truncated.
- A subscription ends when it is closed, when its context ends, or
  when the Bus is closed, and its channel is then closed.

Nothing else depends on delivery. Every message is a hint. A client
that misses one converges anyway, because the sync protocol compares
state on every exchange. So an adapter needs no persistence, no
acknowledgements and no ordering.

Prove it with the suite, where every Bus `newBus` returns shares one
backend, as two replicas share one broker:

```go
func TestConformance(t *testing.T) {
    conformance.Run(t, func(t *testing.T) fanout.Bus {
        b := newYourBus(t) // a new connection to the shared backend
        t.Cleanup(func() { b.Close() })
        return b
    })
}
```

The suite checks delivery across two Buses, topic isolation, that the
publisher hears itself, binary data at the largest size, the limits,
unsubscribing, a slow subscriber, and closing.

NATS is the worked example of what a distribution would write. Publish
is `nc.Publish(prefix+topic, data)`; NATS subjects already carry the
topic, and its default message size is far above the limit. Subscribe
is `nc.ChanSubscribe(prefix+topic, ch)` on a buffered channel; a slow
consumer is what NATS drops by default, which is the behaviour the
port wants. Close drains the connection and closes every subscription.
Its test starts a server in process (`nats-server/v2/test`) and runs
`conformance.Run` against two connections to it.

Add a value for `CARTOGRAPH_FANOUT` in `internal/config` and a case for
it in `compose` in `cmd/cartograph/store.go`. The adapter lives in its
own package with a rule in `internal/arch`, and its driver is allowed
there and nowhere else. The memory adapter serves one replica; the
Postgres adapter uses `LISTEN/NOTIFY` on the store's database and one
listening connection per replica.

## A different CRDT engine

The shared drafts reach their CRDT through `crdt.Engine` and
`crdt.Doc` (`internal/crdt`). The one adapter, `crdt/automerge`, runs
Automerge compiled to WebAssembly from the `crdt/` crate, on wazero.
Another adapter must pass `internal/crdt/conformance`:

```go
func TestConformance(t *testing.T) {
    conformance.Run(t, func(t *testing.T) crdt.Engine {
        return newYourEngine(t)
    })
}
```

The suite checks save and load round trips, that a reconcile is
minimal and idempotent, that keyed list items keep their identity,
that texts merge character by character, that concurrent writes are
kept as conflicts, and that replicas converge across random partitions
with messages lost, repeated and reordered. Passing it is not the whole
job. Interfaces sync with the stock automerge-repo libraries, so a
different engine also needs interfaces that speak its sync protocol,
and the sync socket's protocol version is part of what a release
promises (`VERSIONING.md`). Read ADR 0007 first.

## Rules for every extension

- An adapter imports the core; the core never imports an adapter.
- An adapter has a conformance test or it is not done.
- No adapter writes to stdout; use `log/slog`.
- No organisation's words anywhere in the repository (`just words`).
- `just test` stays within a few seconds: the adapter's own tests carry the
  `integration` build tag.
- Say in the pull request what you ran and what it printed.
