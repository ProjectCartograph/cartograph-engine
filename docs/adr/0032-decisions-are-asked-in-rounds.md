# 0032. Decisions are asked in rounds the engine computes

**Status:** Accepted. Extends [0030](0030-meaning-is-system-one.md) and
the order of work (TAXONOMY.md D28, D56).

## Context

An agent defining work with a person asked "one question at a time, as
soon as a check needs it", led by `next`, which hands over one check at
a time. A port of one charter took about 120 calls, and the person met
the same kind of question again and again, each one a separate exchange.

The grilling technique (Matt Pocock's `grilling` skill, the primitive
under `grill-me`) interviews a person over a **design tree** of
decisions, each branching into the decisions that hang off it. It asks
in **rounds**: each round is the whole **frontier**, every decision whose
prerequisites are settled, numbered, each with a recommended answer
worded so that "yes" accepts it. Finding facts is the agent's job;
decisions are the person's; the session ends when the frontier is
empty, and nothing is acted on until the person confirms. Its own
documentation names its weakness: the agent chooses the frontier by
judgement, not from a graph, so two questions can share a round when one
answer should have changed the other, and weaker models answer their own
questions or skip the confirmation.

Cartograph has the graph. The record is a directed acyclic graph written
from the top down, and every open check already has a place in the
order of work: its record's stage, its phase (what the record is, its
numbers, its links) and its flow step.

## Decision

The engine computes each round, and the MCP server hands it to the
agent with how to ask it (`round`).

- **The frontier.** A question waits on what its own answer names: a
  field that names a record (an indicator's aims, a project's outcome)
  is asked once that record's definition has no check open that
  blocks. Nothing else in the work holds it back. Within a record, its
  definition's checks are asked together, and its numbers and links
  wait while one of them that blocks is open. A warning holds nothing
  back. What depends on an answer in this round is counted as waiting,
  and comes in a later round.
- **Facts and decisions.** When the work has a document, what a
  document itself states (the checks a port writes and never waives)
  is listed in `settle`: the agent writes it, and asks only where the
  document is silent. Everything else is in `ask`, the person's. Links a
  later record settles are in `write`.
- **Recommendations.** Where existing records could answer a question,
  the decision model ranks them against the record, and the most
  relevant is recommended (0023). Without a model nothing is judged
  (0030): the agent recommends from the documents and the guide's good
  examples, and says why.
- **How to ask.** Every round answer carries the format: the whole of
  `ask` in one message, numbered, each with the recommendation worded
  so that yes accepts it, the choices by name and room for the person's
  own; with a multiple-choice tool, the recommended option first. A
  person who chose step by step gets the same questions one at a time.
- **The confirmation gate** is Cartograph's own: a round that finds
  nothing left tells the agent to show the person the summary and ask
  them to confirm before proposing, and nothing becomes the record until
  the person accepts the change set in Cartograph. `leave_open` already
  refuses a check left open without the person's answer.

The instructions change with it: ask in rounds, never ask what `settle`
lists, never answer a question in `ask`. `next` stays, for working
alone.

## How it is judged (docs/EVALUATING.md)

Read from the server and the trace, on fresh runs of one frozen build:

1. Every check the change set leaves for the person carries what the
   person was asked, and none is a check the document states of the
   main project (a part the document only names is the person's to
   authorise).
2. With a person present, the agent calls `round` before it proposes,
   and asks more than one question a round where the round has more
   than one: questions per exchange above one.
3. No round asks a question whose record has an earlier step open
   (a defect the engine now prevents). The trace keeps no values, so
   this is held by the engine's tests, not scored.
4. The streak's median calls per run no higher than the last streak's
   median: rounds must not add waste. Judged on the streak, not each
   run: one run in two of an unchanged process is above its median, and
   runs 009 and 010 failed on that alone.
5. With a person present, the first proposal comes after the person
   answered: the evaluator records each answer (`eval answer`), since
   what the agent writes as asked cannot show it (Sonnet run 007 wrote
   "answer awaited" and proposed in the same turn).

The scorer (`rounds` in the criteria file, `internal/evaluate`) reads
1 from each left check's `asked`, which the change set keeps with it;
2 from the trace for the round, and from the exchanges the left checks
name for the questions; 4 in `eval status`, the streak against `maxCalls`,
unset on the first streak, whose median becomes it.

The bar is three passing runs in a row, one agent at a time.

## Consequences

- The person answers several independent questions in one exchange,
  and is never asked one whose premise they have not settled.
- An agent that answers its own decisions still can; the rule against
  it is in the instructions and in every round's `how`, the weakest
  kind of fix (0027). What the engine holds is the frontier and the
  split between facts and decisions.
- The frontier follows each answer's own references. It began by
  stage (every later stage waiting on every earlier definition,
  warnings included), then by record (a record waiting on everything
  it names, and offering one step at a time). Evaluation runs 002 and
  003 showed both asking two to five questions a round with a dozen or
  more waiting: a project's risks held behind its component's sponsor.
  It never asks one too early: an answer that names a record waits for
  that record.
