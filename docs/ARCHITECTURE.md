# Cartograph systems architecture

**Describes the tree as built, at engine 2.1.0. Where it describes a
port with one adapter, that is the state of things, not a promise. The
decisions behind it are recorded one per file in
[`adr/`](adr/README.md).**

Cartograph captures what an organisation has decided to do (goals, programmes,
projects, operations, KPIs, the data sources behind them) as declarative
manifests, checks them against a contract and a set of discipline rules,
and renders the result as documents other tools and people consume.
It does not manage delivery. Jira, or whatever the organisation runs,
does that; Cartograph hands a defined project over and keeps the record.

Three decisions shape everything below.

1. **Contract first.** `contract/openapi.yaml` and one JSON Schema per
   kind are the source of truth. The Go server types and the TypeScript
   client are generated from them and committed; CI fails on drift. No
   handwritten request or response type exists.
2. **The record is the committed text of each version, in whatever
   store the deployment chose.** The engine holds no file path and no
   syntax: a `ManifestStore` adapter keeps the text, a `Codec` adapter
   says what the text is written in. The reference adapter, the vault,
   makes that record a directory of files, one per manifest, plus
   `vault.yaml` saying which are live, so a vault can be read with
   `cat`, diffed with git, and edited in any editor while the server is
   running; its index (versions, references, summaries) is a cache that
   can be deleted and rebuilt. A database adapter keeps the same record
   as rows, and the engine cannot tell the difference. "Files are the
   truth" is the vault's promise, not the architecture's.
3. **One engine, many adapters.** Every rule lives in the Go engine, which
   talks to the world through interfaces (ports). Files, SQLite,
   Postgres, a browser that prints PDFs, an authenticating proxy: each
   is an adapter a deployment picks by configuration. The HTTP API and
   the command line are two front doors to the same engine; the web
   interface is a client of the API and its sync socket, and nothing
   more.

## 1. Context

```mermaid
flowchart LR
    author(["Definition author<br/>(whoever defines the work)"])
    reader(["Reader<br/>(executive, reviewer)"])
    operator(["Operator<br/>(runs the deployment)"])

    subgraph cartograph["Cartograph (one binary)"]
        direction TB
        spa["Web interface"]
        api["HTTP API /api/v1"]
        cli["Command line"]
        engine["Engine"]
        spa --> api --> engine
        cli --> engine
    end

    vault[("Vault<br/>directory of YAML")]
    index[("Index<br/>SQLite, rebuildable")]
    pg[("Postgres<br/>(optional, instead of the vault)")]
    browser["Headless Chromium<br/>(optional, prints PDF)"]
    proxy["Authenticating proxy<br/>(optional)"]
    delivery["Delivery tool<br/>(Jira, monday.com)"]
    git[("git / backups")]

    author --> proxy --> spa
    reader --> proxy
    operator --> cli
    engine <--> vault
    engine <--> index
    engine <-.-> pg
    engine --> browser
    engine -. "handoff bundle<br/>(HTML, JSON, PDF)" .-> delivery
    vault <-. "commit, diff, restore" .-> git
```

With a vault, the vault is the only thing that needs backing up, and
`git` is a fine way to do it. The index lives beside the vault under
`.cartograph/` and is ignored by version control. With a Postgres store
(`CARTOGRAPH_STORE`), the database holds everything instead, and its
own backups are the backup. The proxy and the browser are optional: a
single operator on a laptop runs `cartograph serve .` and gets everything but
PDF printing and identity.

## 2. Containers

A deployment is one process, or several identical replicas of it
against one Postgres database. The pieces inside a process are
separable, and the command line proves it: every subcommand opens the
same engine over the same adapters without an HTTP server in the way.

```mermaid
flowchart TB
    subgraph process["cartograph serve (one Go process, one port)"]
        direction LR
        subgraph driving["Driving adapters"]
            http["net/http mux<br/>/healthz /readyz<br/>/api/v1/* /*"]
            authmw["auth middleware"]
            apigen["Generated strict server<br/>(oapi-codegen)"]
            apipkg["internal/api<br/>request to one engine call"]
            syncsrv["internal/syncserver<br/>/api/v1/sync, WebSocket<br/>automerge-repo protocol v1"]
            spaemb["internal/spa<br/>embedded web build"]
        end
        core["internal/engine<br/>+ internal/kinds<br/>+ internal/contract<br/>+ engine.Shared (shared drafts)"]
        subgraph driven["Driven adapters"]
            vaultad["store/vault"]
            sqlitead["store/sqlite"]
            pgad["store/postgres"]
            memad["store/memory"]
            amad["crdt/automerge<br/>(WebAssembly on wazero)"]
            fanad["fanout: memory | postgres"]
            chrom["printer/chromium"]
            none["printer.None"]
            authn["auth: none | proxy"]
        end
        http --> authmw --> apigen --> apipkg --> core
        authmw --> syncsrv --> core
        syncsrv --> fanad
        http --> spaemb
        core --> vaultad --> sqlitead
        core --> sqlitead
        core --> pgad
        core --> amad
        core --> fanad
        core --> chrom
    end
    cliproc["cartograph validate | import | export | diff | list | snapshot |<br/>check | render | handoff | apply | exclude | recover"] --> core
    fs[("vault directory")] --- vaultad
    db[("index.sqlite")] --- sqlitead
    pg[("Postgres (optional)<br/>store and LISTEN/NOTIFY")] --- pgad
    pg --- fanad
```

