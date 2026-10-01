# Cartograph design rules

Rules that every screen follows. Builders derive copy and controls from
these; they do not invent their own.

## The running example

The examples below follow one case, the same one `TAXONOMY.md` uses. A
ministry of education wants more children to read well in the early grades.
Its goal is *make sure every child can read with understanding*; under it
the objective *raise reading in the first three grades*; under that the
outcome *children read fluently by the end of grade 3*. The KPI is *share of
grade 3 learners reading at the expected level*, 38 percent at the baseline
and 60 as the target, read once a year from the national reading assessment.
The gap is *reading falls behind by grade 2 and the distance widens after*.
The **Early Reading Programme** coordinates two projects: *coach early-grade
teachers in reading* and *add a grade 2 reading check*, which lands in the
national assessment service, an operation. The groups concerned are
early-grade learners, learners in rural schools and early-grade teachers.

## The rules

1. **Strict shadcn/ui.** Neutral theme, default radius, default type stack,
   stock components and variants. No custom tokens. Destructive is the only
   non-neutral colour.
2. **No login, no persistent actor.** Attribution is asked in the dialog
   that needs it, as a dropdown of declared people.
3. **Three phases, three questions.** Initiation (how does this project
   start), Closing (how does it close), Landing (how does it land). Nothing
   else is a step.
4. **One section on screen at a time.** Inside a phase, a section rail lists
   the sections with their state; the main pane shows only the current
   section; the right rail shows what this section feeds and its own checks.
   Nothing else. A person is never shown the whole phase at once.
5. **One visual per section, matched to its data type.** Goals: chips, one
   per goal a project may align to.
   Aim: sentence cards on a baseline-to-target scale. Scope: in and out as
   chips. Deliverables: cards. Beneficiaries: picked groups as chips, and
   no count. Timeline: phases first, each with a duration in months; the bars, the end date and the total follow from the start month, so the time cost is assessed phase by phase. People: role slots
   and a power-and-interest grid. Data: a uses-to-produces flow. Risks: a
   likelihood-by-impact grid. Closing and Landing: proposed test lines.
6. **Pick over type.** Anything that already exists is chosen, never typed:
   people, teams, parties, goals, KPIs, data sources, cycles, beneficiary
   groups, operations, programmes. Every picker searches and filters by
   goal, team, operation or cycle through the reference index.
7. **Typed parts over sentences.** Numbers carry a kind and a unit, dates are
   months, directions are chosen; the sentence is generated.
8. **Free text is short and capped.** Intro paragraphs are not written; a screen's question is its subtitle, and "Feeds" is a chip and a few words. Objective 120 characters, problem and
   change statements 240, a scope chip or deliverable name 60, a rationale
   160; a counter shows the remainder. Where a fact is unknown, a "not known
   yet" choice with a reason replaces prose.
9. **One line per item; long lists truncate.** Text is one line with an
   ellipsis and the full text on hover; lists show a few items then "+n
   more"; sheets are tables with filters and pages.
10. **Later phases are proposed from earlier ones.** Nothing is written twice;
    the person confirms thresholds and sign-off.
11. **Checks never block a draft.** They block a submission, and each says
    where to fix it.
12. **Representation follows importance and type.** The most important
    statement on a screen is the largest field on it, at the top: the
    problem, the change, the objective. Sentences are rows, one per line,
    numbered where order matters. Chips are for tags, states and short
    categories only, never for a sentence. A count is a number with a
    unit and a bar; a date is a month; a choice is a control. A short
    name picked from a register is a chip, and its tag is the branch it
    was picked from.
13. **Sample content is marked (sample).** The tool ships generic; instance
    content lives in an instance directory.

## Contract additions: mandate, funding, risk rows, personal data
Mandate on Project, Programme and Operation (kind, title, reference, date,
issued by); one approved funding envelope per currency on Project; risk
rows typed risk, issue, dependency, assumption or constraint with an
escalate flag and reason; personal data (none, personal, sensitive) on
produced and consumed data; BeneficiaryGroup register (its structured
beneficiary counts since withdrawn: see "Beneficiaries are qualitative");
maxLength caps. Approve with conditions belongs to
change control: each condition has an owner role and a month,
and open conditions show on the checks.

