# 0023. A decision model behind a port

**Status:** Accepted

## Context

Cartograph checks what people write with code: a statement's words, its
numbers, its dates, its links. Code is right for numbers and links and
wrong for meaning. Whether an outcome is written as a state or an action,
whether an aim says one thing or two, whether a gap is a shortfall in
results or a missing resource: the guidance teaches each with good and
poor examples, and the only way code could check them was a list of
words (`aimVerbs`), which a plan's own words defeat. And a person who
types what they are working on has no way to be told what it is in
Cartograph's terms, or that a record already says it.

Decision models answer typed questions about a text, with calibrated
probabilities, without writing anything: which of these options it is,
whether a statement holds of it. Laya is one, open under Apache 2.0,
with an English and a multilingual checkpoint. It answers in tens of
milliseconds on a server CPU and needs about 2 GB of memory, but it runs
on the ONNX runtime, through Python or Node.

## Decision

**A port, `internal/decide`, with two answers:** a `Decider` answers
named questions about one text in one call, each a `Choice` among
described options or a `YesNo`, each answer a probability. It reads; it
never writes text.

**The engine asks it, and works without it.** `engine.WithDecider` sets
it; none is the default (`CARTOGRAPH_DECIDE=off`). The engine asks it two
things, in the shapes measured to work (below):

