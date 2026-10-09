# 0034. What people do is traced by its shape

**Status:** Accepted. The people's counterpart of
[0028](0028-agents-calls-are-traced.md); follows from
[0017](0017-one-bar-for-people-and-agents.md).

## Context

Agents' calls are traced (0028) and every change agents use is judged by
DMAIC from the trace (`docs/EVALUATING.md`). Nothing recorded what
people do in an interface, so a change to a flow, a step or a screen
was judged by how it looked. The design rules hold claims nobody
measured: every press answered within 100 ms, a wait over a second shown
as one, a save that changes only what was changed. One bar for people
and agents (0017) asks for one method too.

Some of a person's work only the server sees: a version refused and the
fields it named, a change set rolled in or closed. Some only the
interface sees: a step left backwards, a press on nothing, a picker
opened and closed with nothing chosen, how long a press took to answer.
And Cartograph holds what an organisation decided: a trace must never
become a second copy of it.

## Decision

**A domain of its own, behind two ports.** `internal/activity` is the
domain: an act by its shape (the name, the source, the session, the
person, the surface as its route pattern, the kind, the record's id,
the step, the field's JSON pointer, a target and an outcome from short
lists, a duration, whether a wait showed itself, the fields a refusal
named), the tasks rebuilt from acts, their opportunities from the
contract's flows, the analysis, the charts and the path budget. It reads
no file and opens no connection. An interface's act is made only by
`NewInterfaceAct`, which refuses one that could carry a value, so an
invalid act is never passed on. Its names follow OpenTelemetry's general
attributes where there is one (`event.name`, `session.id`,
`service.version`), Cartograph's own under `cartograph.*`. It is apart
from the agents' trace because the two are read differently: a call is
an opportunity, a person's opportunities come from the flow they walk.

The ports are `Recorder`, which appends and never fails the act it
records, and `Reader`, which reads acts back for a window and a person,
in the order they happened. The analysis reads only through `Reader`,
so it is the same wherever the trace is kept. The adapters are `jsonl`
(a file), `postgres` (a table), and `memory` (tests), and
`activity/conformance` holds each to the same promises; another place to
keep a trace is another adapter that passes it. `cmd` alone chooses one.

**The server records what it decides, for any interface.** The engine
records, through `engine.WithActivity`, a draft kept or thrown away, a
version saved or refused, a check left open, a change set opened, sent,
rolled in, closed or reopened. Only on a context a driving adapter marks
as a person's own request (`engine.ByPerson`: the HTTP API, the
in-process client), so the engine's own work and an agent's are left out.

**The interface sends what only it sees.** `POST /events` takes a batch
of acts from a fixed list of names. Every field is a token, a route
pattern or a JSON pointer whose list items are keys: no sentence, name
or figure fits, and a batch with one act that does not is refused whole.
The person is who the request runs as, never what the batch says.

**Off unless asked, and stateless when on.** `CARTOGRAPH_UI_TRACE` is
`off`, a `postgres://` URL whose table every replica appends to, or a
file the `jsonl` adapter appends to; each act is stamped with the
server's build. No replica holds an act after the request that brought
it: a deployment of several replicas keeps its trace in Postgres, or a
file on a shared volume, and the batch an interface has yet to send
waits in the browser. While the trace is off the session says so
(`traceOn`), the interface builds and sends nothing, and `POST /events`
answers 404: an idle feature costs nothing.

## Options considered

**One trace for agents and people.** One file, one reader. Rejected: a
call and an act share few fields, and the analyses share only their
arithmetic, which `internal/spc` now holds for both.

**Record at the HTTP adapter only.** Simpler, but a terminal interface
running in process never crosses it, and the adapter would need to know
which requests are decisions.

**A browser analytics product.** It records what the page shows, which
here is the organisation's record, and runs outside the deployment.

## Consequences

- Each criterion in `docs/EVALUATING_PEOPLE.md` is measured from one
  file, by `cartograph ux`, for any interface that sends its acts.
- An interface that sends a value learns at once: the batch is refused.
- A new act is a new name in the contract, so every interface and the
  analysis learn it together.