## Contract changes queued (from rules 6 and 8)
- New directory kind **BeneficiaryGroup** (name, description, source ref
  DataSource). `Project.spec.summary.beneficiaries[]` becomes
  `{group: ref BeneficiaryGroup (req)}`. (Amended: the counts and
  `typicalSize` this bullet first specified are out; see "Beneficiaries are
  qualitative" below.)
- `maxLength` on free text: objective 120, problem and change 240, scope
  items and deliverable names 60, rationale 160, risk description 160.

## Bootstrap, field names and level names
- **No circular dependency at bootstrap.** A required picker never has zero options. While no
  Person exists, "who is doing this" is hidden and the change is recorded as bootstrap; the
  first person may be declared as their own author. Every directory-fed picker with an empty
  list shows where to add the entry and never blocks a save the schema does not require.
- **Plain field names under a named parent.** `baseline.date` and `target.date`, never `asOf`
  or `by`; the parent already says what the date is.
- **Level names:** goal, objective, outcome, then Project (`TAXONOMY.md` D25 gives the levels
  these names; they were first called pillar, strategic and functional). The organisational
  unit stays "Team". (The third level was removed once and restored after a review of the
  running interface: an outcome sits under an objective, and projects align to outcomes.)

## Goals as the root
- **Hierarchy:** goal, objective, outcome, project. A goal has no parent; an objective serves one
  goal; an outcome serves one objective; a project aligns to outcomes. Each level depends only
  on the level above.
- **A goal references nothing below it.** No team, cycle, source or KPI on a goal; KPIs, projects,
  programmes and operations reference goals, and the tree reads those references back.
- **Goals are easily mutable.** Add a goal or an objective by title alone, inline; rename in
  place; move between goals; delete when nothing references it. Shaping direction must feel
  like thinking, not filing.
- **Roles, not persons, in definitions.** A project names the roles it needs (sponsor, lead,
  accountable per section, stakeholders with influence and interest); mapping roles to people
  comes later, in a role binding when known or in the delivery tool.

## Single-person Cartograph
- **Resources, not roles or persons.** The project's People section is "Resources": the roles and
  other resources a project needs, picked from a shared catalogue (`Resource` manifests) or
  titled inline. Names of people never appear.
- **One person, no actors.** No login, no "who is doing this", no approvers. Every save is a
  version by the local operator. Sharing happens through synchronised manifests, not accounts.
- **Change control is not Cartograph's job.** Proposals, approvals and conditions live in the delivery
  tool. Cartograph keeps immutable versions and the diff between them so a formal change is scoped
  here and carried there.

## The vault and the boundary
- **Files are the truth.** Cartograph opens a vault of YAML manifests; the index is a cache. Edits made
  outside Cartograph count. Sync and history across machines are the vault's own version control.
- **The gate is advisory; handoff enforces.** Checks never stop a save or a snapshot; handing
  off refuses while anything blocks.
- **The boundary is the bundle.** The rendered charter leaves Cartograph; approval and change
  requests happen in the workflow tool; the definer returns to Cartograph to make the change.
- **Deterministic from the files.** Every command's output is a function of the vault; the
  interface is a guide over the same engine.

## Picking a goal is a drag from a visible tree

Wherever a definition picks a goal (Start a project, the Initiation goals section, KPI links),
the goal tree is on screen beside the drop zone and every outcome in it is draggable;
search only filters the tree. A search box with no tree is not the design. Moving a goal in the
tree never changes the card's shape or text size; the same card renders at every level and
after every move.

## Tests run in seconds

`just test` is the gate and completes in under 10 seconds on the development machine: Go
tests plus web unit tests (Vitest, jsdom), no browser, no network, no fixed sleeps (watchers
and debounces take their interval from an option the test shortens). Every contributor runs
`just test` and `just tsc` after each edit; nobody is asked to run the browser flows.
The browser flows (`just e2e`, the former smoke) are an end-to-end check run
at acceptance only; they wait on conditions, never on timers, and finish in under
two minutes; the randomised journey is a separate on-demand recipe. A behaviour that matters
gets a unit test at the layer that owns it (an earlier data loss on moving a goal would
have been a ten-line test of the mutation against a fake client). This is the pitfall of many earlier projects and does not repeat here.

## Files never move; the state manifest says what is live

Files are the source of truth as in GitOps: nothing in Cartograph moves, renames or removes a
manifest file. `vault.yaml` includes what is live; the index is a cache rehydrated from it.
A delete removes an include line with a reason; a recovery puts it back; both are ordinary
diffs for the vault's version control. Every mutation is an apply unit journalled before
the files are written (outbox), replayed on open. Deleting stays possible only on leaf
nodes of the reference graph. Supersedes an earlier delete that removed the file.

## A goal keeps its level

A goal defined at a level stays at that level, in the files, in the engine and in the
interface. A move changes only the parent, and only to a parent of the level above (an
outcome to an objective, an objective to a goal); the engine refuses any
other parent on every write path, and a drop target refuses the wrong level with a message
before anything is sent. The interface renders a card by the goal's `level`, never by its
depth in a column. The rule came from an outcome shown as an objective.

Every goal card carries a tag naming its level, in a muted tone of its own (violet for a goal, sky for an objective, emerald for an outcome, on the tag and as a faint tint behind the card, in both themes), so the tier is visible without reading the tree's indentation and a goal that needs to move is recognisable at a glance.

## Names are not identities; goals are isolated by level; a goal can exist unbound

A goal's name is not its identity. A goal's id is generated by the server (ten lowercase
base32 characters, never derived from the name) and the file is `Goal/<id>.yaml`; existing
ids keep their form; files never move. Names are scoped to the parent branch the way
Kubernetes names are scoped to a namespace: two outcomes under different objectives
may share a name, two objectives under different goals may share a name, and a
name is refused only when a sibling under the same parent already carries it (top-level goal
names are unique across the vault). The ministry can have an outcome called *teachers use
the coaching in class* under its reading objective and another under a numeracy objective.
The interface always shows the name with its ancestry where a
name alone would be ambiguous. The same holds for every kind: ids are generated by the
server for every resource created from now on, the file is `<Kind>/<id>.yaml`, and the
interface never presents an id (a project's organisational code such as a register number
is a field of its own, `metadata.code`, shown and searchable like the name). Names are unique
within their namespace: the parent branch for goals, the kind for everything else. Existing
ids, including the readable slugs in the example and the instance, stay as they are. An objective or outcome may exist without its parent: it is then unbound, shown
in an Unbound tray on Goals home rather than in the tree, flagged by an advisory check, and
not offered to projects until bound; binding it means giving it a parent of the level above.
A parent of the wrong level is still refused, and a goal still keeps its level. The
rule came from an outcome named "test" colliding with an objective named "test".

## Picking from a register is chips, grouped by the shape it has (supersedes "Picking a goal is a drag from a visible tree")

Wherever a definition picks something a register already holds, the control is
chips: one chip per thing that can actually be picked, one search box, a click to
pick and a click to unpick. Nothing unpickable is a control, there is no second
panel repeating what has been picked, no drop zone, and no paragraph explaining
the control. The rule came after the goal tree on the Alignment step put
two unpickable levels and a duplicate panel on screen to pick one outcome.
The canonical tree stays where it is edited (Goals home, with its drags and its
levels); a definition only picks from it.

**Where the register has a shape, the chips are grouped by it** (correcting
the first attempt): a flat wrap of chips gives a person no
place to put what they are looking at, which defeats the step. The branches are
quiet headings (the goal small and uppercase, the objective in plain
weight under it, the chips under that behind a left rule), and the step reads
down the shape of the plan. On the reading check's Alignment step, *make sure
every child can read with understanding* is the top heading, *raise reading in
the first three grades* sits under it, and *children read fluently by the end
of grade 3* is a chip under that. The ancestry belongs to the heading, so a chip
carries its own name and nothing else; repeating the objective on every chip is the
noise the grouping removes. Search matches an outcome, its objective or its goal, and a
branch with nothing left in it disappears whole. A picked chip stays where it
belongs and is never sorted to the front, because moving it breaks the one thing
the grouping is for; only a flat register (no branches, such as beneficiary
groups) sorts its picks forward. The level tag every goal card carries on Goals
home is not repeated here: every chip on the step is the same level, so the tag
would say nothing. This section is calm, focused and purposeful: one question,
one structure, no scenery.

## Beneficiaries are qualitative

A beneficiary group is a group of people the problem is later targeted at, so it
is identified and never counted. Learners in rural schools are named on the
coaching project, not counted there. `Project.spec.summary.beneficiaries[]` is a
reference to a BeneficiaryGroup and nothing else; `count`, `countBasis` and
`countReason` are out of the contract, and so is `BeneficiaryGroup.spec.typicalSize`.
The step is the register as chips, plus the dialog that adds a group the register
does not hold. The checks ask only whether any group is named. This
supersedes the structured beneficiary counts in "Contract additions" above.

## Text is the last resort

Any text on a screen must survive the question "can iconography, structure or the
control itself say this instead?". If it can, the text goes. What survives:
a field's label, its placeholder, a state ("Saved", "2 blocking"), a rule a person
would otherwise break silently (capped at about 40 characters, never a sentence
about why the rule exists), and an example behind the lightbulb, which is opt-in.
What does not survive: a paragraph introducing a step, a hint restating its label,
a note explaining where a field's data goes afterwards, a sentence justifying the
design to the person using it. The rule has a number on it: applying it cut the
project flow's copy by half, and no screen
carries an explanatory paragraph any more. A builder adding text says, in the
commit, which of the surviving categories it falls into.

## What a success criterion is, and is not

SMART goals define what the project intends to achieve. Scope defines the
boundaries of the work required to achieve them. Deliverables define the outputs
the project must produce. **Success criteria define how stakeholders will
determine whether those outputs actually produced the intended result**: the
measurable standards by which the project is judged, agreed before work begins,
so that success is observable rather than argued. Without them a project can
complete every task and deliverable and still create no value.

It follows that a deliverable's acceptance is not a success criterion: it is the
deliverable's own test, written on the deliverable, and restating it says nothing
about whether the result arrived. The derivation proposes outcome lines from the
key results, plus the schedule, budget and compliance the project committed to;
the "<name> accepted: <role> signs it off" line every deliverable used to propose
is gone. The coaching project's guide for coaches being signed off is the
guide's acceptance; grade 3 reading in coached schools rising towards 60 percent
is a success criterion.

## A goal is a commitment, not a chip

On the Alignment step a goal is a full-width row with its own selection mark, not
a chip in a wrap: a goal is the aligning factor of the whole project and the
element has to carry that weight. Rows sit in a bordered list under their
objective, which sits under its goal; each heading carries the level's own
mark (the same marks Goals home uses), and a branch with goals picked in it shows
that count as a number beside its name. Chips remain right for a flat register of
short tags, such as beneficiary groups.

## Goals cards are neutral

The three goal levels no longer carry a tint of their own, on the tag or as a
wash behind the card. Strict shadcn neutral is the rule everywhere else in the
app, and the level's own mark distinguishes the tiers by shape, which is what a
person scanning a column uses. Supersedes the per-level colour in "A goal keeps
its level".

## A key result is a unit and an outcome, and one sentence

The number's unit is chosen once. The metric field then asks only what happens to
them ("teachers" + "coached"), and the two are stored as one metric, so
nothing says the unit twice. Where a kind carries no unit (a percent, a ratio) the
field asks for the whole phrase. One builder writes the sentence and every
surface reads from it: "Increase teachers coached to 1200 by 2027-06, from 0 in
2025-09" on the card is the same sentence the editor previewed. A saved key result
always shows its whole statement, never a fragment with the number in a footnote.

## A KPI belongs to the project, not to a key result

`Project.spec.kpis[]` is `{kpi, reason}`: the standing measures this project
moves, each named with the reason it is named. A KPI is a whole-of-project
concern and one KPI may be moved by several projects (the grade 3 reading KPI
is moved by both the coaching and the reading check), so hanging it off a single
key result said something untrue; `relatesTo` is out of the contract. Nothing
assumes a KPI can judge this project's landing: how landing is assessed is a
separate question, still open.

## Every resource is standalone; attach directly, or bind

A role, unit, party, system or facility is a `Resource` manifest, declared once and
used across many projects. Work attaches to one of two ways. **Directly referenced**
when it is simply part of the cast: `Project.spec.resources[]` carries the position
and the project's own title for it. **Bound** when the link itself carries data that
belongs to neither end: how much power a stakeholder holds over *this* project varies
per project, so it lives on `StakeholderMap`, a binding scoped to the work. The
colleges that train teachers hold more power over the coaching project than over
the reading check.

The test for a new field: if the fact is about the *relationship* rather than about
either end, it goes on a binding kind. Do not put it on the resource, where it would
have to be the same for every project, and do not put it on the project, where it
duplicates the party. A binding must never become the only way in; the project still
references its stakeholders directly; the map only scores them.

## One reference shape

`common.schema.json#/$defs/Ref`: exactly one of `kind`+`id` (another manifest),
`local`+`id` (an item inside this manifest), or `external` (a target the vault will
never hold, so nobody invents a manifest for the treasury). One shape so one walk compiles
every edge in a vault into a tree and one picker edits them all.

Adding a reference costs nothing in the engine. `x-cartograph-ref: "*"` on the id reads the
sibling `kind` and the generic walker checks the target exists; the `local` and
`external` forms carry no sibling kind and are skipped by it, which is correct, and a
kind rule resolves the local form because resolving it means reading a sibling list.

Three fields held a *copy* of a role's title until this landed (the contract's own
descriptions said "exactly as spec.resources names it"), and renaming a role left
three stale copies with nothing to notice. The first run of the migration found one in
the example vault that had already drifted.

