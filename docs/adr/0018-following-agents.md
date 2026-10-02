# 0018. Following agents, each person their own

**Status:** Accepted

## Context

An agent works in Cartograph over MCP while its person watches in the
interface (ADR 0016). Without a view of what it is doing, the two feel
like separate worlds: the person sees proposals arrive, and nothing of the
work that led to them. Multiplayer editors answer this with following:
your view moves with someone else's.

What an agent works on is not everyone's to see. It acts for one person,
on manifests some colleagues may not read, and one person may run several
agents, or one agent with sub-agents. Most of the time a person wants
their own agents, and sometimes none of anyone's.

## Decision

**An agent's steps are presence, announced by the engine.** An agent holds
no socket, so the engine speaks for it: each step (it read a kind's guide,
read a manifest, saved a draft and how its checks stand, ran checks,
proposed) is a presence message with an `agent` block
(`contract/schemas/presence.schema.json`). Nothing is stored; a step lasts
as presence does, and an interface keeps the steps it has seen.

**Each person has their own feed.** Steps go to a live-only document per
person, `agents:<person>`, which the sync socket opens only for that
person, or for an administrator who chooses to follow someone
(`GET /agents/feed`). Not knowing a feed and not being allowed it look the
same. The presence document everyone joins carries none of it. On a
manifest's own document an agent shows as any collaborator does, to the
people who may read that manifest.

**Following moves the view.** An interface lists the agents on the feed,
one lane per agent, and a sub-agent its own lane (a client names one in a
call's `_meta`, `cartograph/subagent`; it is recorded beside its parent,
"Claude › researcher"). Following a lane moves the person's view to the
manifest the agent drafts, brings the fields it changed into view and
makes them glow, and opens the review of what it proposed. Any input of
their own hands the view back.

**What a person sees is theirs to choose.** They hide lanes, and choose
whether other people's agents show on drafts; their own always do. These
are their preferences, kept on their device.

**An agent's name comes from its client.** A sessionless MCP server hears
a client's name only in its first request, so a client that does not
repeat it is named from its User-Agent ("claude-code/2" is Claude Code).

## Two faults found on the way

- The presence document was created empty. An automerge-repo client never
  opens a document with no change, so presence away from a manifest had
  never reached a browser. Live-only documents now hold one field, and an
  empty one from an earlier release gains it on first use.
- The engine sent presence timestamps as eight-byte CBOR integers, which a
  browser decodes as BigInt and the interface's presence check refuses, so
  an agent's presence on a draft was dropped. They are sent as floats,
  exact for any date ahead.

Both were found by a client of the real stack listening over the socket;
each now has a test.

## Options considered

**Steps on the presence document.** One document for everyone, simplest;
it would tell every signed-in person what anyone's agents work on.

**A stored activity log.** Survives a reload; it is a record of a person's
work no one asked to keep, and the event log already records what was
proposed and decided.

**A server-sent event stream.** A second channel beside the sync socket,
held open while a window is, against ADR 0015.

## Consequences

- A person sees their agents work, in Cartograph, as it happens.
- What an agent does reaches no one its person did not choose, beyond
  presence on the drafts it edits.
- Steps seen before a reload are gone after it; the proposals remain.