The web interface is built with Vite in `cartograph-ui` and embedded
into the binary with `go:embed`, so a deployment ships one file. The
engine embeds a released build, not a source tree: `UI_VERSION` names
the release and `UI_SHA256` pins its bytes, and `just ui` and the flake
both unpack exactly that into `internal/spa/dist` before compiling
(ADR 0002). The interface talks only to `/api/v1`, through one HTTP
adapter generated from the same OpenAPI document the server is
generated from, which is how the two halves stay in step. Live editing
and presence go over the sync socket at `/api/v1/sync`, behind the same
authentication and authorization, with the stock automerge-repo
WebSocket adapter (ADR 0007).

## 3. The hexagon

The engine depends on nothing but ports. This is the diagram to keep in
mind when adding anything: if a new feature needs the engine to know
about a file path, a SQL dialect or an HTTP header, it needs a port.

```mermaid
flowchart LR
    subgraph drivers["Driving side (who calls the engine)"]
        direction TB
        d0["pkg/client.Client<br/>the port every interface uses"]
        d1["HTTP API<br/>internal/api (the remote transport's far end)"]
        d4["Sync socket<br/>internal/syncserver<br/>automerge-repo protocol v1"]
        d2["Command line<br/>cmd/cartograph"]
        d3["Tests<br/>conformance, engine, api"]
        d0 -. inproc .-> E
        d0 -. remote: HTTP over TCP, UNIX socket, SSH .-> d1
    end

    subgraph core["Core"]
        direction TB
        E["Engine<br/>validate, commit, version, diff,<br/>reference index, checks, goal tree,<br/>project state, apply gate, handoff,<br/>shared drafts (engine.Shared)"]
        K["Kinds registry<br/>18 kinds: schema file + rules func"]
        C["Contract<br/>JSON Schema and Flow per kind, embedded"]
        E --> K --> C
    end

    subgraph ports["Driven ports (interfaces the engine owns)"]
        direction TB
        p1["store.ManifestStore<br/>versions, working copies,<br/>references, summaries, exclusions"]
        p2["store.OperationalStore<br/>project state history"]
        p3["store.StateStore<br/>apply gate: include list"]
        p4["store.BundleStore<br/>handoff bundles"]
        p5["printer.Printer<br/>HTML to PDF"]
        p6["auth.Authenticator<br/>request to principal"]
        p7["auth.Authorizer<br/>principal + action to yes/no"]
        p8["store.VaultIndex<br/>what the vault keeps beside files"]
        p9["codec.Codec<br/>manifest text to document and back"]
        p10["store.DocStore<br/>shared drafts: snapshot + chunks"]
        p11["crdt.Engine, crdt.Doc<br/>the CRDT under shared drafts"]
        p12["fanout.Bus<br/>hints between replicas, presence"]
        p13["engine.Bus<br/>events: version, state"]
    end

    subgraph adapters["Adapters (one deployment picks each)"]
        direction TB
        a1["vault (files + index)"]
        a2["sqlite"]
        a3["memory"]
        a9["postgres"]
        a4["chromium"]
        a5["printer.None"]
        a6["auth none"]
        a7["auth proxy"]
        a10["oidc (not built)"]
        a8["auth AllowAll"]
        a11["auth roles"]
        a12["codec yaml"]
        a13["codec json"]
        a14["automerge<br/>(WebAssembly on wazero)"]
        a15["fanout memory"]
        a16["fanout postgres<br/>(LISTEN/NOTIFY)"]
        a17["NATS etc.:<br/>a distribution's adapter"]
        a18["bus memory"]
    end

    d1 --> E
    d4 --> E
    d2 --> E
    d3 --> E
    E --> p1 & p2 & p3 & p4 & p5 & p9 & p10 & p11 & p12 & p13
    d1 --> p6 & p7
    d4 --> p7 & p12
    a1 --> p8

    p1 --- a1 & a2 & a3 & a9
    p2 --- a2 & a3 & a9
    p3 --- a1
    p4 --- a1 & a3 & a9
    p5 --- a4 & a5
    p6 --- a6 & a7 & a10
    p7 --- a8 & a11
    p8 --- a2
    p9 --- a12 & a13
    p10 --- a2 & a3 & a9
    p11 --- a14
    p12 --- a15 & a16
    p12 -.- a17
    p13 --- a18
```

A few things the diagram cannot say.

The ports are small and typed on plain values (`[]byte` of YAML, strings,
times). An adapter never sees an engine type, so it can live in another
module tomorrow without the engine noticing.

