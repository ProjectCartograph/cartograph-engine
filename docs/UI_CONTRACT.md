# The interface contract

Design of record. Built so far: the client port with two transports,
the conformance suite as data with its reference driver passing over
both, and the flow contract with the Goal exemplar.

Cartograph has one engine and will have more than one interface: the web
interface in the reference build, a terminal interface built with Bubble
Tea, and whatever an organisation builds for itself. The interfaces
live in their own repository. A person at a terminal and a person in a
browser must be able to work on the same vault, go through the same
steps in the same order, be refused the same things at the same fields,
and end up with the same manifest. This page says what makes that true
and how it is tested, in any repository, in CI.

Three decisions:

1. An interface depends on a port, never on a wire. The engine
   publishes `pkg/client.Client`, a Go interface. In the same process it
   is the engine itself; across a UNIX socket, a TCP port or an SSH
   forward it is a transport adapter. A terminal interface runs inside
   the cartograph binary with no server at all, or against a remote
   one, with the same code. Nothing in an interface names an HTTP path.
2. Transports are adapters. HTTP is the first; a UNIX domain
   socket carries the same contract today
   (`CARTOGRAPH_ADDR=unix:///run/cartograph.sock`, `remote.Dial("unix://...")`),
   and SSH forwards it. Another transport implements `client.Client`
   and passes the same suite. Live editing and presence do not travel
   on the port. A manifest's shared draft is an Automerge document
   (ADR 0007); the port names it (`SharedDocument`), and the interface
   syncs it over the sync socket (`/api/v1/sync`, the automerge-repo
   network protocol, version 1) with the stock automerge-repo
   libraries.
3. The suite is data. Scenarios are JSON files an interface's own
   CI runs against its own driver, in Go through this package or in the
   interface's language through a runner of its own.

## 1. The five documents

Every interface is built from the same five documents, all reached
through the client port.

| Document | Client method | What an interface takes from it |
|---|---|---|
| JSON Schema per kind | `Schema(kind)` | Field types, enums, reference targets (`x-cartograph-ref`), titles and descriptions, which lists are keyed (`x-cartograph-list-key`) |
| Flow per kind | `Flow(kind)` | The steps in order, the fields each step asks for, how each is asked (`control`), the guide sentence, which checks a field answers |
| Checks | `Checks(kind, id)` | What is wrong or missing, as `{path, message, state, fix}`; `fix` names the flow step that holds the field; in process, conflict notes read from the shared draft arrive here too |
| Problems | the `Refused` error of a validating save | `{path, message}`, landed on the field at `path` |
| Settings | `Settings()` | The organisation's words for levels and kinds, examples per field |

A kind with no flow is a sheet: one step, every field of the schema in
the schema's order. The directory kinds (Team, DataSource, Resource,
ReportingCycle) are sheets.

The flow document is `contract/flows/<kind>.flow.json`, validated by
`flow.schema.json`, embedded in the binary beside the schemas, and
served by every transport. `goal.flow.json` is the exemplar: five steps
(Aim, Measures, Owner and horizon, Rationale, Also leads to) and a
review.

### 1.1 Fields are named by path

A field's identity is its JSON pointer in the manifest:
`/spec/objective`, `/spec/keyResults/{kr-2}/target`. The schema is
keyed by it, a check's `path` names it, a problem lands on it, the
conformance driver sets it, and presence names it as the focused field.
An element of a keyed list is addressed by its key in braces, never by
its index, because two people's lists do not share indices.

In a web interface this is a `data-cartograph-field` attribute on the
control; in a terminal interface it is the model's field id. Neither may
address a field any other way in anything a test touches.

### 1.2 Controls

A flow says how each field is asked for, from a short fixed vocabulary.
An interface implements each control once and reuses it everywhere.

| Control | Asks for | Terminal (Bubble Tea) |
|---|---|---|
| `text` | a short string | `textinput` |
| `sentence` | one line of prose; marks light as it takes shape; merges character by character between editors | `textinput` bound to an Automerge `Text`, marks in the status line |
| `choice` | one of the schema's enum, in the organisation's words | `list` |
| `reference` | one existing manifest of the `x-cartograph-ref` kind, with a way to add one | filterable `list` with an add row |
| `references` | several | multi-select `list` |
| `number` | a number | `textinput` with validation |
| `horizon` | years and optional months, or "same as above" | two inputs and a toggle |
| `list` | a keyed list of items, each with its own fields | a table with an item form |
| `toggle` | yes or no | checkbox |
| `readonly` | shown, never edited | text |

The words are not the interface's. A label is the schema's `title`, a
guide is the flow's `guide`, an option is the enum value through the
Settings words table. `DESIGN_RULES.md` ("text is the last resort",
"standard terms, not phrases", "speak like a person") binds the
documents, and so binds every interface at once.

### 1.3 Behaviours every interface has

Rules from `DESIGN_RULES.md` restated as what the interface does. Each
is a scenario in the suite, or will be.

- A working save is never refused for incompleteness. Save keeps the
  draft; only Save as version validates.
- A refused version shows every problem at its field, with the message
  the engine gave, and nowhere else.
- A check never blocks a save; checks are marks beside the field and a
  list with fix links; handing off refuses while a blocking check stands.
- Ids are never typed. The interface shows names; the id is the engine's.
- A reference control can create what it is missing without leaving the
  step.
- Another person's edit to the manifest you have open appears without a
  reload, attributed; two people typing in one sentence both keep their
  words; two people setting one field get a note, never a lost value
  (`MULTIPLAYER.md`).
- Leaving a flow and returning lands on the same step with the same
  draft.

