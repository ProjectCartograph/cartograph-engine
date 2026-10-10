# Evaluating what people use

People use Cartograph through an interface: the web interface today, the
terminal interface as it is built, or one an organisation builds for
itself. A change to anything they meet (a flow, a step, a control, a
check, a message, a screen, the change set route) is judged the way
`EVALUATING.md` judges what agents use: by defined criteria, measured
from what the server and the interface recorded, never from what
anybody remembers doing. One bar for people and agents (docs/adr/0017)
means one method too.

This page says what counts as waste in a person's work, how each kind is
measured, how the figures become defects per million and control
charts, and which commands do it.

## Terms

- **Task:** one piece of a person's work, from where it starts to where
  it is finished or given up. `cartograph ux` rebuilds four from the
  trace:
  - *Define:* a record taken through its flow to a version. KPI readings
    are a kind with a flow of their own (`KPIReadings`), so recording a
    period's readings is a Define or Change task too.
  - *Change:* a record that had a version, opened and saved again.
  - *Review:* a change set from opened to rolled in or closed. An
    agent's work arrives in a change set (docs/adr/0024, 0025), so
    deciding on it is a review.
  - *Find:* a record searched for and picked.

  A Define or Change task is finished by a version saved, or by its
  change set rolling in; it is given up when its draft is thrown away,
  its change set is closed, or nobody touches it for an hour before the
  trace ends (`-idle`). Anything else is still open and is left out of
  the rates.
- **Opportunity:** a place a task can go wrong. In a Define or Change
  task it is each field the kind's flow asks for (read-only fields
  aside), plus the decision to save. The flow document fixes the count,
  so a goal's opportunities are the same in every interface and every
  run, and defects per million compare across kinds.
- **Defect:** an opportunity that went wrong in a way the server can
  see: a problem a refused version named, or a check left open. A task
  with no defect passed first time.
- **Waste:** anything in a task that took a person's time or attention
  and added nothing to the record. Lean names eight; each is defined
  below in Cartograph's terms.
- **Shortest path:** the fewest presses and answers a task needs, worked
  out from the contract: to define a record, one press to open its
  flow, Next past each step after the first, one answer for each field
  the schema requires (a list that must hold an item costs a press and
  its item's required fields), and the save.

## Define: what counts as waste

Each criterion names the waste, what it looks like in Cartograph, and
the figure `cartograph ux` takes from it. A criterion marked *not
measured yet* is listed in every report, so a missing figure is never
read as a clean one. The bars are set from a measured baseline, never
guessed.

### Defects: work that has to be done again

| Criterion | Seen as | Figure |
|---|---|---|
| A version refused | Save as version answered with problems | problems per million opportunities, by kind, step and field |
| A check left open | a check left open with a reason | per million opportunities |
| A request failed | the interface's request could not be answered | per million requests |
| A dead end | an empty state or an error with no action that fills it | per thousand screens |
| A version undone | a field set back to its value in the version before | *not measured yet* |

A refused version is the clearest defect Cartograph has: the engine
names the field and the reason, so the fields refused most say which
question misleads.

### Overproduction: work made that nobody keeps

| Criterion | Seen as | Figure |
|---|---|---|
| A draft abandoned | a task given up after a draft was kept | per hundred Define and Change tasks |
| A change set discarded | a change set closed after drafts were kept in it | per hundred change sets |
| A record defined twice | a record the engine flags as a duplicate | *not measured yet* |
| A record left alone | a record nothing references where its kind expects it | *not measured yet* |

### Waiting: a person held up by the system

| Criterion | Seen as | Figure |
|---|---|---|
| An answer slow on the page | a press answered after 100 ms | share of presses |
| An answer slow from the server | a request answered after one second | share of requests |
| A wait with no sign | a request or screen over one second with no skeleton or count | share of waits |
| A change set waiting for review | from sent for review to rolled in or closed | lead time, median and p95 |

The time budgets are `DESIGN_RULES.md`'s ("The interface answers",
rules 1 and 8). They are rules the interface claims; this measures
whether it keeps them.

### Non-used knowledge: asking for what Cartograph already knows

| Criterion | Seen as | Figure |
|---|---|---|
| A guide passed by | a field refused whose guide, or its list's, was never opened in the task | share of refused fields |
| Typed again | a record defined while one of its kind and name exists | *not measured yet* |
| A reference made by hand | a reference left empty while the register held a candidate | *not measured yet* |

### Transport: moving between places to finish one thing