## Declare an edge at one end only

The far end is derived. A dependency says `direction: needs` on the project that
waits, and the project waited on shows "waiting on you" without writing anything. A
project names the programmes it belongs to, and the programme reads that back. One
place to change, no pair to keep in step, and no question about which end is right
when they disagree. The reading check needs the move of the learner records to a
new system; only the reading check says so, and the records project shows that
it is waited on.

## Declaring a thing and assessing it are separate acts

Scores are optional on the thing they score. A stakeholder named but not yet placed on
the grid is a real entry that compiles, renders and is reported by a check; a shape
that required the score would lose the stakeholder until somebody got round to
scoring them. The same holds anywhere an assessment is attached to a declaration.

## A gap is what is wrong; a problem is what it does to somebody

`Gap` holds an evidenced shortcoming with its citation, declared once and cited by
every problem that answers it. The problem stays a statement about a named group on a
particular piece of work. Who feels a gap lives on the problem and never on the gap,
because the same finding lands on different groups in different work. The reading
gap is cited by the coaching project, where it lands on learners in rural schools,
and by the reading check, where it lands on early-grade learners everywhere.

Pouring a plan's rationale into a problem produces "Early-grade learners the
national assessment shows that reading falls behind by grade 2", which is nonsense
and leaves the cause with nothing to hold. That is the shape of the mistake this
rule prevents.