## 2. The client port

```go
type Client interface {
    Kinds(ctx) ([]string, error)
    Schema(ctx, kind) ([]byte, error)
    Flow(ctx, kind) (Flow, bool, error)
    Settings(ctx) (Settings, error)

    List(ctx, kind, query) ([]Summary, error)
    Get(ctx, kind, id) (Manifest, error)
    Working(ctx, kind, id) (Manifest, error)
    SaveWorking(ctx, kind, id, doc) error
    SaveVersion(ctx, kind, id, doc, reason) (Version, error)   // *Refused carries the problems
    Validate(ctx, kind, doc) ([]Problem, error)
    Checks(ctx, kind, id) ([]Check, error)

    SharedDocument(ctx, kind, id) (string, error)   // the shared draft's Automerge document id
    Subscribe(ctx, kind, id) (<-chan Event, error)  // a version saved, a state changed

    Actor(ctx) (string, error)
    Close() error
}
```

Two adapters today, under `pkg/client`:

- `inproc`: the engine in the same process. What the terminal interface
  uses inside the cartograph binary, and what the suite runs first.
  `SharedDocument` answers `ErrUnsupported` when the engine runs
  without shared drafts.
- `remote`: the HTTP contract, over TCP or a UNIX socket (`Dial`).
  `SharedDocument` is `GET /manifests/{kind}/{id}/document`.
  `Subscribe` answers `ErrUnsupported` until the events endpoint lands;
  everything else is carried.

A transport is proven by running the conformance suite over it with the
reference driver, which is what `clientdriver_test.go` does for
both. The suite therefore proves two things at once: that an interface
behaves, and that a transport carries behaviour unchanged.

The web interface, in TypeScript, implements the same port as a TS
interface with one HTTP adapter behind it, generated from the same
OpenAPI document; its code depends on the interface, never on a path.

## 3. Conformance

`pkg/uiconformance` is the suite. It is public so an interface in its
own repository imports it and runs it in its own CI.

The `Driver` protocol is what an interface implements to be driven:
`Open`, `Step`, `Set` (by field path), `Act` (save, saveVersion, discard,
next, back, apply), `Problems`, `Where`, `Close`. The suite reads the
vault back through a `client.Client` and asserts on two things only:
what the vault holds and which problems the interface showed at which
fields. It never asserts on a widget, a pixel or a key binding.

Scenarios are JSON under `pkg/uiconformance/scenarios`, one file each,
named after the rule they hold:

```json
{ "key": "problem-lands-on-field",
  "rule": "A check says where to fix it: problems land on the field by path",
  "steps": [
    { "open": { "Kind": "DataSource" } },
    { "set": { "Field": "/spec/team", "Value": "no-such-team" } },
    { "act": { "Action": "saveVersion", "Reason": "try" } },
    { "expect": { "problem": { "field": "/spec/team", "contains": "no-such-team" } } } ] }
```

Steps are `open`, `step`, `set`, `act` and `expect` (`noProblems`,
`problem`, `where`, `current`, `working`, `idIsIdentifier`). The Go
runner (`uiconformance.Run`) executes them; a runner in another language
reads the same files. Four scenarios exist, and every rule in 1.3
becomes one. Shared editing is tested where it lives: the CRDT
conformance suite and the multi-replica test in the engine, and two
automerge-repo peers in the web interface's tests (`MULTIPLAYER.md`
section 10).

Drivers:

- `clientdriver` (built): the reference, over any `client.Client`.
- `tuidriver` (planned): drives the Bubble Tea program in-process with
  `teatest`, fields by model id.
- `webdriver` (planned, in the interface repository): drives the served
  web interface headlessly over the Chrome DevTools Protocol, controls by
  `data-cartograph-field`, steps by key, actions by accessible name.

In CI, the engine repository already runs the reference driver in the
ten-second gate. An interface repository runs its driver against a copy
of the example in its own job, with the engine from the cartograph
binary (in-process for the terminal, served over a socket for the web).
A new interface is a new driver and one line in a job.

## 4. What this asks of the web interface

The web interface works through the port. `src/client/port.ts` is a
TypeScript `Client` typed from the generated contract, and
`src/client/http.ts` is the one HTTP adapter behind it. Components get
the client from a provider, tests hand them a fake, and `just wire`
fails the build on a path, a `fetch` or `openapi-fetch` anywhere else
(ADR 0006).

The shared draft is the stock `@automerge/automerge-repo` behind that
port (ADR 0007). One repo per tab syncs over `/api/v1/sync` and keeps
documents in IndexedDB, so edits made offline survive a reload. Each
field change is one Automerge change at that field's path; a field the
document holds as text is spliced with `updateText`, so two people
typing in it both keep their words. Presence (who is here, their
focused field, caret and pointer) travels as ephemeral messages in the
shape of `presence.schema.json`. Every control that edits a field
carries `data-cartograph-field` with its JSON pointer, and the main
regions carry `data-cartograph-region`, which is what a pointer is
anchored to. `just wire` also fails on an `@automerge/` import outside
the client adapter.

Two things are still to do: rendering flows from `Flow(kind)` instead
of the hand-coded `definition/outline.ts` and `GoalSteps.tsx`, and the
web driver for the conformance suite. Nothing visible changes.

## 5. Repository split

The interface repository imports the engine by its module path,
`github.com/ProjectCartograph/cartograph-engine/v2`, and depends on
`pkg/client` and `pkg/uiconformance` from it. Only `pkg/` is public;
`internal/` stays the engine's own. The HTTP contract and the schemas
reach it as a synced copy (`cartograph-ui/contract`).