`StateStore` and `BundleStore` are optional. The engine attaches them when
the manifest store happens to implement them (the vault does) or when the
composition root passes them as options. A store without an apply gate,
such as a database where every row is live, answers `ErrNoState`, and the
API turns that into an empty list or a refusal. That is the whole
difference between "define in YAML" and "define in a database" as far as
the engine is concerned.

The identity ports sit on the driving side on purpose. The engine takes an
actor string and records it on every version and state transition; it
does not know what a session is. Two middlewares wrap the whole API, in
order: `auth.Middleware` turns the request into a `Principal` (or answers
401), and `auth.Authorize` classifies the request into an `Action` (read
or write, and the manifest it names) and puts it to the `Authorizer` (or
answers 403). The handlers never see a credential and never decide
policy; they take the actor off the context. This keeps identity out of
the core and lets a deployment choose "no identity on a trusted network",
"trust the proxy's header, readers and editors by group", or (later)
"verify an OIDC token" without the engine changing.

`codec.Codec` is the one port the engine owns for its own input: manifest
text in, document out, and canonical text back for a save. The engine
parses no syntax itself. YAML is what the vaults are written in; the JSON
adapter proves the seam and is the natural syntax for a database-backed
deployment. A store carries the text its codec produced, byte for byte,
so versions and diffs are over what the person wrote; changing the codec
of an existing vault is therefore a migration (`cartograph export --codec
json`, then serve the export with `CARTOGRAPH_CODEC=json`), never a flag flip
on live files.

`VaultIndex` is the vault adapter's own port, one layer down: the vault is
files plus a rebuildable cache, and that cache is SQLite today and may be
Postgres tomorrow. The engine does not see this port at all, and the
vault does not choose its adapter: the composition root passes an opener
(`vault.Options.OpenIndex`), so the vault and SQLite adapters never
import each other (ADR 0004).

`pkg/client.Client` is the port on the other side: what an interface
(web, terminal, a bot) uses, as a Go interface and never as a wire. In
one process it is the engine (`inproc`); across a process boundary it is
a transport adapter (`remote`: the HTTP contract over TCP or a UNIX
socket, which SSH forwards). The HTTP API in `internal/api` is the far
end of that transport, not something an interface talks to directly.
Both transports are proven by the same conformance suite
(`pkg/uiconformance`), which is how a new transport is admitted.
`UI_CONTRACT.md` and `MULTIPLAYER.md` are the design of record for the
interfaces and for shared editing.

