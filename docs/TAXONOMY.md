# Cartograph taxonomy: what the words mean outside this repository

This file began with a question: should a programme be linkable to projects
only through a shared goal? It should not, and finding out why turned up
several other places where Cartograph's vocabulary and the discipline's
vocabulary had drifted apart.

This file is the reference for any decision about what a kind is.
`DESIGN_RULES.md` says how Cartograph behaves; this says what the nouns mean and
where the definitions come from, so an argument about a kind's shape can be
settled against the discipline rather than against taste.

Cartograph may depart from the standard definition, since it is a capture
tool for an organisation's plans and not an implementation of MSP. A departure
should be deliberate and written down here, not accidental.

---

## The running example

Every example below comes from one case. A ministry of education wants more
children to read well in the early grades. Its goal is *make sure every child
can read with understanding*. Under it sits the objective *raise reading in
the first three grades*, and under that the outcome *children read fluently
by the end of grade 3*. One KPI measures it: *share of grade 3 learners
reading at the expected level*, in percent, read once a year from the
national reading assessment, 38 at the baseline and 60 as the target. The gap
it answers is *reading falls behind by grade 2 and the distance widens
after*, observed in rural and in urban schools.

To close the gap the ministry runs the **Early Reading Programme**. It
coordinates two projects: *coach early-grade teachers in reading*, which
starts in rural schools, and *add a grade 2 reading check*, a short check
every teacher can run so a child who falls behind is seen early. The check
lands in the **national assessment service**, which runs every year with no
end date: an operation. The groups concerned are early-grade learners,
learners in rural schools and early-grade teachers. The work is paid for
from the ministry's reading grant.

---

## The definitions

**Project.** A temporary endeavour producing outputs. Projects produce
outputs; they do not, by themselves, produce benefits.

**Programme.** APM: "the coordinated management of projects and
business-as-usual activities to achieve beneficial change"; a programme is "a
unique and transient strategic endeavour" incorporating related projects and
routine operations. PMI: programmes exist to turn project outputs into
outcomes, coordinating the projects (and sometimes operations) to deliver
benefits and governance no single project could.

What matters is that a programme coordinates the work inside it. That is
a governance relationship, and it only means something if the programme knows
what is inside it.

**Portfolio.** A grouping for investment decisions and return. PMI's test,
which is the sentence that settled this: components "that do not advance
common or complementary goals; do not jointly contribute to the delivery of
common benefits; and/or are related only by common sources of support,
technology, or stakeholders are often better managed as **portfolios** rather
than as programs."

So **shared goals is the test for whether a grouping deserves to be a
programme at all.** It is not the definition of membership in one.

**Benefit.** A measurable improvement resulting from an outcome, perceived as
an advantage by a stakeholder. The organising idea of every programme
standard.

**Operation.** Continuing work with no end date. In MSP and PMI, operations
can sit inside a programme's boundary ("business-as-usual activities", "and
sometimes operations").

In the running example: the coaching and the reading check are projects,
because each ends. The Early Reading Programme is a programme, because it
coordinates both towards one change. The national assessment service is an
operation. The benefit is more children reading at the expected level by
grade 3, as early-grade learners perceive it.