## A category needs a shape a wrong answer cannot fill

Rows typed `dependency` turned out to be mostly a risk, an issue or a
constraint: "teachers may not attend the coaching" typed as a dependency is a
risk. Nobody was careless: the type had no shape a risk sentence could not
be poured into, so it collected whatever sounded vaguely like waiting. It carries a
direction, a reference to the far end and the phase it must land by now, and a risk
sentence cannot fill those.

When a category is being used wrongly, do not add a hint explaining it. Give it fields
only the right answer can fill. This is the same rule as "text is the last resort",
applied to a type rather than to a label.

## What the nouns mean lives in TAXONOMY.md

This file is how Cartograph behaves. `TAXONOMY.md` beside it is what project, programme,
portfolio, benefit and operation mean in the discipline, with sources, and the
decisions Cartograph has taken where it departs. Read it before adding a kind, a field
linking two kinds, or a rule that refuses a link.

It exists because a blocking rule shipped that refused a project joining a programme
it shared no goal with, and shared goals turns out to be the test for whether a
grouping should be a programme *rather than a portfolio*, not the definition of
membership in one.

## Autosave stages; saving is a decision

Autosave wrote straight to `<vault>/<Kind>/<id>.yaml` and added the ref to
`vault.yaml`. So opening a wizard made a half-answered definition part of the vault,
reformatted a hand-maintained file under its author, and left "discard draft" with
nothing to go back to: the working copy *was* the file.