- *Judgements*, from the contract's guidance: a contrast read off one
  field's good and poor examples, asked as a two-way choice with the good
  option first (`judgements` in each kind's guidance). The engine reports
  each as an advisory check, on the step that holds the field. Without a
  model, they are not asked. Only contrasts measured against examples
  whose answer is known are kept.
- *Relevance* (`POST /relevant`, MCP `relevant`): the likeliest few
  records of each kind for a piece of work, asked of each record on its
  own whether the work is about the same thing, for a shortlist put first
  in every picker. `GET /decision-model` (MCP `decision_model`) says
  whether a model is configured and ready.
- *Matching* (`POST /match`, MCP `match`, and `POST /understand` across
  every stage, for the home page): of each existing record on its own,
  whether a text says the same, by its name. The answer is the one
  record the model is sure of (at least 0.85) and clear of the next by
  0.05, or none: a lower bar named a wrong record for one sentence in
  four. Without a model, both fall
  back to the words the texts share, and say so (`by: words`).
- *Routing* (`POST /understand`): the three flows likeliest to define
  what a person typed, offered for the person to choose, each opening
  its flow with the text in it. Each stage is asked on its own whether
  the text is what its cue describes (`cues` in each kind's guidance),
  "yes" first; a goal's three levels share one flow. The model never
  picks the flow: see below.

**Laya runs as a sidecar** (`deploy/laya`), reached over HTTP by the
`decide/laya` adapter, because its runtime needs cgo and a static binary
built without cgo cannot carry it. The protocol is Laya's own call as
JSON. A sidecar that is down or slow (`CARTOGRAPH_DECIDE_TIMEOUT`, 5 s)
is unavailable, not an error: the engine answers as it would without
one. Answers are cached in the process by text and question, a cache
that costs only time to lose.

**Agents and people are held to the same judgements**, because the
checks are the engine's, read through every interface and MCP alike.

## Measured

Laya's English checkpoint, run through the sidecar, on examples from the
guidance and the example workspace whose answers are known
(2026-10-04):

| Question | Shape | Right |
|---|---|---|
| Is an outcome a state, not an action | yes or no, "rather than" | 2 of 10 |
| Is an outcome a state, not an action | two-way choice, good first | 9 of 10 |
| Is a gap a result, not a resource | two-way choice, good first | 6 of 6 |
| Does an aim say one result, not two | two-way choice | 2 of 6 |
| Is a problem what people cannot do | two-way choice | 3 or 4 of 8 |
| What kind of thing a sentence is | one choice among the stages | 3 or 4 of 10 |
| What kind of thing a sentence is | a tree of two-way choices | 3 of 12 |
| Which record says the same | one choice among the records | 4 of 8 |
| Which record says the same | each record on its own, "same" first | 7 of 8 |
| Which record says the same, if any, among all 35 | as above, by name, sure (0.85) and clear of the next (0.05) | 10 of 12, no wrong record named |

Routing was measured on 50 sentences a person might type, five for each
of ten stages, half outside the example's domain, and then, without any
change, on 30 more written afterwards for other kinds of organisation:

| Asked | First flow right | Right flow in the first three |
|---|---|---|
| One choice among the stages | 24 of 50 | 32 of 50 |
| Each stage on its own, first wording | 26 of 50 | 39 of 50 |
| As close to each example as to others of its stage | 18 to 22 of 50 | 31 or 32 of 50 |
| Eleven two-way properties, combined by a fitted model (left out one at a time) | 15 of 50 | 30 of 50 |
| Each stage on its own, plain cues (kept) | 28 of 50 | 47 of 50 |
| The same cues, on the 30 not used to write them | 15 of 30 | 27 of 30 |

A goal's level was the weakest part, and the cause was the definitions,
not the model: goal and objective differed only by "broad" and "several
years" against "specific" and "one to three years", which no sentence
shows. Rewritten so each level has one test that tells it apart, and
measured on 15 sentences written to the new definitions in two domains
the example does not use, before any wording was tuned:

| Level definitions | Right level of 15 |
|---|---|
| The glossary's, by scale | 6 |
| Longer, with "never finished" and "concrete enough to plan projects for" | 10 |
| "A broad direction your organisation keeps working towards" / "One concrete change your organisation sets out to make" / "A fact about people or things once that change is made" (kept) | 13 |
| The same, objective adding "under a goal" | 9 |

The kept definitions are both what a person reads in the glossary and
what the model is asked: a goal's flow is ranked by them, and its level
chosen among them in one question. Ranking by them rather than by
separate cues left routing as it was (27 of 30 unseen sentences had the
right flow in three; 28 with the definitions).

Portfolios (TAXONOMY.md D32) added a stage, and the programme's cue had
to change with its definition. Measured on 10 sentences written to D32
before any cue was tried (5 programmes, 5 portfolios), and on the 72
sentences of the other stages:

| Programme cue / portfolio cue | D32 sentences, right flow in three | Other stages, in three |
|---|---|---|
| "several projects that each need the others to bring about one change" / "projects and programmes grouped to decide which to fund first" | 7 of 10 | 64 of 72 |
| "related projects coordinated together towards one shared change" / "a set of projects ranked and funded against the strategy" (kept) | 9 of 10 | 61 of 72 |
| "several projects grouped under one programme" / "work grouped to decide what to invest in, hold or stop" | 7 of 10 | 64 of 72 |
| kept programme cue / "projects and programmes grouped to decide which to fund first" | 8 of 10 | 59 of 72 |
| kept programme cue / "a set of projects the organisation chooses between and funds" | 8 of 10 | 60 of 72 |

One more stage is one more for the three places: the other stages had
their right flow among three 85 percent of the time with it, against 89
without it. The kept pair tells programmes and portfolios apart best.

Relevance across the workspace (`POST /relevant`) was measured on ten
project briefs, seven in the example's domain and three about work the
workspace does not touch, each labelled with the records of eight kinds
a person would want suggested, before any question was tried. Shown is
each kind's top three at or above the floor:

| Asked of each record | Floor | First right, larger kinds | Shown on unrelated briefs | Shown that was labelled |
|---|---|---|---|---|
| Shared words (no model) | 0.5 | 17 of 28 | 1 | 24 of 42 |
| "Is this record relevant to the work described?" | 0.5 | 19 of 28 | 22 | 30 of 113 |
| "Is the work about the same thing as the record?" (kept) | 0.5 | 22 of 28 | 3 | 30 of 81 |

The model ranks better than shared words and the kept question keeps
quiet about unrelated work; much of what it shows beyond the labels is a
plausible neighbour (another outcome under the same objective). So it is
a ranking to put first, as a shortlist, and never a filter: everything
else stays searchable, and the person chooses. Its limits stay what they
are: no reasons, about 512 tokens of context, two-way questions only.

Agents are told to ask `decision_model` first (MCP), whatever is behind
the port, and when it is ready to call `relevant` with what the work is
about before choosing what a draft names; the port gained `Ready` for
it.

No confidence picked out a first flow that could be trusted (the surest
were still wrong a third of the time on the 30), so three are offered
and the person chooses.

So: the two contrasts that measured well are the judgements; matching
asks of each record on its own; routing asks of each stage on its own
and offers three flows, never one. A sentence's kind is never decided
for the person.
The model reads the options' order and keys (the same contrast, good
option second, fell from 9 to 7 of 10), so the adapter sends them in the
order given. A new judgement, or a changed cue, comes with its own measurement.

## Consequences

- A deployment that wants the checks runs a second process with about
  2 GB of memory and a 1.7 GB model download. One that does not changes
  nothing.
- A judgement is advice, never a refusal: a model can be wrong, and a
  check never blocks a save.
- The questions are the contract's words, versioned with it; changing
  one changes the check, as editing an example does.
- Laya reads at most 512 tokens; Cartograph asks it about one statement
  at a time, well inside that.

## Rejected

- *In-process inference.* The ONNX runtime's Go bindings need cgo, and
  no cgo-free runtime for this model could be confirmed. Giving up the
  static binary for one optional feature is the wrong trade.
- *A hosted API.* Sending what organisations write to a third party by
  default is not Cartograph's to decide; a deployment may point the
  sidecar's URL anywhere it chooses.
- *A generating model for the home page.* Cartograph understands and
  routes; conversation is what agents over MCP are for.
- *Picking the flow for the person.* Measured above, the first flow is
  right half the time; it is offered, never taken.