| Criterion | Seen as | Figure |
|---|---|---|
| A change set switched | another change set made the one worked in, mid-task | per task |
| Screens per task | screens shown while a task was open | per task |

### Inventory: work started and not finished

| Criterion | Seen as | Figure |
|---|---|---|
| Open drafts | Define and Change tasks still open with a draft kept | count and median age |
| Open change sets | change sets neither rolled in nor closed | count and median age |

### Motion: interactions beyond what the task needs

| Criterion | Seen as | Figure |
|---|---|---|
| Extra interactions | presses and answers in a task against its shortest path | median ratio, by task and kind |
| A dead click | a press on something that is not an action | per thousand presses |
| A repeated press | three presses on one thing within a second | per thousand presses |
| Hunting | a menu, popover or picker closed with nothing chosen | per task |

### Extra processing: doing more than the record asks

| Criterion | Seen as | Figure |
|---|---|---|
| A field reworked | a field answered again before the task finished | per task |
| Back in a walk | a step left backwards | per task |
| A diff with noise | a save whose diff touches what nobody edited | *not measured yet* |
| A review of nothing | a change set item with no change in it | *not measured yet* |

A diff with noise is the defect "A save changes what you changed, and
nothing else" (`DESIGN_RULES.md`) was written against; it is next to
measure.

### Cross-cutting elements: what bears on several sections at once

Some of what a person records belongs to no one section: it bears on
several. A risk threatens a milestone, a deliverable or a cost line,
and moves scope, schedule or cost (TAXONOMY.md D60). An assumption sits
on the link it conditions. A dependency joins two pieces of work. An
owner is a role named in many places. A timing names the risks that
could move it (D47). Each is declared once and shown wherever it bears,
and each has two ways to go wrong: it is captured away from where it
bears, so the person leaves the section to record it and back
(transport), or it is saved without being placed, so the record cannot
say what it threatens (a defect).

| Criterion | Seen as | Figure |
|---|---|---|
| A risk raised in the register, not where it bears | an answer about a risk given in the risks step rather than beside the deliverable, milestone or cost line | share of risk answers |
| A risk on no side when saved | a project version with `risks-constrained` open | per hundred project versions |
| A side with no stance when saved | a project version with `constraints-stated` open | per hundred project versions |
| A held side's risk only accepted when saved | a project version with `risks-held-accepted` open | per hundred project versions |
| A response spending a held side when saved | a project version with `risks-spend-held` open | per hundred project versions |

The server records, with each version a person saves, the ids of the
checks still open on it: what was left unmet when they decided, never
what was written. `cartograph ux` prints these under "Cross-cutting".
The register stays the place to see every risk at once and to add one;
a share of risks raised there is expected, and the figure is read
against its own baseline, not against zero. Assumptions, dependencies
and owners are measured the same way as their checks name them.

## Measure

### Where the figures come from

Every source is held to one rule (docs/adr/0034): **a shape, never a
value.** No text a person wrote, no name and no figure they entered
reaches a trace; only the act's name, the kind, the record's id, the
step, the field's JSON pointer, the screen as its route pattern, a
target and an outcome from short lists, how long it took, and whether a
wait showed itself.

1. **The server's own acts.** The engine records what it decides for a
   person, for any interface: a draft kept or thrown away, a version
   saved or refused with the fields it named, a check left open, a
   change set opened, sent, rolled in, closed or reopened. The engine's
   own work and agents' calls are left out.
2. **The interface's acts.** What only the interface sees, sent to
   `POST /api/v1/events`: a screen shown, a dead end, a flow opened, a
   step entered or left backwards, a field answered (on change, never
   per key), a guide opened, a picker opened and closed, a press with
   the time to its answer, a request with its time and outcome, a search
   opened, picked from or closed, a change set switched. The server
   refuses a batch whole if any act could carry a value.
3. **The build.** Each task's shortest path, from the flows and schemas,
   kept in `internal/activity/paths.json`.
4. **Lab runs.** A task on a fresh vault from a frozen build, done by
   one person, or one agent driving the browser, at a time, scored from
   sources 1 and 2.

The trace is off unless asked. `CARTOGRAPH_UI_TRACE` is `off`, a
`postgres://` URL whose table every replica appends to, or a file the
acts are appended to as JSON lines; each act is stamped with the
server's build. Where it is kept is an adapter behind two ports, one to
record and one to read, so the figures below are the same from either.
No replica keeps an act after its request; a deployment of several
replicas keeps its trace in Postgres, or a file on a shared volume.
While it is off, the session says `traceOn: false`, the interface builds
and sends nothing, and `POST /events` answers 404.

