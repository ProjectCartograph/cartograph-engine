# Extending Cartograph

Every extension follows one shape: write an adapter for a port, prove it
with the port's conformance suite, select it in the composition root
(`cmd/cartograph`). The engine, the contract and the web interface do not
change. `ARCHITECTURE.md` section 7 lists the ports; this page is the
how.

Before adding a kind, a field that links two kinds, or a rule that
refuses a link, read docs/
TAXONOMY.md`. If the discipline has a word for what you are building,
use that word with that meaning. If Cartograph must depart from it, add a
decision to that file saying why.

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
5. Give the kind its words in `cartograph-ui's src/copy.ts` and, if it has a fixed
   vocabulary, marks in `cartograph-ui's src/components/vocab.tsx`. The directory
   sheet renders any kind from its schema; a kind with its own flow (as
   Project and Goal have) gets its own route under `cartograph-ui's src/routes`.
6. Add the kind to `examples/minimal` with fictional, organisation-free
   content, and to the example-import test's list of kinds the flow
   reaches for. `just words` must stay clean.

What a kind cannot do: name a person. Definitions name roles
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

Implement `store.ManifestStore` in `server/internal/store/<name>/`. The
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

The same suite runs against SQLite and memory, which is what makes them
interchangeable. An adapter that also keeps project state implements
`store.OperationalStore` and runs `RunOperationalStore`.

Select it in `cmd/cartograph/store.go`: that function is the one place a path
or DSN becomes an adapter. A Postgres adapter would be chosen there from
an `CARTOGRAPH_STORE=postgres://...` style value added to `internal/config`.

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
`cmd/cartograph/store.go`. The files stay where they
are; only the cache moves. This is the step that lets several replicas
share one index.

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
`cartograph export --codec <name>` is how an existing vault moves. The JSON
adapter is 90 lines and shows the one trick: typed views are decoded
through YAML's field mapping so a second syntax needs no second set of
struct tags.

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
and build the principal from its claims. Nothing else changes.

## Authorization and user types

Implement `auth.Authorizer`:

```go
type Authorizer interface {
    Authorize(ctx context.Context, p auth.Principal, a auth.Action) error
}
```

`Action` carries a verb (`read` for safe methods and validation, `write`
for the rest) and the kind and id when the request names one manifest.
`auth.Authorize` is the one enforcement point: it wraps the whole API
after authentication, classifies every request with `auth.ActionFor`,
and answers 403 in the contract's problem shape when the policy
refuses. Every operation in `openapi.yaml` declares 401 and 403.

Two adapters ship: `auth.AllowAll` and `auth/roles` (read roles, write
roles, anonymous refused), selected by `CARTOGRAPH_AUTHZ`. A finer policy, per
kind or per manifest or per state transition, is another adapter over
the same `Action`; nothing else moves. Roles come from the authenticator
(`Principal.Roles`), so a role policy needs no store of its own. A policy
that assigns roles to subjects inside Cartograph would be a small kind of its
own, and belongs in `TAXONOMY.md` first.

## A PDF printer

Implement `printer.Printer` (`Print(ctx, html) ([]byte, error)`), return
`printer.ErrUnavailable` when the machine cannot print, and construct it
in `cmd/cartograph/serve.go` and `cmd/cartograph/printer.go`. A remote printing
service, or a Go PDF library, slots in without the API knowing.

## A bundle store

Implement `store.BundleStore`. `PutBundle` must be all-or-nothing and
return a location the state history can record. Object storage is the
obvious second adapter.

## Rules for every extension

- An adapter imports the core; the core never imports an adapter.
- An adapter has a conformance test or it is not done.
- No adapter writes to stdout; use `log/slog`.
- No organisation's words under `cartograph/` (`just words`).
- `just test` stays under ten seconds.
- Say in the pull request what you ran and what it printed.