A draft goes to `<vault>/.cartograph/staging/<Kind>/<id>.yaml` instead, inside the
directory every scan in the vault package already skips, so it is invisible to the
vault scan, the unapplied listing and export. Saving promotes it: the manifest file
and the include entry that admits it land in one journalled apply unit, and then the
draft is cleared.

**Only the ref being saved.** There may be many drafts, and saving one is not a
decision about the rest.

Two consequences worth stating. A kind whose only write was autosave (a programme, an
operation) now needs an explicit save control, because the writing used to be
continuous and the deciding never happened. And a draft has to be a file rather than
memory, or closing the browser would throw away unfinished work.

## The save is the atomic operation

While autosave wrote the vault, autosave was the only caller of the journalled path,
and a save was a bare `os.WriteFile` plus a separate `vault.yaml` write, either of
which could land without the other. Moving autosave out left the vault with no
journalled write in normal use, which is the wrong half to keep.

So the save writes both files in one unit: temp file, fsync, rename, recorded in the
journal and replayed on open. A crash before the rename leaves the draft; after it,
the vault file is correct.

**Where the save actually happens is not where it looks.** `Commit` runs inside a
transaction, so `txStore.PutVersion` is the path the app takes and `ManifestStore.PutVersion`
is not. The promotion was written into the second one first: a store unit test passed
while every real save left its draft behind. A test that drives `engine.Commit` is the
one that holds this.

## Write files the way the vaults are written by hand

`yaml.v3` indents four spaces. Every manifest in this repository is written two, so a
save reindented the whole file and turned a one-line edit into a whole-file diff: the
same complaint that moved autosave into staging, arriving one step later at the save.
One encoder (`internal/yamlfmt`), and everything that writes a manifest or a
`vault.yaml` goes through it.