### The figures

`cartograph ux <trace file>...` or `cartograph ux <postgres URL>` prints
them; `-json` for scripts, `-person` for one person's acts, `-from` and
`-to` for a window.

- **Defects per million opportunities (DPMO)** over the Define and
  Change tasks closed, and the sigma level from it with the customary
  1.5 shift, worked out as `cartograph traces` does for agents
  (`internal/spc`). Also per task type and kind.
- **First pass yield:** the share of closed tasks finished with no
  defect.
- **Rolled throughput yield:** per kind, the product of each step's
  yield, a step passing when no field it asks was refused. The step
  with the lowest yield is where to look.
- **Completion:** tasks finished out of tasks closed.
- **Task time:** start to finish, median and p95, by task and kind.
- **Value-added ratio:** answers kept and versions saved, out of every
  act counted as value-adding, necessary or waste.
- **Lead time** of change sets, and **work in progress** at the end of
  the trace.

### Control charts: XmR

`cartograph ux -period day|week|build -chart` takes one reading per
period of each figure that can be charted (defects per million
opportunities, first pass yield, completion, median time to define a
record, the share of presses answered slowly, dead clicks per thousand
presses), each with the count it was worked out from, and charts each
series as an individuals and moving range chart: the centre is the
mean, the natural process limits the centre less and plus 2.66 average
moving ranges, and a signal is a reading beyond a limit, the eighth in a
row on one side of the centre, or the sixth rising or falling.
`internal/spc` computes it, as it does for a KPI's readings (TAXONOMY.md
D58).

- Limits are trusted from twenty readings; until then the chart says so.
- A rate from few tasks moves for no reason but the count. Read each
  reading against its count, or take a longer period.
- A change to the interface is a new process: `-split <period or build>`
  works the limits out from there on.

Nothing is written to a vault; the command prints.

## Analyse

- **Pareto:** the fields refused most, by kind and step, come first in
  the report. The top few are the questions to rewrite first.
- **Value stream:** a task's acts as value-adding (an answer kept, a
  version saved), necessary (moving through what the task needs) and
  waste (everything in the eight above).
- **Variance:** task time and interactions across people and runs. A
  task that varies widely is one whose path the interface leaves open.
- Find the root cause in the contract or the engine before the
  interface: a flow that asks in the wrong order, a guide that says what
  a thing is rather than what to write, a check that names the wrong
  field. A fix in the contract fixes every interface at once.

## Improve

- Fix the cause where it is held: the flow, the guidance, a rule in the
  engine, then a shared component in the interface, and a screen last.
- Every defect found gets a test that fails without the fix: a
  conformance scenario where it is a behaviour, a unit test otherwise.

## Control

- **The path budget.** The gate fails a change that lengthens a task's
  shortest path past `internal/activity/paths.json`, naming the task.
  `just ux-budget` writes the file, on the host, and the pull request
  says why the path grew. A screen gets harder to use only on purpose.
- **The time budget.** The interface's own tests hold the time from a
  press to its answer to 100 ms.
- **The charts.** Read each period. A signal is investigated before the
  next change to the same surface.
- **Lab runs on a frozen build,** one at a time, each scored before the
  next, three passing in a row by default:

| Do | Run |
|---|---|
| Freeze the build under test | `just eval-build <dir> [rev]` |
| Serve a fresh run of a task; print its brief | `just ux-serve <dir> <task>` |
| Score the run from the people's trace | `just ux-score <dir> <run> <task>` |
| Every run and the streak | `just ux-status <dir> [task]` |
| Stop a run's server, or every run's | `just ux-stop <dir> [run]` |

A task file is JSON, kept beside the evaluation directory, never in the
repository: the `name`, the `brief` the person is told word for word, an
optional `seed` vault, the `runs` that close it, and the `bar`: the task
`type` and `kind` that must be finished, and any of `maxDefects`,
`maxSeconds`, `maxExtra` (interactions as a multiple of the shortest
path), `maxSlowPresses` (a share) and `maxDeadClicks`.

```json
{
  "name": "define a team",
  "brief": "Add the packing team, under operations.",
  "bar": {"type": "define", "kind": "Team", "maxDefects": 0, "maxSeconds": 300, "maxExtra": 2}
}
```

The result, with the figures, goes in the pull request.