Shared editing has four engine-side pieces (ADR 0007, ADR 0008).
`engine.Shared` is the shared-draft service: one Automerge document per
manifest, reached through the `crdt.Engine` port, whose one adapter
runs Automerge compiled to WebAssembly on wazero, so the binary stays
free of cgo. `store.DocStore` keeps each document as a snapshot plus
the chunks appended since, in memory, in SQLite (the vault's index) or
in Postgres. `fanout.Bus` carries "this document changed" and presence
from one replica to the others, as hints that correctness never
depends on. The sync socket, `internal/syncserver`, is a driving
adapter beside the HTTP API: it speaks the automerge-repo network
protocol to interfaces and turns each message into a call on
`engine.Shared`. Live edits and presence travel there, not on the
client port, which only names a manifest's document
(`SharedDocument`). `engine.Bus` is what is left of the old event
bus: versions saved and states changed, in one process.

### 3.1 Clean architecture, and the test that keeps it

The hexagon above is clean architecture drawn from the side. Read as
rings, from the centre out:

| Ring | Here | May depend on |
|---|---|---|
| Entities | `internal/kinds`, `kinds/<kind>` and `kinds/kit` (what a kind is, its rules), `internal/contract` (the schemas and flows), `internal/sentence` | nothing in the module but each other; no driver |
| Use cases | `internal/engine` | the ports and the entities |
| Ports | `internal/store` (with `store.DocStore`), `internal/codec`, `internal/printer`, `internal/crdt`, `internal/fanout`, `engine.Bus` (driven); `pkg/client`, `pkg/uiconformance`, `internal/auth` (driving) | the entities; `internal/auth` also `net/http`, because the identity ports are request middleware |
| Interface adapters | driving: `internal/api`, `internal/syncserver`, `internal/render`, `internal/spa`, `pkg/client/inproc`, `pkg/client/remote`, `uiconformance/clientdriver`; driven: `store/vault`, `store/sqlite`, `store/memory`, `store/postgres`, `crdt/automerge`, `fanout/memory`, `fanout/postgres`, `codec/yaml`, `codec/json`, `printer/chromium`, `auth/proxy`, `auth/roles`, the conformance suites, and the helpers the adapters share (`yamlfmt`, `store/manifestmeta`) | the rings inside them, never another adapter of their side |
| Frameworks and drivers | `net/http`, `database/sql`, `os/exec`, `modernc.org/sqlite`, `pgx` (only in the Postgres adapters), `wazero` (only in the Automerge adapter), `fsnotify`, `yaml.v3`, Chromium, the generated server (`api/gen`) | used only by adapters |
| Composition root | `cmd/cartograph`, `internal/config` | everything; the one place an adapter is chosen |

The dependency rule, dependencies point inward, is a test
(`internal/arch`, run by `just arch` and in the gate). Every package in
the module has a rule there, and a new package without one fails the
build, so nothing joins the tree without somebody saying which ring it
belongs to. Each rule lists what that package must never depend on,
transitively: module packages by subtree, third-party drivers by
module path, and standard-library drivers (`net/http`, `database/sql`,
`os/exec`) by name. The engine may not import an adapter, a driver, or
a syntax library; a port may not import the engine; an adapter may not
import another adapter, because composing adapters is the root's job;
the entities and the ports `internal/codec`, `internal/printer`,
`internal/crdt` and `internal/fanout` may not import anything else in
the module; `cmd` is the only
package that knows everything, which is what makes it the one place an
adapter is chosen. A change that needs a new edge gets a new port, not
an exception (ADR 0003).

### 3.2 Packages

| Package | Role | Depends on |
|---|---|---|
| `internal/contract` | The JSON Schema and flow files, embedded; the schema set | nothing |
| `internal/sentence` | Composes the sentences a manifest stores in parts, the same way everywhere they are shown | nothing |
| `internal/kinds`, `internal/kinds/<kind>` | The registry of kinds and each kind's rules beyond its schema | `kinds/kit` |
| `internal/kinds/kit` | The small types rules need (`Problem`, `Lookup`) so kind packages never import the engine | nothing |
| `internal/engine` | The core: validation, commits, versions, diffs, references, checks, state, apply gate, handoff, the shared drafts (`Shared`, and `Shape`, which maps a kind's schema onto the document), the event bus | `store`, `codec`, `crdt`, `fanout`, `kinds`, `kinds/kit`, `contract`, `sentence`; a JSON Schema validator |
| `internal/store` | The port definitions (`ManifestStore`, `OperationalStore`, `StateStore`, `BundleStore`, `VaultIndex`, `DocStore`) and the record shapes | nothing |
| `internal/store/vault` | Files are the truth; journalled writes; watcher; apply gate; bundles. Its index is a `store.VaultIndex` the root opens for it | `store`, `store/manifestmeta`, `yamlfmt`, `fsnotify`, `yaml.v3` |
| `internal/store/sqlite` | SQLite adapter: manifest store, operational store, journal, document store, vault index | `store`, `store/manifestmeta`, `modernc.org/sqlite` |
| `internal/store/postgres` | Postgres adapter for a stateless deployment: manifest store, operational store, bundle store, document store; migrations applied on open | `store`, `store/manifestmeta`, `pgx` |
| `internal/store/memory` | In-memory adapter for tests and the conformance suite: manifest store, operational store, bundle store, document store | `store`, `store/manifestmeta` |
| `internal/store/manifestmeta` | Reads the envelope (kind, id, name, labels) from manifest text for the store adapters | `yaml.v3` |
| `internal/store/conformance` | The one suite every adapter of a store port must pass, `RunDocStore` included | `store` |
| `internal/crdt`, `crdt/automerge`, `crdt/conformance` | The CRDT port (`Engine`, `Doc`, `SyncState`), its Automerge adapter (the module built from `crdt/`, run on wazero), and the suite every CRDT adapter passes | nothing / `crdt`, `wazero` (automerge) |
| `internal/fanout`, `fanout/memory`, `fanout/postgres`, `fanout/conformance` | The port that carries a hint from one replica to every other, its in-process and `LISTEN/NOTIFY` adapters, and the suite both pass | nothing / `fanout`, `pgx` (postgres) |
| `internal/codec`, `codec/yaml`, `codec/json`, `codec/conformance` | Manifest syntax port, its two adapters, and the suite both pass | nothing / `codec`, `yamlfmt`, `yaml.v3` |
| `internal/yamlfmt` | Canonical YAML output, shared by the YAML codec and the vault | `yaml.v3` |
| `pkg/client`, `client/inproc`, `client/remote` | The port every interface uses, and its two transports | nothing; `engine` and `auth` (inproc), `net/http` (remote) |
| `pkg/uiconformance`, `uiconformance/clientdriver` | The interface suite as data, its Go runner, and the reference driver | `client` |
| `internal/printer`, `printer/chromium` | PDF port and the headless-browser adapter | nothing / `printer`, `os/exec` |
| `internal/auth`, `auth/proxy`, `auth/roles` | Identity and policy ports, context plumbing, the two middlewares; the proxy-header authenticator and the role policy | `net/http` / `auth` |
| `internal/render` | HTML documents (charters) from engine data; reads through the engine and decodes through its codec | `engine` |
| `internal/api`, `api/gen` | The generated strict server and the thin handlers | `engine`, `store`, `codec`, `auth`, `printer`, `render` |
| `internal/syncserver` | The sync socket: the automerge-repo network protocol, version 1, over a WebSocket, turned into calls on `engine.Shared` | `engine`, `crdt`, `fanout`, `auth`; `coder/websocket`, a CBOR codec, `net/http` |
| `internal/spa` | The embedded web build, a pinned `cartograph-ui` release | `net/http` |
| `internal/config` | Every setting, from environment then flags | nothing |
| `internal/arch` | The dependency rule as a test: a rule per package | nothing (it reads `go list`) |
| `cmd/cartograph` | The composition root and every subcommand | everything above |

Dependency direction is inward: adapters and drivers import the core,
never the reverse. `render` is the one package that imports the engine
and is also called by the API; it reads, it never writes, and it is the
natural seam for a document-rendering port if a second document format
ever appears.

## 4. Data

### 4.1 Manifests and kinds

Every object is a manifest: an envelope (`apiVersion`, `kind`,
`metadata`, `spec`) whose `spec` is validated by its kind's JSON Schema.
A reference to another manifest is a field the schema marks with
`x-cartograph-ref: <Kind>` (or `"*"` for any kind). The engine walks each
kind's schema once at start-up to learn where its references live, so
adding a reference is a schema change, never an engine change.

```mermaid
erDiagram
    MANIFEST {
        string kind
        string id "generated, never derived from the name"
        string name "unique under its parent, not globally"
        map labels
        yaml spec "validated by the kind's schema"
    }
    VERSION {
        int number "1, 2, 3; immutable once written"
        bytes yaml "the exact text committed"
        string actor
        string reason
        time on
    }
    REFERENCE {
        string path "JSON pointer in the referrer"
        string toKind
        string toID
    }
    WORKING_COPY {
        bytes yaml "autosaved, never versioned"
    }
    STATE_ENTRY {
        string projectID
        string state "draft, defined, handed off, cancelled"
        int snapshot
        string bundle "where the handoff bundle went"
    }
    EXCLUSION {
        string reason
        string operator
        time on
    }
    MANIFEST ||--o{ VERSION : "has"
    MANIFEST ||--o| WORKING_COPY : "may have"
    MANIFEST ||--o{ REFERENCE : "points to"
    MANIFEST ||--o{ STATE_ENTRY : "a Project has"
    MANIFEST ||--o| EXCLUSION : "may be"
```

The 18 kinds, in registry order: Team, ReportingCycle, DataSource,
BeneficiaryGroup, Resource, FundingSource, Segment, Gap, Assumption,
Goal, Unit, KPI, KPIReadings, Programme, Operation, Project,
StakeholderMap, Settings. There is no Person kind and no Portfolio kind;
`docs/TAXONOMY.md` says why,
and that file is the place to argue before adding a kind.

### 4.2 The vault on disk (the reference store)

```
my-vault/
  vault.yaml                 the state manifest: which refs are live
  Goal/raise-produce-quality.yaml
  Project/quality-check-rollout.yaml
  <Kind>/<id>.yaml           one manifest per file, the exact bytes committed
  .cartograph/                    ignored by git; delete it and it is rebuilt
    index.sqlite             versions, references, summaries, journal, file hashes
    staging/<Kind>/<id>.yaml working copies: a draft is not a vault file until saved
    handoff/<project>/v<n>/  the charter bundle of each handoff
```

`vault.yaml` is the apply gate. A file can be dropped into `Goal/` by
hand, by a generator (a generator does exactly this), or by `git pull`, and it is invisible until somebody applies it.
The Snapshots screen and `cartograph apply` do that; `cartograph exclude` takes a
manifest out of the live state and keeps the file; `cartograph recover` puts
it back. Deleting a manifest from the interface deletes its file: the
index is a cache and must never hold a fact the files do not.

### 4.3 The vault's index is a cache

On open, the vault hashes every live file, compares against the hashes
the index recorded, and appends a new version for any file that is new or
changed. Then it rebuilds the reference index by walking every schema. A
watcher does the same for files that change while the server runs
(debounced 200 ms). Deleting `.cartograph/` and reopening yields an equivalent
index; version numbers may restart at 1, and that is the one thing a
rebuild loses, which is why `cartograph snapshot` exists for the versions that
matter.

## 5. Flows

### 5.1 A validated write

Every write goes through one path. The API has one handler for `PUT
/manifests/{kind}/{id}`; the command line's `import` calls the same engine
method per file.

```mermaid
sequenceDiagram
    participant UI as Web interface
    participant MW as auth middleware
    participant API as internal/api
    participant E as Engine
    participant S as ManifestStore (vault)
    participant J as Apply journal
    participant FS as files
    participant IX as index

    UI->>MW: PUT /api/v1/manifests/Goal/g1 (YAML, reason)
    MW->>API: request + Principal on context
    API->>E: Commit(kind, id, yaml, actor, reason)
    E->>E: parse YAML, validate against schema
    E->>S: look up every x-cartograph-ref target
    E->>E: run the kind's rules (kit.RulesFunc)
    alt problems
        E-->>API: ValidationError{problems}
        API-->>UI: 422 {problems:[{path,message}]}
    else clean
        E->>S: WithinTransaction
        S->>J: PutUnit(applied=false, files...)
        S->>FS: write <Kind>/<id>.yaml (temp, fsync, rename)
        S->>FS: write vault.yaml last (if the id is new)
        S->>IX: PutVersion(n+1), IndexReferences
        S->>J: MarkApplied
        E-->>API: Version{n+1}
        API-->>UI: 200 Version
    end
```

Two properties fall out of this. A crash at any point converges on reopen,
because the journal replays units not marked applied (and a unit's files
are idempotent by hash). And the version number is the concurrency
control: an adapter refuses a `PutVersion` whose number is not exactly
current plus one, so two writers cannot both win.

### 5.2 Open, rehydrate, apply

```mermaid
sequenceDiagram
    participant CLI as cartograph serve / apply
    participant V as vault adapter
    participant IX as VaultIndex
    participant E as Engine

    CLI->>V: New(dir, Options{Watch, OpenIndex})
    V->>IX: OpenIndex(.cartograph/) (SQLite, as cmd chose)
    V->>V: replay unapplied journal units
    V->>V: read vault.yaml (generate it if absent)
    loop every included ref
        V->>V: hash the file
        V->>IX: GetFileHash
        alt new or changed
            V->>IX: PutVersion, PutFileHash
        end
    end
    V->>V: load staged working copies
    CLI->>E: New(vault, ops)  (engine sees StateStore + BundleStore)
    CLI->>E: Reindex() rebuild every reference
    Note over CLI,E: later: POST /vault/apply {refs}
    CLI->>E: Apply(refs)
    E->>V: Apply: stat each file, rewrite vault.yaml once
    E->>V: Rehydrate
    E->>E: Reindex
```

Apply, recover and the watcher all end in rehydrate plus reindex, once per
batch. The reference index is rebuilt whenever the state is loaded, not
only on writes, because files edited while nothing was running carry
references the index has never seen.

### 5.3 Handoff

A handoff is the moment a definition leaves Cartograph for the delivery tool.
The engine owns the gate; the API orchestrates rendering around it.

```mermaid
sequenceDiagram
    participant UI as Web interface
    participant API as internal/api
    participant E as Engine
    participant R as render
    participant P as Printer
    participant B as BundleStore

    UI->>API: POST /manifests/Project/p1/state {to: "handed off"}
    API->>E: HandoffGate(p1)
    alt blocking checks, no version, or already handed off at this version
        E-->>API: ValidationError
        API-->>UI: 422 with the checks that block
    else
        E-->>API: snapshot n
        API->>R: Charter(p1, n) (HTML + JSON)
        API->>P: Print(html)
        P-->>API: pdf, or ErrUnavailable (then no PDF)
        API->>E: Handoff(p1, actor, {html, json, pdf})
        E->>E: HandoffGate again (the rule lives once)
        E->>B: PutBundle(p1, n, files) atomic
        E->>E: record state entry {handed off, snapshot n, bundle location}
        E-->>API: HandoffResult
        API-->>UI: 200 ProjectState
    end
```

Checks never block a save. They block a handoff, and only the ones whose
state is `block`; the rest are advice with a link to the step that fixes
them.

### 5.4 A request's identity

```mermaid
sequenceDiagram
    participant C as Client
    participant X as Proxy (optional)
    participant MW as auth.Middleware
    participant A as Authenticator
    participant H as Handler
    participant E as Engine

    C->>X: request with session
    X->>MW: request + X-Forwarded-User
    MW->>A: Authenticate(r)
    alt none configured
        A-->>MW: Anonymous
    else proxy configured, header present
        A-->>MW: Principal{subject, roles}
    else proxy configured, header missing
        A-->>MW: ErrUnauthenticated
        MW-->>C: 401
    end
    MW->>MW: Authorize(principal, ActionFor(request))
    alt policy refuses
        MW-->>C: 403 {problems:[{message}]}
    end
    MW->>H: request with Principal on context
    H->>E: Commit(..., actor = principal.Actor(settings.operator), ...)
```

An anonymous principal records the vault's `spec.operator` as the actor,
which is what a single-operator vault has always done. An authenticated
one records its subject. The policy is one call for the whole API, before
any handler: `CARTOGRAPH_AUTHZ=roles` with `CARTOGRAPH_READ_ROLES` and
`CARTOGRAPH_WRITE_ROLES` is the shipped one, and a policy per kind or per
manifest is another adapter over the same `Action`. Every operation in
the contract declares 401 and 403 for this reason. Nothing in the engine
changed to make any of it true.

## 6. Deployment

Cartograph is built to the twelve-factor shape so the same binary runs on a
laptop, in a container, and behind an organisation's proxy.

```mermaid
flowchart LR
    subgraph laptop["Laptop"]
        l1["cartograph serve ./vault"]
        l2[("./vault + .cartograph/")]
        l1 --- l2
    end
    subgraph container["Container (distroless, nonroot)"]
        c1["ENV CARTOGRAPH_VAULT=/vault<br/>CARTOGRAPH_LOG_FORMAT=json<br/>CARTOGRAPH_AUTH=proxy"]
        c2[("/vault volume")]
        c3["/healthz /readyz"]
        c1 --- c2
    end
    subgraph replicas["Replicas (any number, scale to zero)"]
        r1["ENV CARTOGRAPH_STORE=postgres://...<br/>CARTOGRAPH_AUTH=proxy"]
        r2[("Postgres<br/>store and fan-out")]
        r1 --- r2
    end
    subgraph org["Organisation"]
        p["oauth2-proxy / Pomerium / ingress OIDC"]
        idp["Identity provider"]
        p --- idp
    end
    users(["People"]) --> p --> c1
    p --> r1
    backup[("git remote / snapshot")] <-. "the vault is the backup" .-> c2
```

| Factor | What Cartograph does |
|---|---|
| Codebase | One repository, one `cartograph` binary, many deployments by environment |
| Dependencies | `go.mod`; `flake.nix` pins the toolchain; the embedded web build is a `cartograph-ui` release pinned by `UI_VERSION` and `UI_SHA256`; the image builds from source |
| Config | `internal/config`: every setting is a `CARTOGRAPH_*` variable (address, vault or store, fan-out, codec, authenticator, policy, roles, log format, printer), a flag overrides it. See `DEPLOYMENT.md` |
| Backing services | The store (a vault directory and its index, or a Postgres database named by `CARTOGRAPH_STORE`), the fan-out (`CARTOGRAPH_FANOUT`: in-process, or Postgres `LISTEN/NOTIFY`), the printer and the identity provider are attached resources chosen by configuration |
| Build, release, run | `just ci` builds; the image or `nix build .#cartograph` is the release; `cartograph serve` is the run. Nothing is edited at run time |
| Processes | The engine keeps nothing between requests except through a port. With a vault, state is on a volume, and two replicas writing to one vault are not arbitrated, so run one writer. With `CARTOGRAPH_STORE=postgres://...` the processes are stateless: versions, working copies, shared drafts and bundles are in the database, fan-out goes over `LISTEN/NOTIFY`, and any replica serves any request and scales to zero (`SERVERLESS.md`, ADR 0008). What a replica holds besides (open documents, sync states) is a cache |
| Port binding | `CARTOGRAPH_ADDR`; a bare port works for platforms that hand out `PORT` |
| Concurrency | One process per vault, or as many as needed against one Postgres store; the engine is safe for concurrent requests within a process |
| Disposability | SIGTERM drains in-flight requests (`CARTOGRAPH_SHUTDOWN_TIMEOUT`), `/readyz` goes 503 first; the journal (vault) or a transaction (Postgres) makes a kill at any point safe, and a client on the sync socket reconnects to any replica |
| Dev/prod parity | `just` recipes run inside the flake, and the image is built from the same flake; CI runs `just ci` and `nix build` on both architectures |
| Logs | `log/slog` to stdout, text or JSON; health probes are not logged |
| Admin processes | Every admin task is a subcommand of the same binary against the same adapters (`import`, `export`, `apply`, `snapshot`, `render`, `handoff`) |

## 7. Extension points, the Kubernetes way

Kubernetes is extended by replacing a component behind a stable interface
(CSI for storage, CNI for networking, an authentication webhook, a CRD
for a new kind, an admission webhook for a new rule), never by patching
the API server. Cartograph follows that pattern at a smaller scale. Each row
is one thing an organisation may want to change, the interface it swaps,
and what a new implementation must pass.

| Want | Kubernetes analogue | Cartograph port | Today | Add one by |
|---|---|---|---|---|
| A new kind of thing to capture | CRD | `kinds.Spec` (schema file + rules func) | 18 kinds | A schema under `contract/schemas`, a line in `kinds/registry.go`, words in `cartograph-ui's src/copy.ts`; the engine does not change |
| A rule beyond the schema | Admission webhook | `kit.RulesFunc` | per-kind packages | A function `(doc, RuleContext) []Problem`; cross-kind lookups through `kit.Lookup` |
| Manifests somewhere other than files | CSI driver | `store.ManifestStore` | vault, sqlite, memory, postgres | An adapter that passes `conformance.RunManifestStore`; wire it in `cmd/cartograph/store.go` |
| The vault's index in another database | etcd backend | `store.VaultIndex` | sqlite | An adapter that passes `conformance.RunVaultIndex`; pass its opener as `vault.Options.OpenIndex` in `cmd/cartograph/store.go` |
| Define projects in a database, not YAML | Different storage class | `store.ManifestStore` without `StateStore` | sqlite, postgres (`CARTOGRAPH_STORE`) | The API is the only write path; the apply gate simply reports `ErrNoState` |
| Write manifests in another syntax | Serialisation (JSON/protobuf at the API server) | `codec.Codec` | yaml, json | An adapter that passes `codec/conformance.Run`; select it by `CARTOGRAPH_CODEC`; migrate a vault with `cartograph export --codec` |
| Authentication | Authn webhook, OIDC | `auth.Authenticator` | none, proxy | An adapter that returns a `Principal`; select it by `CARTOGRAPH_AUTH` |
| User types and permissions | RBAC, authz webhook | `auth.Authorizer` | AllowAll, roles | An adapter over `(Principal, Action)`; enforced once for the whole API by `auth.Authorize`; select it by `CARTOGRAPH_AUTHZ` |
| PDF without Chromium | Container runtime (CRI) | `printer.Printer` | chromium, none | An adapter over `Print(ctx, html)` |
| Handoff bundles in object storage | Volume plugin | `store.BundleStore` | vault, memory, postgres | An adapter over `PutBundle`; the state entry records the location it returns |
| A new interface (terminal, native, bot) | kubectl, the dashboard: clients of the API | `pkg/client.Client` + the `Driver` protocol | web (on the port; flows still hand-coded), reference driver | Build on the client port, write a driver, pass `pkg/uiconformance` in your own CI (`UI_CONTRACT.md`) |
| Another transport (socket, SSH, broker) | API server transports | `pkg/client.Client` as an adapter | inproc, remote (HTTP over TCP or UNIX socket) | Implement the port, pass the same suite with the reference driver |
| Shared drafts kept somewhere else | Storage class | `store.DocStore` | memory, sqlite (the vault's index), postgres | An adapter that passes `conformance.RunDocStore`; wire it in `cmd/cartograph/store.go` |
| Fan-out across replicas | etcd watch | `fanout.Bus` | memory, postgres (`CARTOGRAPH_FANOUT`) | Add NATS, Redis streams or a cloud pub/sub: an adapter that passes `fanout/conformance.Run`, selected by a new `CARTOGRAPH_FANOUT` value (`EXTENDING.md`) |
| Another CRDT engine | etcd's storage engine | `crdt.Engine`, `crdt.Doc` | automerge | An adapter that passes `internal/crdt/conformance`; interfaces must then speak its sync protocol too (ADR 0007) |

The pattern for every row is the same three steps. Write the adapter in
its own package under the port's directory. Make it pass the port's
conformance suite, which is how the SQLite, memory and Postgres adapters
are proven interchangeable today. Pick it in `cmd/cartograph` from a configuration value.
No step touches the engine, the contract or the web interface.

What this is not: a plugin system that loads code at run time. An adapter
is compiled in and selected by configuration, as the Kubernetes in-tree
providers were. Out-of-process adapters (a gRPC `ManifestStore`, say) are
possible behind the same interfaces and are not planned until somebody
needs one.

## 8. Decisions and their reasons

The reasons in brief. Decisions made since the first release, with the
options weighed and what they cost, are in [`adr/`](adr/README.md).

**Why files, and not a database, as the truth.** Because the people who
own the content are not database administrators, and a directory of YAML
survives every tool choice. It can be reviewed in a pull request, restored
from any backup, generated by a script, and read without Cartograph running.
The cost is that concurrent writers to one directory need arbitration,
which the journal provides within one process and nothing provides across
processes. A deployment that needs more than one writer uses the
Postgres store instead, and gives up reading the record with `cat`.

**Why the engine knows no file path and no HTTP header.** So the same
rules run in every front door and in every test. The handoff gate is one
function; the API, the command line and the tests call it. An engine that
read `vault.yaml` itself would make a database-backed deployment
impossible without a fork.

**Why generated code is committed.** So a fresh checkout builds without
running generators, so a reviewer sees what a contract change does to the
Go and TypeScript types, and so CI can refuse drift with one `git diff`.

**Why one binary.** Deployment is copying a file. The web interface
cannot be out of step with its API because they ship together. The
command line is the API's twin, which is what makes scripts and the
interface agree. The interface's build is the one input a fresh
checkout does not carry: it is a pinned, checksummed `cartograph-ui`
release that `just ui` fetches (every recipe that compiles runs it), not
committed output (ADR 0002).

**Why identity is a middleware concern.** A version records an actor
string. Everything above that line (sessions, tokens, groups) varies by
organisation and must not leak into the rules. The proxy adapter exists
because every organisation already has an authenticating proxy or can run
one; an OIDC adapter is a contained addition.

**Why checks never block a save.** Half-finished work is the normal state
of a definition. A check that refused the save would push people back to
Word. Checks block the handoff, where incompleteness has a cost.

## 9. What is not built yet

In rough order of expected need.

- An OIDC `Authenticator`, for deployments without a proxy.
- A finer `Authorizer` (per kind, per manifest, per state transition)
  once an organisation needs one; the shipped roles policy is read or
  write for the whole API.
- The events endpoint of the HTTP transport, so `Subscribe` works over
  `remote` as it does in process.
- The terminal interface (`UI_CONTRACT.md`).
- Presence and live editing in a terminal interface: an automerge-repo
  peer on the sync socket, as the web interface has (`MULTIPLAYER.md`).
- A document-rendering port, if a second output format joins HTML, JSON
  and PDF.
- Out-of-process adapters, if an organisation needs to write one in
  another language.

Related reading: `DEPLOYMENT.md` (running it), `SERVERLESS.md`
(stateless operation), `EXTENDING.md` (adding to it),
`../README.md` (building it), `DESIGN_RULES.md` (how it behaves),
`TAXONOMY.md` (what the nouns mean), and `adr/` (each decision, with
what it cost).