Still open: a save re-orders keys alphabetically and expands flow sequences, because
the manifest arrives as JSON and a Go map has no order. The diff is no longer
whole-file noise, but it is not yet nothing.

## Indexing what exists is not staging somebody's work

`Reindex` walks every current manifest on open so `ListReferencing` can resolve
references, and it did that by calling `PutWorking`, harmless while `PutWorking`
wrote the file the manifest had just been read from. Once `PutWorking` staged a draft,
opening the example vault created a draft of every manifest in it, none of
them anybody's work.

Two jobs had been sharing one method. They are separate now: `PutWorking` stages what
somebody is editing, `IndexWorking` tells the index what exists. When a method is
called for a side effect rather than for what it means, that is the bug waiting.

## A save changes what you changed, and nothing else

Writing a manifest from a plain object loses everything the object cannot
carry. Keys come back in the alphabet's order rather than the author's, because
the round trip goes through a Go map; `[a, b]` becomes a block list; a quoted
scalar loses its quotes. None of that is an edit anybody made, and all of it
lands in the diff: a one-line change arrived as a whole-file rewrite.

So the interface **reads the file's own text** (a manifest read carries it
beside the parsed object) and **writes over it key by key**, touching only what
differs. What nobody edited keeps the node it was written as. A definition
being created for the first time has nothing to merge into and is written
whole.

The same fault, one layer up, alphabetised every sheet's columns, because the
schema endpoint decoded each schema into a map and re-encoded it. Schemas are
served as their own bytes now. **Whenever a Go map stands between a file and a
reader, the order is already gone.**

## A check nobody can pass checks nothing

`just words` forbade organisation-specific words and em dashes
everywhere under `cartograph/`. It had been failing for a long time on dozens of em dashes,
all of them in source comments and contract descriptions, which are prose
for whoever maintains this, not text anybody sees on screen. A gate that cannot
go green is a gate nobody runs.

The rule was right and its scope was wrong, so the scope changed: domain words
are still forbidden everywhere, and em dashes only in `copy.ts` and the example
instance, which are what a person reads. Both are now zero, and the recipe
passes, which is the point of having it.

It also caught nothing real while failing, and found four genuine leaks the
moment it could: a check message reading "Circular:" collided with the word
list, the name of a real body stood in for a generic body outside Cartograph,
and two schema descriptions used sector words as examples in general-purpose
prose.

## A hint says what to write, not what a thing is

The gap statement's hint read:

> *A shortfall in results, not a missing thing. "No case management system"
> names a solution; what is short is what that costs.*

Every word of that is true and none of it tells somebody what to type. It is a
principle stated at a person who wanted an instruction, and it reads as archaic
besides. It now reads:

> *Write what is falling short, not what is missing. Instead of "we have no
> case management system", write what that costs: "cases take three weeks to
> reach a decision".*

The same voice had spread through sixty-odd strings. The rules that came out of
fixing them:

- **Lead with the instruction.** "Write...", "Pick...", "Add one for each...".
  A rule the reader has to invert into an action is half a hint.
- **Show the shape when it is unusual.** One before-and-after example is worth
  three sentences of principle, and it is what the reader copies.
- **No internal vocabulary.** "The rest is the walk", "read back", "roll-up",
  "slices", "cited whole", "this gap's two states": every one of those is a
  word from the design docs that escaped onto a screen.
- **No archaism.** "which is not the same as nought" was on the readings step.
- **Say it once.** A step's subtitle and its first field's hint saying the same
  thing means one of them is doing nothing.

The design documents keep the dense voice; they are for whoever maintains this,
and compression there is a feature. A screen is for somebody with a job to
finish.

### Second pass