Sources: [APM, What is programme
management](https://www.apm.org.uk/resources/what-is-project-management/what-is-programme-management/);
[PMI, The Standard for Program Management, 5th
ed.](https://www.pmi.org/standards/program-management-fifth-edition); [PMI,
Program and Project Management: Relations, Commonalities,
Differences](https://www.pmi.org/learning/library/program-project-management-relations-commonalities-differences-10591).

---

## How Cartograph maps onto them

| Discipline | Cartograph | Notes |
|---|---|---|
| Project | `Project` | Matches. |
| Programme | `Programme` | Matches, subsidiary programmes included (D32). What it carries is derived below. |
| Portfolio | `Portfolio`, `PortfolioDecisions` | Added by D32, which revisits D3. |
| Benefit | `Goal` + `KeyResult` + `KPI` + `BeneficiaryGroup` | Decomposed, not absent. See D4. |
| Operation | `Operation` | Matches; names its programmes (D2). |
| Blueprint / target operating model | *nothing* | Out of scope: Cartograph captures, it does not design the future state. |
| Tranche | *nothing* | Scheduling, which belongs to the delivery tool. |
| Business case | `Project.spec.funding` + `mandate` | Partial and deliberate. |

---

## What a Programme carries, derived

Not chosen. A programme coordinates work to deliver beneficial change, so it
holds what is *its own* and nothing that belongs to what it coordinates:

| Because a programme is… | it carries |
|---|---|
| a vehicle for beneficial change | `aim`: the change sought |
| answering something wrong | `problems[]`, citing `Gap` |
| judged on benefits realised | `goals[]`, `kpis[]` |
| a coordinating structure | its **components**, derived from the work that names it |
| governed | `leadTeam`, `supportingTeams` |
| exposed at programme level | `risks[]`, including dependencies on other programmes |
| something people have a stake in | a `StakeholderMap` scoped to it |

For the Early Reading Programme: its aim is more children reading fluently by
grade 3; its problem cites the reading gap; it is judged on the grade 3
reading KPI; its components are the two projects and the assessment service,
each of which names it.

Nothing else. Two fields look as though they belong, because a source plan
has headings for them, and they do not:

- `description`: a plan may state an Objective *and* a Description for
  every programme, so the ministry's plan might give the Early Reading
  Programme both. But "what the work does" is the aim plus the components
  inside it; a Description is that document's convention, not a programme's
  property.
- `partners`: a list of collaborating bodies (the colleges that train
  the ministry's teachers, say), which is a stakeholder list under another
  name. `StakeholderMap` already scopes to a programme and accepts any
  `Resource`, external bodies included. Two places to name one party.

The general lesson is that a source document's section headings are not a
schema. The plan is content. Where it states something Cartograph has no field
for, the first question is whether the concept belongs to a programme at all,
not where to put the text. Outputs belong to the components; phases are
delivery; a Rationale is evidence and belongs in the Gap register.

---

## Decisions

### D1. Membership of a programme is declared, not derived. *(resolved)*

*The things inside a programme are its **components**, which is PMI's own word
and what the interface calls the step. "Membership" below is the relation; a
component is the thing.*

A project names the programmes it belongs to (`Project.spec.alignment.programmes[]`);
the programme side is read back from that index. One end declares and the
other derives, as dependencies do. The reading check names the Early
Reading Programme; the programme lists the reading check without saying so
itself.

Sharing a goal **corroborates** membership and a check says so when it is
missing, but it does not constitute it. That check once *blocked*, on the
reading that membership is "proven, not asserted". Three things were wrong
with that:

- A programme's work includes enabling components that serve no
  programme-level goal of their own (a shared migration, such as moving the
  learner records both reading projects use to a new system; a procurement
  vehicle for the reading books). Refusing those refuses a normal part of a
  programme.
- It applied the portfolio criterion (common goals) as a membership gate
  one level down.
- It broke a manifest from outside itself. Editing a programme's goals
  retroactively invalidated every project that named it, so a definition valid
  yesterday blocked today because somebody edited a different file. Nothing
  else in Cartograph does that.

The check is `checkWarn`, and has a test.

### D2. An operation names its programmes. *(resolved)*

`Operation.spec.programmes[]`, mirroring D1 exactly: declared on the
operation, derived on the programme, a plain reference because the link
carries nothing of its own. Both standards put business-as-usual inside the
programme boundary, and without this a programme would answer "what is in
this programme?" with only its projects. The national assessment service
names the Early Reading Programme, and the programme shows it beside the two
projects.

### D3. Cartograph has no portfolio kind, on purpose. *(superseded by D32)*

The top level of a strategic plan does portfolio duty, and it is made of
`Goal`s at the top level, not containers. *Make sure every child can read
with understanding* groups the reading work without holding it. That is
coherent. The goal tree carries the strategic grouping, and the investment
decision a portfolio exists to make (what to fund, what to stop) is a
concern for whoever holds the budget and for the delivery tool, outside the
capture boundary.

**Do not add a Portfolio kind** without revisiting this. If a fourth tier is
ever needed, it is a goal level, not a container.

*Revisited in D32: the goal tree aligns work but holds no decision about
what to fund, and a programme was being asked to stand in for that.*

### D4. There is no Benefit kind, and the parts are already here. *(resolved)*

A benefit is a measurable improvement, perceived as an advantage by someone.
Cartograph holds each part: `Goal` and `KeyResult` say what improvement, `KPI` says
how it is measured over time, `BeneficiaryGroup` says who perceives it, and
`Project.spec.successCriteria` says what counts as enough. For the ministry:
the outcome *children read fluently by the end of grade 3* says what
improvement, the grade 3 reading KPI says how it is measured, early-grade
learners are who perceives it, and the coaching project's success criteria
say what counts as enough.

**Do not add a Benefit kind.** It would duplicate `KPI` and re-open the
question `relatesTo` already settled. If benefits realisation is ever wanted,
it is readings against existing KPIs, not a new noun.

### D5. Subsidiary programmes are not built, and the shape is known. *(resolved by D32)*

PMI lists subsidiary programmes as components. The plans Cartograph captures
run goal → programme → project and do not nest programmes, so this is not
biting. If it ever does: `Programme.spec.programmes[]`, declared on the
child, derived on the parent, same as D1, plus a cycle check, which the
dependency work already needs.

### D6. A programme's checks advise; none of them can block. *(resolved)*

`Programme` has a `Rules` func: the risk list a project and a programme
share, validated without phases, because a programme schedules nothing and a
dependency on one lands by no phase of its own.

It also has `/manifests/Programme/{id}/checks`, and the contract gives it
**no block state at all**. That is the decision, not an omission. Every
question worth asking about a programme is answered by reading a *different*
manifest (the work that names it, the goals it aligns to, the graph it sits
in), which is exactly the case D1 demoted: a definition that saved yesterday
must not be refused today because somebody edited another file. A project's
checks can block because most of them read only the project.

What it asks follows from the definition rather than from the field list:
whether it coordinates anything (derived from the work), whether there is a
change to judge (problems, and whether each names who it lands on and cites
its evidence), whether the benefit can be measured (goals, KPIs), and whether
it sits in a dependency loop. Required fields are not checked, because the
schema already refuses a programme without them, and repeating a refusal as
advice says nothing.

A plan captured from its document starts this way: programmes that
coordinate nothing yet, with no measure and sometimes no problem stated. The
Early Reading Programme, entered from the plan before either project names
it, coordinates nothing, and the check says so. All three are true of a new
capture, and none of them is a reason to refuse a file.

### D7. A budget is a FundingSource, not a Resource. *(resolved)*

The discipline's word is **funding source**: the budget, grant, vote or
facility a piece of work draws money from. PMI's cost management treats it
as part of the funding limit a project is reconciled against; in public
finance it is a line in a chart of accounts, identified by a code (a Head, a
subhead, a fund).

Cartograph had no kind for it, so `Project.spec.funding[].source` was free text
and, in the example instance, somebody had already filed a budget in the
`Resource` catalogue under category `other`, which is what `other` filling
up always means.

**It is not a Resource.** Resource is what a project draws on to do the work:
a role, unit, party, system or facility. Money is what it draws on to pay for
the work, and it carries things none of those do: a code in somebody else's
ledger, the body that holds it, the period it covers. A `budget` category on
Resource would have meant a `code` field that is meaningless for a
person-role.

**The code lives on the source, not on the funding line.** The ministry's
reading grant has a code in the government's ledger, and that code
identifies the grant, not the coaching project's use of it, so it is
written once where it cannot be mistyped per project. That empties the
funding line of free text entirely: amount, currency from the ISO 4217
list, a reference to the source, and a status.

Decided against the alternatives of a `budget` category on Resource and of
keeping a per-project allocation reference.

### D8. Readings are their own file, beside the KPI, not inside it. *(resolved)*

D4 said benefits realisation, if ever wanted, would be "readings against
existing KPIs, not a new noun". This is those readings.

A KPI says what is measured, in what unit, which way is good, from where and
how often. It is written once and rarely changes. A reading says what the
number was in one period, and a new one arrives every cycle forever. The
lifetimes are different, and that is the whole argument. Keeping readings
inside the KPI would make every quarterly number a new version of the
definition, and every diff would read as though the definition had changed
when only the world had. The grade 3 reading KPI is defined once; this
year's 41 percent is a reading.

So `KPIReadings` is a kind of its own, scoped to one KPI the way a
`StakeholderMap` is scoped to one piece of work: `spec.kpi` names it, and
`spec.readings[]` is the series. A plural kind name for a kind that is a
series is honest; what it holds is not one reading.

**Not a Measurement kind, one manifest per reading.** Twenty KPIs read
quarterly for five years is 400 files saying four things each. A series is
one thing that grows, and it belongs in one file.

**One series, kept twice over (2.3).** The series stays one thing a
person reads and edits whole. Each reading is also kept as its own
record, keyed by period, of who recorded what and when (ADR 0013), so
recording one costs the same however long the series is, a restated
figure keeps the one it replaced, and a period can be read as it stood
on any date. That is storage, not a second noun: the manifest is still
the series.

**Which periods exist is derived, not stored.** The KPI names a
`ReportingCycle`, and the cycle's `periodMonths` and `startMonth` say what
the periods are. Storing them again on the readings would be a second place
to change the cycle.

### D9. What a Gap is, and what it is short of. *(resolved)*

A gap is the distance between a current state and a desired state, and
Kaufman's test is the one worth keeping: a need is a gap in **results**, not in
resources or methods, and it is a noun, not a verb. Cartograph's Gap was a
sentence and its provenance: no current state, no desired state, no scope, and
a citation that could only claim the whole of it.

In short, a measured gap references the KPI whose baseline and
target already are its two states; a gap enumerates the `Segment`s it was
observed in; and a citation names which of those segments the work addresses,
so a project addressing one quarter of a gap is recorded as addressing one
quarter of it. A gap's statement is never edited to make this work: where a
register quotes a source, the quotation stands and the segments carry the
precision.

The reading gap references the grade 3 reading KPI, so 38 and 60 are its two
states. It lists two segments, rural schools and urban schools. The coaching
project, which starts in rural schools, cites the rural segment only, and
the record says it addresses that part and no more.

**Goals do not reference Gaps, and should not.** A goal says where we want to
be, a gap says how far we are from it, and where both are quantified they meet
at the measure. A direct edge would carry no fact the KPI does not already
carry, and would break the rule that a goal references nothing below it.

### D10. A unit is a declared thing, and the standard ones ship. *(resolved)*

`KPI.spec.unit` was free text, and free text costs this: `percent` many times
over, plus `score`, `index`, `count`, `USD`, `rate` and `hours`. No two of
them were checkable against each other, and nothing could say that two KPIs
are measured in the same thing.

The answer is a `Unit` kind, which the KPI references. This is Kubernetes'
own shape. The built-ins are not a special case but ordinary objects you are
given, so a standard set ships with every vault and an instance declares its
own beside them with the picker's own add. There is no second code path for a custom
unit, which is the whole point of doing it this way. The grade 3 reading KPI
uses the shipped `percent`; a ministry that scores reading on its own scale
declares a unit for it.

A unit carries the **dimension** it belongs to, from the closed set
`KeyResult.kind` already uses (percent, count, money, ratio, duration), so
the two places Cartograph talks about measurement agree. What a unit is *called*
and what it *is* are then separate: "Hours" and "Days" are both durations,
and a roll-up that has to add two measures can tell whether it may.

**Not an enum.** A closed list would be wrong in a way a vault cannot fix:
every organisation measures something nobody anticipated, and a schema that
refuses it sends the number into a note. Sensible defaults with an open
extension is the answer a closed list cannot give.

`KeyResult.unit` has the same fault and is deliberately not changed here. It
is the same fix, and doing both at once would put two migrations in one
change; noted rather than done.

### D11. Labels are already in the contract, and they are Kubernetes'. *(resolved)*

`metadata.labels` has been on **every** kind since the envelope was written
(a string map, exactly Kubernetes' shape), and nothing had ever read or written
one. It was asked for on KPIs, where a row of twenty chips is the problem
that makes grouping worth having, but it belongs where it already is: on the
envelope, for every kind, because the reason to group KPIs is the reason to
group anything.

**A label is not a field.** The rule Kubernetes settles and Cartograph keeps is
that if a fact belongs to what the thing *is*, it is a property with a name and
a check.
A label is for the cuts somebody wants to make *later* and nobody anticipated:
which region, which office, which reporting pack. The ministry might label
its KPIs by region to compare rural districts; nobody planned that cut when
the KPIs were written. Putting an anticipated fact in a label loses the
check that would have caught it missing; putting an unanticipated one in a
property means editing the contract every time somebody wants a new view.

So labels stay free-form on purpose, and nothing validates their keys. What
they buy is filtering, and what they cost is that a typo is a new group. That
trade is the right way round for a cut nobody planned.

### D12. The goal tree is alignment, not a Theory of Change. *(open)*

`Goal.level` and `Goal.parent` make a three-deep tree, and it is tempting to
read it as a causal pathway: outcome leads to objective leads to goal. It
is not one, and reading it that way produces a diagram that looks like a Theory
of Change and contains none of the reasoning.

- `parent` is **containment**. Its whole description is "Required for
  objectives and outcomes; forbidden for a goal."
- Alignment allows **one** parent. A pathway routinely has one outcome feeding
  several and several converging on one.
- Nothing on the edge says **why** the lower advances the higher, which is the
  part a Theory of Change exists to make examinable.

The tree says *children read fluently by the end of grade 3* files under
*raise reading in the first three grades*. It does not say that coaching
teachers leads to fluent readers, or why.

So a pathway, if it is built, is a **separate edge set** from the goal tree, and
it belongs on the Programme rather than the Goal, because a goal outlives any one
programme's theory of how to reach it, and two programmes may hold different
theories about the same goal.

Assessed against what Cartograph holds, the measurement
apparatus of a results framework is present and strong (every level labelled,
every result naming where it is read, how often, who tracks and who confirms),
and the causal logic of both frameworks is absent. Nothing says what leads to
what, or what has to be true for it to. The evidence that assumptions have
nowhere to go is that `risks[].type` has had an `assumption` value throughout and
risk lists do not use it.

---

## How to use this file

Before adding a kind, a field that links two kinds, or a rule that refuses a
link, check here. If the discipline has a word for what you are building, use
that word and that meaning; if Cartograph needs to depart from it, add a decision
here saying so and why.

### D13. Several preconditions on a pathway step are an "and". *(resolved)*

*A step may rest on more than one outcome, and all of them have to hold.
There is no "or".*

The shape already allowed it (`pathway[].from` is a list, and the shipped
example has a step resting on two), but nothing on the screen said which
reading a list takes, and a picker that accepts several reads as a menu.
The card now says it: *All of these have to
hold: A and B.* In the Early Reading Programme, *children read fluently by
the end of grade 3* rests on *teachers use the coaching in class* and
*teachers see who is falling behind by grade 2*, and both have to hold.

Why not an "or"? A step that holds either way is two theories of how the
change happens, and a programme is working to one of them. Recording both
would mean recording which is being pursued, which is a decision the delivery
tool makes and revisits, not a fact about the change. Where two routes are
genuinely live, they are two steps with the same outcome, each with its own
reasoning and its own assumptions, which reads as what it is, and costs
nothing to add.

The practical consequence is the reason to state this at all. A programme
with several strands converging is exactly the case the field is for.
Picking three preconditions is not a workaround; it is the shape.

---

## D14 to D22: what the standards settle *(resolved)*

Found by reviewing the flows as Lean Six Sigma reviews a process, and
derived from the standards, not chosen. Each decision names the standard it
follows.

- **D14. The first question is whether the work ends.** Work that ends is a
  project or a programme; work that keeps running is an operation (PMI,
  GovS 002, PRINCE2, ITIL). Then: can one team under one sponsor and one budget
  deliver it (MSP)? "New" asks both, by picking. The reading check ends; the
  assessment service it lands in does not.
- **D15. A project can be a component of one other project.** PMI's
  subproject; the World Bank's component under one PDO. `Project.spec.alignment.partOf`,
  declared on the component and read back on the parent (the D1 rule), one
  parent, one level. The parent holds the results framework; components hold
  deliverables and their own key results. A component with a different
  sponsor or funding source from its parent is advised that the standards
  call that a programme. **A results framework sits at project level and a
  theory of change at programme level** (UNSDG, ADB, EU logframe): they are
  two levels, not two flavours of one container. Printing the check booklets
  can be a component of the reading check.
- **D16. Shared enabling work is its own project**, used by others through a
  dependency, and lands as an operation (ITIL). A component has one parent;
  a second user promotes it. Moving the learner records to a new system,
  used by both reading projects, is its own project.
- **D17. The name of a service belongs to the operation.** A project is
  named for the change it makes to that service, and lands in it; the
  operation's team accepts the handover (GovS 002 §6.4.8). Hence *add a
  grade 2 reading check*, landing in the national assessment service.
- **D18. A data source says how records get into it** (`capture`), picked
  from a list. A time-bound target read from a source captured by hand, or
  not decided, is advised.
- **D19. Parts are stored and shown as parts.** No composed sentences; fixed
  answers are picked. A logframe is a table.
- **D20. Practitioners' words; examples from the vault.**
- **D21. A charter at every level** from one five-part skeleton the standards
  share; names, never codes.
- **D22. The charter builds beside the steps; progress is honest; reinforcement
  is short and specific.**

### D23. One vocabulary, the discipline's, everywhere. *(resolved)*

The same thing was called different names on screen,
in the contract and in the charter, and some things were called by words no
standard uses ("Responsible" as a role, "Escalated" with no one escalated
to, "Landing", "Nearness"). A full audit set these, applied in the
contract, the vaults, the interface and the charter at once:

- **Project roles** are the standard project organisation, not RACI
  letters: sponsor (GovS 002 SRO / PRINCE2 executive), manager (project
  manager), teamMember, userRepresentative (PRINCE2 senior user),
  technicalLead, dataOwner and dataCustodian (DAMA-DMBOK), projectSupport
  (PRINCE2), serviceOwner (ITIL; accepts the handover). The old ids (lead,
  owner, custodian, technicalOwner, filer, operationalOwner) are rewritten
  on read.
- **Escalation names whose decision is needed** (`escalate.to`), on
  projects and programmes: escalating is handing a decision up, so it says
  to whom (PRINCE2, GovS 002).
- **A programme has a sponsor** (MSP SRO) and **an operation has a service
  owner** (ITIL), both roles from the Resource catalogue; a team runs it.
- **Phases and steps use the standard nouns**: Initiation, Closure,
  Handover; Objectives, Success criteria, Schedule, Theory of change,
  Governance, Actuals, Service levels. Success criteria fall due at
  closure, at handover, or after handover.
- **Titles and labels are nouns** (Assumptions, not "What we're
  assuming"); subtitles are removed unless they add something; hints sit
  behind a "?".
- **Charter values from a fixed list are chips**: an icon and a word, with
  meaning carried by shape and fill so they print in black and white.

### D24. The strategy is read top-down, and a gap closes into an outcome. *(resolved)*

**The discipline.** Strategic planning states a purpose (vision and mission)
before any goal (balanced scorecard, strategy maps; public-sector plans such
as Scotland's National Performance Framework). Goals and objectives are
aims, written as what the organisation sets out to do; outcomes are changed
states, what will be true (results-based management, logframe: impact,
outcome, output). A need is the distance between current and desired
results (Kaufman), so a gap is only useful once it says which result would
close it.

**What Cartograph does.**
- The `Purpose` kind holds the vision and mission, with their source,
  stated once above every goal (one per workspace, id `default`). It is
  the top of the strategy, versioned and proposed like the goals beneath
  it, and edited at the top of the Strategy view by whoever may edit
  goals. Until 2.7.0 it was `Settings.spec.purpose`; a workspace that
  still holds it there is read from there until a Purpose is saved (ADR
  0020). The Strategy view at `/strategy` reads the tree
  top-down: purpose, each goal with its reason, its objectives, the
  outcomes under each, and the gaps each outcome closes (current state to
  desired state) with the work aligned to it. The board that edits the tree
  moved to `/goals`.
- Goals and objectives are aims and start with a verb; outcomes are
  states. The editor's hint changes with the level.
- Each level is defined by the one thing that tells it from the others,
  since scale and dates do not show in a statement: a goal is a broad
  direction, never finished; an objective is one concrete change, done
  once it is made; an outcome is a fact about people or things once that
  change is made. Defined by scale alone ("broad", "specific", "several
  years"), a decision model placed 6 of 15 sentences at the right level;
  defined this way, 13 of 15 (docs/adr/0023). A definition a model cannot
  apply is taken as one a newcomer cannot apply either.
- `Gap.spec.outcomes` names the outcome goals that would be true once the
  gap is closed. It is the link from a gap to the goals above it; the gap's
  check asks for one.
- `Goal.spec.contributesTo` lets an outcome name other objectives it also
  serves, beyond its parent, each with a reason. A tree keeps one parent
  (the owner); a strategy map needs the cross-links. The reason is required
  in practice because an unexplained link cannot be checked.

**Departure.** None from the discipline. Cartograph does not derive
`contributesTo` from shared projects: two outcomes funded by one project do
not serve each other, so the link is declared (see "membership declared
rather than derived").

**Capturing a published plan.** The vision and mission are quoted verbatim,
with the plan's sections as their source. Goals keep the plan's own verbs
(*make sure*, *raise*). A gap's outcomes are the outcomes of the programmes
the plan pairs with that finding; a gap no programme answers stays unlinked,
and its check says so. In the running example the reading gap names the
outcome *children read fluently by the end of grade 3*, the outcome the
Early Reading Programme works towards.

### D25. Goal, objective, outcome; SMART; the classifications audited. *(resolved)*

**The discipline.** In a results hierarchy only the top rung is a goal:
logframe Goal > Purpose > Outputs, USAID Goal > Development Objective >
Intermediate Result, GPRA Strategic Goal > Strategic Objective >
Performance Goal. A goal is the long-term aim; an objective is a specific
aim under it; an outcome is a changed state. SMART (Doran, 1981) is the
test an aim is written against: Specific, Measurable, Achievable,
Relevant, Time-bound. Results-based guidance (for example CDC's brief on
SMART objectives) keeps the goal broad and makes the objectives SMART.

**What Cartograph does.**
- The levels are `goal`, `objective`, `outcome` (were pillar, strategic,
  functional; "functional" named an organisation's scope, not a result).
  A vault may label them in its plan's words (a plan that calls its goals
  Priorities can say Priority). The kind keeps the id `Goal` for all three;
  the interface never calls an objective or an outcome "a goal". Goals and
  objectives are named as aims (verb first), outcomes as states.
- The tree is the Strategy. The sidebar's separate "Goals" entry is gone;
  the board is Strategy's Arrange view.
- SMART is read, never typed, and shown as five marks on every record,
  filled where met. Specific: a statement with the numbers left to the
  measures. Measurable: a key result or an aligned indicator with a
  target. Achievable: every measure has a baseline (or an admitted
  unknown). Relevant: placed under the right level (a goal under the
  vision or mission) with a rationale. Time-bound: every target dated. It
  applies at every level, including the goal, because SMART goals are what
  people ask for and Doran's own title covers goals. The outcome *children
  read fluently by the end of grade 3* is Measurable through the grade 3
  reading KPI, and Time-bound once its target of 60 carries a date.
- The other classifications were audited and fixed: KPI shown as
  Indicator; Means of verification; success dimensions from Shenhar and
  Dvir (efficiency, impact on the customer, impact on the team, business
  success, preparation for the future) plus compliance; data source
  provenance split from category; classification a fixed scheme; refresh
  `irregular`; `dataSteward`; resource category `orgUnit`; programme
  manager and business change manager on Programme; the stakeholder
  approach derived from Mendelow's grid.

**Departures.** Compliance is kept beside Shenhar and Dvir's five: a
requirement that is met or not is a different shape from a measure. The
kind id stays `KPI` and `Goal`; identifiers are not words a person reads.

**Not yet.** Risk proximity (PRINCE2) and milestones as their own
construct (PMBOK charter); the charter derives dated rows from phases.

*Addendum to D24.* Order and the gap/outcome distinction. A
gap is measured against the purpose (vision, mission, goals), not against
an outcome; outcomes are then written to close prioritised gaps (Kaufman's
needs assessment; problem tree to objective tree; a plan's situational
analysis before its goals). The outcome names the change; a gap's desired
state is where that change should land on one measure, and may be an
ideal that a dated target approaches over several periods. One outcome can
close several gaps. In practice the two used to be revisited together, and
Cartograph let either be recorded first and linked from the gap's side.
D28 settles the order: the outcome is written first, as the state the
purpose calls for, and the gap after it, naming it.

### D26. Attainable, measures first, and each aim's context: owner and horizon. *(resolved)*

**The discipline.** SMART's A is read as Attainable: the target can
reasonably be reached. Goals are set before objectives and cover a longer
horizon (a strategic goal usually spans the plan, three to five years; an
objective one to three years inside it). Measures carry the numbers, so
Measurable is where SMART starts. An aim has someone accountable for it
(GPRA's goal leader). Objective types strategic, tactical and operational
(Anthony, 1965) map onto Cartograph's kinds: strategy objectives, project
objectives and deliverables, and operations with their service levels.

**What Cartograph does.**
- A is Attainable (id `attainable`): every measure has today's figure,
  dated before its target, and each target moves the way its measure says
  (up for increase, down for decrease). Ambition itself is for people to
  judge. The reading KPI's 38 is dated before its 60, and 60 is above 38
  on a measure that should increase.
- Every goal, objective and outcome may name an `owner` (a Resource role,
  never a person) and a `horizon` (start and end, a year or a year and
  month). An objective or outcome without a horizon takes its parent's.
  Time-bound reads each target against the horizon; a check flags a horizon
  that runs outside the one above it, and asks for an owner. The ministry's
  reading objective might be owned by its head of early grades, a role.
- The editor shows the level, horizon, owner and SMART marks together, with
  the usual horizon for the level as a hint, and puts Measures (key results
  and aligned indicators) directly under the statement. The Strategy view
  shows each goal's horizon and, in the side panel, the owner.

**Not adopted.** A goal "type" field (time-bound, outcome-oriented,
process-oriented, as some vendors group goals): it is not a standard and
repeats what SMART, the outcome level and operations already express;
labels cover any local grouping. An objective-type field: the kind already
says it. Team and individual goals: the delivery tool's job.

### D27. People are principals, never definitions; teams are the structure. *(resolved)*

**The discipline.** Role-based access control (NIST's RBAC model,
standardised as ANSI INCITS 359) grants permissions to roles and
assigns people to roles, so a permission never names a person. Least
privilege gives each role only what its work needs. An organisation's
structure is a tree of units, each answerable to the one above, and a
directory (LDAP, Entra ID, any OIDC provider) is where an enterprise
keeps both its people and its groups.

**What Cartograph does.**
- A person is a principal: who signs in, what roles they hold and
  which teams they act for. The access list keeps them (address,
  display name, roles, teams, last sign-in) as operational state, the
  way project state history is kept. A person never becomes a
  manifest, and a definition still names a role, never a person.
- The organisation's structure is the `Team` kind, as it already was:
  a team names its parent, and projects, operations and data sources
  name their team, programmes their lead team. Groups from the
  directory may be enrolled as teams, so the tree in Cartograph is the
  one the organisation already keeps.
- Four roles, each a bundle of permissions. A *reader* sees every
  goal, definition and register, and changes nothing. A *contributor*
  defines the projects, programmes, operations and data sources of
  their own teams and the teams beneath them, keeps the shared
  registers, records readings and hands off a defined project. A
  *strategy editor* shapes the goals, objectives and outcomes. An
  *administrator* does everything the others do, for every team, and
  also keeps the teams, the settings and the vault, recovers removed
  records, and decides who may sign in and what they hold. Roles
  combine; a person with none signs in to nothing.
- In the running example, the ministry's early grades team sits under
  its curriculum division. A contributor in the curriculum division
  may edit *coach early-grade teachers in reading*, which the early
  grades team runs; a contributor in the assessment team may not, but
  may record a reading of the reading KPI.

**Not adopted.** A Person kind: it would put people into definitions,
which D1 to D26 keep to roles, and copy what the directory already
holds. Permissions per manifest (an access list on each project): the
team a manifest names already says whose work it is. Provisioning by
SCIM: enrolment at sign-in covers what Cartograph needs; a SCIM
endpoint is an adapter a deployment may add later.

### D28. The record is a directed acyclic graph, written from the top down. *(resolved)*

**The discipline.** A results chain and a strategy map read in one
direction: purpose, goals, objectives, outcomes, then the measures and
the work that delivers them (logframe, results-based management, the
balanced scorecard's strategy map). Planning walks it top-down: what
the organisation sets out to do comes before what it will do about it.
A reference that goes both ways is a loop, and a loop has no first
step.

**What was wrong.** The links all pointed one way (a gap names its
outcome, a KPI its aims, a project its programme), but the order of
work ran the other way. The plan for an objective started with its gaps,
which name an outcome that did not exist yet, so the gap was saved half
done and opened again once the outcome was written. The worklist settled
every record's definition, then every record's figures, then every
record's links, so each record was visited three times. The graph was
drawn round a ring. People and agents alike went back and forth, and a
new workspace showed no first step at all: "New" asked only about
projects, programmes and operations, with nothing in the strategy for
them to serve.

**What Cartograph does.**
- One order of work, held by the engine (`internal/engine/order.go`) and
  served at `GET /order`: Purpose; Goal at each level (goal, objective,
  outcome); KPI; Gap; Assumption; Programme; Operation; Project;
  StakeholderMap; then KPIReadings, which are a KPI's data. The
  registers (Team, Resource, Unit, ReportingCycle, Segment, DataSource,
  FundingSource, BeneficiaryGroup) are the roots: anything may name
  them, they name only each other, and they are added when a field
  first asks for one.
- **A manifest names only what comes before it**, or one of its own kind
  in a tree (a team's parent, a goal's parent at the level above, a
  project's parent). A test fails the build on a schema reference that
  points downstream. Saving a version refuses one that does, through a
  generic reference such as a risk's dependency, and refuses a reference
  that closes a loop. Until now a loop was advice. The save that closes
  one is now refused. Only a reference being added is refused: one a
  record already held is kept, and an import brings records in as they
  were saved, so no deployed record is broken (VERSIONING.md). A loop
  already in a store is still reported by its check until somebody
  removes it. A programme that waits on a project
  says so from the project's side, with `direction: neededBy`, so
  nothing is lost.
- **The work walks the order once.** What a new thing names exists
  before it is written; what will name it is written after it. The
  guide's plan says which is which (`when`: before or after), and the
  worklist finishes each record in one visit (what it is, its figures,
  its links to what is already there) before the next one down. A check
  that a later record settles by naming an earlier one (an outcome
  waiting for the gap that closes it) is listed where that later record
  is written, so it reads as the next thing to write, not as something
  to go back to.
- **A new workspace starts at its purpose.** "New" shows the stages in
  order, with the one to write now first and the ones still waiting on a
  stage before them marked, and MCP's `next` with no work says the same.
- **The graph is drawn in layers**, the registers in the first band and
  one band per stage below them, so every edge runs upwards
  (`CARTOGRAPH_GRAPH_LAYOUT`, `layered` by default).

**Departure.** Kaufman's needs assessment finds gaps before outcomes are
written (D24's addendum). Cartograph writes the outcome first, as the
state the purpose calls for, and the gap after it, naming it. A finding
recorded before anyone has written the outcome it bears on waits in the
documents until the outcome is written. This is the price of never
opening a finished record again. The discipline's order of discovery is
kept in what the gap says (where things are now, measured against the
purpose); only the order of writing changes.

In the running example, the ministry writes its purpose, the goal *make
sure every child can read with understanding*, the objective under it,
and the outcome *children read fluently by the end of grade 3*. Next it
writes the grade 3 reading KPI that measures the outcome, then the
reading gap, which names both. The Early Reading Programme cites the gap
and is judged on the KPI. The two projects name the programme and the
outcome, and the reading check names the assessment service it lands
in. Nothing written earlier is opened again.

### D29. Every word is defined as a dictionary defines it, where it appears. *(resolved)*

**The problem.** Each kind's summary was written for a reader who already
knew the record: "an indicator: one number, read from a source on a
cycle, with today's figure and a dated target." That is accurate, and a
newcomer cannot use it. It assumes they know what a source, a cycle and a
dated target are, and it describes the shape of the record rather than
the thing. An agent can follow a definition like that. A person meeting
the word for the first time cannot.

**What products do.** Applications with a vocabulary of their own define
it the way a dictionary does, where the word appears. Obsidian's help
defines a vault in one line, as the folder where your notes live. Google
puts a "?" beside a term in its products, and a click opens a sentence
and an example; a glossary page lists every term in one place. The shape
repeats: the word, one sentence in everyday words that does not repeat
it, and one instance of it.

**What Cartograph does.**
- Every kind's `summary`, and every Goal level's entry in `levels`, is a
  dictionary sentence: what it is, in everyday words, without its own
  name, naming no standard, field or identifier. Each has an `example`
  (`levelExamples` for the levels), from the example workspace, that
  shows every part the sentence names: a gap's example gives both how
  things are and how they should be, a team's gives the team it sits
  under. An example is written in words a newcomer knows, so a record's
  own shorthand ("put right") is spelt out ("sorted, repacked or
  cooled"). A test holds the entries to the parts it can check.
- `GET /glossary` serves them in the order of work, the order a person
  meets them, so every interface shows the same definition. The
  discipline's longer `definition` stays in the guide, for agents and
  for anyone who wants the detail.
- The interface shows the entry behind a "?" beside the word wherever a
  kind is introduced (New, the sheets), and lists them all on a Glossary
  page.

In the running example, a newcomer reading "Indicator" sees: *a number you
track over time to see whether an aim is being met*, and the example *the
share of grade 3 learners reading at the expected level*.

### D30. A service is planned before the project that sets it up, and a part is told from a project by who answers for it. *(resolved)*

**The problem.** Two places in the New flow had no honest answer.

A new service. An operation was "a service that already runs", and a
project that set one up named the literal `new` as where it landed. The
service could only be written once it ran, so the finished project had
to be opened again to name it, which is the loop D28 removes. Nothing
recorded that the service was coming.

A bigger piece of work. New asked how big the work was and offered "part
of a bigger project" beside "several pieces of work", with no way to
tell them apart. D15 has the test, but the question did not ask it.

**The discipline.** ITIL's service portfolio keeps a service through its
whole life: in the pipeline while it is decided on and built, live once
in use, retired when withdrawn. A service is defined before it runs,
which is what lets the work that builds it hand over to something. For
the second question, PMI's subproject and the World Bank's component
rest on one test (D15): one accountable person and one budget make one
project, however many parts it has; parts that need their own are
projects of their own, and a group of those steered towards shared aims
is a programme (MSP).

**What Cartograph does.**
- `Operation.spec.status` is `planned`, `running` or `retired`. A
  service without one is running, as every operation saved before 2.7
  is. A new service is written first as planned (what it will do, who
  will own and run it), then the project that sets it up names it as
  where it lands. The service's `service-status` check asks for that
  project while there is none, and once it has handed over, asks for the
  service to be marked running. The interface keeps the walk in order: the
  service is defined in full and saved first, and only then does the end
  of its walk ask whether to start the project that sets it up now or
  later. A running service is recorded as it
  stands, and a change to it later is a project that names it.
- A service waits on no stage of the strategy, so a workspace can
  start by recording the services it runs today.
- `Project.spec.operation: new` is deprecated. It is still read, and its
  `landing-operation` check now advises naming a planned service; it
  blocks nothing it did not block before.
- New asks about work one yes or no at a time, in plain words. Does it
  keep running? Then, does the service run today (running) or is it new
  (planned, then its project)? Does it finish? Then, is one person in
  charge of all of it, with one budget? Yes: is it part of a bigger
  project? A part is a component; otherwise it is a project of its own.
  No: D32 asks whether the projects need one another for the same
  change (a programme) or are grouped to decide what to fund (a
  portfolio). The
  standards' terms (sponsor, results framework, theory of change) stay
  in the glossary, behind the word they explain.
- The glossary defines Component beside Project, and the Project and
  Programme entries say who answers for each, so the two read apart.
  A kind's guidance may define words it holds besides its own name
  (`terms`).

**Recording what exists.** Bringing an organisation's current work into
Cartograph uses the same order and the same questions. A service that
runs today is a running operation. A project already under way is a
project, named as it is, landing in the service it will hand to (planned
if that service is not running yet). Nothing about porting needs a path
of its own.

In the running example, the national assessment service already runs:
it is recorded as running. The ministry decides to add a grade 2 reading
check as a service of its own, so it writes the check as a planned
operation, then the project that sets it up, which names it. Printing
the check booklets, under the same accountable person and budget, is a
component of that project. The coaching project has its own sponsor and
budget, so it is a project in the Early Reading Programme, beside the
check project, and not a part of it.

### D31. A placeholder for people, and strict order for agents. *(resolved)*

**The problem.** The record is written in order (D28): what a record
names exists before it. A person defining a project can reach its
landing step and find the service it hands over to is not defined yet.
Defining it there and then, in a small dialog, writes a half-defined
service out of order; refusing to go on stops the person cold. Neither
is the walk Cartograph means to be.

**What Cartograph does.**
- **A new service starts from New**, and is defined in its own walk:
  planned, then the project that sets it up (D30). No step of another
  record's walk creates one.
- **People may leave a placeholder** where a reference cannot be made
  yet: `metadata.pending`, on any kind, each entry naming the field (a
  JSON pointer), the kind and the name of the record still to be
  defined, and a note. The field stays empty. A version refuses a
  placeholder that stands for no reference of that kind, names the
  wrong kind, or stands for a field already filled. The record's
  `pending` check lists every placeholder, on the step that holds the
  field, until the reference is made, and anything that needs the field
  (a handoff needs the service a project lands in) still waits for it.
  So the person carries on, knowing exactly what to come back to; the
  out-of-flow step is accepted and designed around.
- **Agents never leave one and never write out of order.** A draft in an
  agent's change set that names anything neither in the record nor in
  that change set is refused, saying what to define first; so is a
  placeholder, and so is a reference that points later in the order.
  The guide's plan marks each before item with no record yet as
  `missing`, and the instructions say the order is enforced. An agent
  defines what a record names first, in the same change set, and the
  graph of the order is the context it works against.

In the running example, a ministry officer defining the grade 2 reading
check reaches its landing step before anyone has written the assessment
service it will join. They record the placeholder "national assessment
service", finish the project's other steps, define the service from New,
and come back to name it. An agent asked to do the same defines the
service first.

### D32. A portfolio decides what to fund, a programme brings about one change, and a project delivers. *(resolved)*

**The problem.** New called a programme "several projects, each with
its own person in charge and budget, run together towards one change",
which any cluster of projects passes. Projects grouped because they
share a budget, a sponsor or a strategic objective, but that run apart
and do not need one another, were recorded as programmes, and nothing
in Cartograph could hold the decision those groupings exist to make:
what to fund, in what order, and what to stop. D3 had left that outside
on purpose.

**The discipline.** Three constructs, told apart by their management
purpose and the relationships between what they hold, not by the fact
that things are grouped:

- A **portfolio** is a strategic investment and prioritisation
  construct: projects, programmes and other portfolios, selected,
  prioritised and overseen together against strategic objectives, with
  no causal link required between them (PMI, The Standard for Portfolio
  Management; Axelos MoP). It may hold programmes, projects and other
  portfolios.
- A **programme** coordinates related projects, and sub-programmes, to
  realise outcomes and benefits they share (MSP; PMI, The Standard for
  Program Management). It has a coherent theory of change and results
  framework, linking activities to outputs, outputs to outcomes, and
  outcomes to impact (UNSDG; the EU logframe). Causal coherence and
  shared benefits are what make it a programme.
- A **project** is the delivery unit, producing defined outputs (D14,
  D15).

A grouping that meets neither test is a collection: a descriptive label
that assumes nothing about governance, dependence or shared benefits.
It is how a person organises what they look at, not a fact about the
work, so it is not a kind. A cluster may be informal, a governed
portfolio, or a programme still forming, and the record says which only
once its management purpose does.

**What Cartograph does.**
- `Portfolio` is a kind: the strategic objectives it serves (goals at
  the goal or objective level), the budgets it draws on, and the team
  that governs it. What it holds is declared by what is in it, as
  programme membership is (D1): `Project.spec.alignment.portfolios[]`,
  `Programme.spec.portfolios[]`, and `Portfolio.spec.portfolios[]` for a
  portfolio inside another. Nesting is a tree, and a loop is refused.
- A portfolio's decisions are their own file, `PortfolioDecisions`, as a
  KPI's readings are (D8): for each thing the portfolio holds, a
  priority and a decision (invest, hold or stop), with the reason and
  the date. They change at every review while the portfolio's
  definition does not, and they name what is in the portfolio, which is
  written after it in the order of work (D28). A decision about
  something the portfolio does not hold is advised.
- A programme is held to its theory of change: its `pathway` is what
  makes it one. A programme without a pathway is advised to write one,
  or that it may be a portfolio. Sub-programmes are built in the shape
  D5 recorded: `Programme.spec.programmes[]`, declared on the
  sub-programme, read back on the parent, and a loop refused.
- The order of work places a portfolio after the objectives it serves
  and before the programmes and projects that name it; its decisions
  come after everything they name, with the readings.
- New tells them apart by management purpose, one question at a time,
  for work that finishes. Is one person in charge of all of it, with one
  budget? Then it is a project (and may be a component, D15). If not,
  does each project need the others to bring about the same change?
  Then it is a programme, and its theory of change comes next. If not,
  are they grouped to decide what to fund and in what order? Then it is
  a portfolio. If not, they are a collection.
- Nothing is removed. Every programme and project saved before keeps
  reading and saving; the new advice is advice.

In the running example, the ministry's education plan is a portfolio:
it holds the Early Reading Programme and the school meals project, which
share a budget line and the strategic objective *every child can read
with understanding*, but not a theory of change. The Early Reading
Programme is a programme, because coaching teachers and the grade 2
check both have to hold for children to read fluently by grade 3. The
portfolio's decisions this year: invest in the programme, hold the
meals project until its pilot reports.