A sweep of every remaining hint found the voice in another thirty: field
descriptions written as noun phrases ("The dimension it measures, and the bar
it has to clear") or as statements of fact about the discipline ("An outcome,
not an activity"; "A project can serve something its programmes do not"). Each
now leads with the verb: *Pick the dimension it measures, then write the bar it
has to clear.* *Write an outcome rather than an activity.* *Add a goal here if
this project serves one its programmes do not.*

The checklist beside a success criterion was titled "A strong criterion", which
grades the reader's work against an ideal. It says "What this one has so far",
which reports.

The test to apply: read the hint and ask what the reader is supposed to do
next. If the answer needs deriving from a general truth, it is a principle;
rewrite it as the instruction it implies.

## A success criterion needs no deliverable behind it

Restating "What a success criterion is, and is not", because a later plan broke it.

That plan first proposed that every success criterion trace to a
deliverable, with a check reporting "an outcome with no output behind it". The
earlier rule had already refuted it in passing: the derivation proposes outcome
lines from the key results, **plus the schedule, budget and compliance the
project committed to**. Those three have no deliverable behind them by
definition, and the check would have flagged every one.

The general form: a success criterion **may** be the result of a deliverable,
and may equally assess a dimension at closing or landing that no deliverable
produces, which is what the seven metric dimensions are for. A criterion
standing on its own is complete. So a link from a criterion to the outputs
behind it is an optional enrichment, worth having so the arrow can be drawn
where it exists, and **never** a completeness test. "The coaching finishes
within its budget" has no deliverable behind it and is complete.

The lesson beyond this field: a framework borrowed from outside brings its own
shape, and where that shape disagrees with a decision already recorded here, the
recorded decision is the evidence: it was made against this work. Check
`DESIGN_RULES.md` and `TAXONOMY.md` before proposing a rule, not only before
writing code.


## An assumption is not a risk, and it lives on a link

`risks[].type` carried an `assumption` value from the beginning, and risk lists
did not use it. People had not declined to
record assumptions; there was nowhere that fitted.

The two are different things and take different answers. A risk **might** go
wrong and is *mitigated*. An assumption is a condition a step of the reasoning
**requires**, and the answer to a false one is a different theory, not a
contingency, so the field that earns its keep is `ifFalse`, not `mitigation`.
They also sit in different places: a risk belongs to the work, an assumption
belongs to the **link between two levels** of it. That is why `assumes` sits on
a pathway step and on a success criterion, and never on the manifest as a whole.
In the Early Reading Programme, the step from coaching to fluent readers assumes
that teachers have time in the timetable to use what they learn; if that is
false, coaching alone is the wrong theory.

So `Assumption` is a kind of its own (the same assumption conditions several
steps across several programmes, and stating it once is the argument every other
catalogue here has already had), and the enum value is retired, retyped to
`constraint` on read with a note. Keeping both would have guaranteed half the
assumptions in the risk list and half in the new kind.

## A pathway is not the goal tree

`Goal.parent` makes a three-deep tree, and reading it as a Theory of Change is
free and wrong. The tree is **alignment**: it says where a goal files, allows
exactly one parent, and carries no reason. A Theory of Change says what produces
what, routinely has several outcomes converging on one and one feeding several,
and exists to make the reasoning examinable.

So `Programme.spec.pathway` is a separate edge set, on the programme rather than
the goal, because a goal outlives any one programme's theory of how to reach it
and two programmes may hold different theories about the same goal. Its
`because` is the point: an arrow with nothing written on it is a picture and
cannot be argued with. Advisory, never blocking: a programme without a pathway
is one nobody has thought through yet, which is worth showing rather than
refusing.

The failure this avoids is worse than having no diagram: a tree read as causal
renders something that looks like a Theory of Change and contains none of the
reasoning, and nobody can tell it is empty.

## A tag is for the cut nobody planned

`metadata.labels` is Kubernetes', and so is the reason to leave it
unvalidated: a label is written to make a cut later that nobody anticipated
(which region, whose reporting pack), and the cost of a typo making a new
group is the right way round for that (TAXONOMY.md D11). A fact worth
checking is a property with a name and a check; putting an anticipated fact
in a label loses the check that would have caught it missing.

Three things follow, and they are why this is a rule rather than a field:

- **Labels live on the manifest, not in the spec.** They say how somebody
  finds this thing again among a hundred others, which is not part of what
  it *is*. So the definition store carries them beside the name, and every
  kind gets them at once.
- **A list carries them.** `Summary.labels` is filled by all three store
  adapters, so an index or a picker groups a register without fetching the
  manifest behind every row.
- **The grouping is measured, not configured.** A picker groups on the key
  the most entries carry, ties broken alphabetically, with a control to
  change it. Asking first would be asking about a cut the person came here
  to use, not to design.

The occasion: two dozen measures read as chips; a hundred read as a wall.

## A register with its own shape brings its own add

Every sheet kind can be created from the picker that wanted it, so defining
a project never has to be abandoned to go and add a data source first. KPI
could not: its baseline and target are objects where a sheet's cells hold
values, so it is not a sheet kind, and the picker silently had no add.

The answer is not to bend the measure into a sheet. It is that the picker
asks *what kind is this* and opens that kind's own dialog: the sheet form
for a sheet kind, the measure's own for a measure. The dialog asks for
exactly what the schema requires plus the cycle, because the cycle decides
which periods exist; the baseline, the target and the readings stay on the
measure's own page, where the chart is.

## A built sentence is finished

The parts of a sentence builder are fragments, because that is how they read
under their own headings: *what they cannot do today*, *so they*. The thing
stored is a sentence, and the joiner wrote whatever the concatenation came
to. Most of the sentences built that way were defective: many started
lowercase or trailed off with no stop, and many read *"so Early-grade
learners get help"*, where a register's heading-cased name lands mid-clause
and reads as a defined term.

So the joiners finish what they build:

- **A capital at the front**, unless the first word is an acronym or carries an
  internal capital, which is somebody's spelling rather than a fragment's.
- **A stop at the end**, and never a second one. A question mark is somebody's
  deliberate wording and is left alone.
- **A name reads as a phrase mid-sentence**: *"so early-grade learners get
  help"*. Leading the sentence it keeps its capital, because there it is the
  subject. The limit worth knowing is that a group genuinely named after a
  place would be lowered too, and the answer to that is to write the name the
  way it should read.

And the splitters hand back parts without the sentence's terminal stop, so the
round trip is stable and the field the person types in stays clean.

The guard is a test over the shipped example rather than over fixtures: every
stored sentence is split and rejoined and has to come back identical, and has
to read as a finished sentence. A rule that holds only against sentences
written to prove it holds is not a rule. A generator that builds a vault from a
source document carries the same three rules, so a sentence written there is
not rewritten the moment somebody opens the step.

## A generated view is a test of the flows

The results framework is derived: every cell is read from the manifests when
the page opens, and nothing is stored. That makes it
something better than a report: **it is an audit of the definition flow**, and
the standard is plain: if a project and a programme are
properly defined, the logframe is a generated artifact; if it cannot be
generated, the fault is in the flows.

Running it found exactly that. The matrix's Level column is
read from `KPI.spec.resultLevel` and its indicator rows from `KPI.spec.goals`,
and **neither field was editable anywhere in the interface**. They were set in
the example vault only because they had been written there by hand, which is
the definition of a bolted-on artifact: correct in the demonstration and
unreachable in use. Both are now on the measure's own step, along with the
splits it is reported by, which had the same hole. The grade 3 reading KPI
gets its level (outcome) and its goal from that step, and nowhere else.

The rule: when a view is derived, an empty cell has two possible causes, and
they are not the same thing. The work saying nothing is a real answer and must
be drawn as one. A field no flow asks for is a defect, and the view is where it
becomes visible. Check which one it is before drawing a dash.

The guard is a test that reads the shipped example, derives both matrices and
asserts every column is filled from the manifests alone.

## Store the parts; the sentence is an output

The interface builds a problem, a change and a programme's aim from parts
under their own headings. It once stored what the joiner made of them
(one sentence), and every reader that wanted to edit it split the text back
into parts.

That is a derived value stored as an input, and it cost what derived values
stored as inputs always cost:

- **The subject lived in two places.** `groups` named the beneficiaries and
  the sentence began with their names, so changing the groups had to rewrite
  the text, and did, silently, in a field the person had typed.
- **Splitting was a guess.** Where does a group's name end? The splitter took
  the first candidate that matched, so a single group whose name prefixed a
  multi-group phrase cut the sentence in the wrong place; it took the
  catalogue's capitalisation, so a hand-written line came back changed. Both
  were found in use.
- **Nobody editing YAML had the structure.** The manifest showed a paragraph
  where the interface showed four labelled fields, so the two taught
  different things about what a problem is.

So the manifest holds `problem: {situation, cause}`, `change: {what, gain}`
and `aim: {change, gain}`, and the sentence is composed on the way out:
by the interface, by the Go renderer that writes charters, and by anything
the CLI grows (`cartograph-ui's src/sentence.ts`, `server/internal/sentence`). Splitting
survives in exactly one place: the legacy rewrite that reads a file still
holding a sentence, once, on the way in. The coaching project's problem is
stored as a situation (*they fall behind in reading by grade 2*) and a cause
(*their teachers were never trained to teach it*), with learners in rural
schools as its group; the sentence is put together when it is shown.

Two consequences worth stating, because they are the point:

- **Changing the groups now changes the sentence and not a word anybody
  wrote.** There is nothing to restate.
- **A person editing the file by hand sees what the interface shows.** The
  reason for the change: outputs are deterministic generations
  from inputs, so a tool that wants a different output writes a different
  generator, not a migration.

The general form, which is the rule: **where the interface composes, the
manifest holds the parts.** A composed value in a file is a second copy of
something, and the first thing anybody does with it is try to take it apart
again.
