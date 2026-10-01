# Cartograph taxonomy: what the words mean outside this repository

**Written 2026-09-28**, after the Programme Lead asked whether a programme
should be linkable to projects only through a shared goal. It should not, and
finding out why turned up several other places where Cartograph's vocabulary and
the discipline's vocabulary have drifted apart.

This file is the reference for any decision about **what a kind is**.
`DESIGN_RULES.md` says how Cartograph behaves; this says what the nouns mean and
where the definitions come from, so an argument about a kind's shape can be
settled against the discipline rather than against taste.

Where Cartograph departs from the standard definition that is allowed — it is a
capture tool for one ministry, not an implementation of MSP — but the
departure should be **deliberate and written down here**, not accidental.

---

## The definitions

**Project.** A temporary endeavour producing outputs. Projects produce
outputs; they do not, by themselves, produce benefits.

**Programme.** APM: "the coordinated management of projects and
business-as-usual activities to achieve beneficial change"; a programme is "a
unique and transient strategic endeavour" incorporating related projects and
routine operations. PMI: programmes exist to turn project outputs into
outcomes, coordinating the projects — and sometimes operations — to deliver
benefits and governance no single project could.

The load-bearing part: **a programme coordinates the work inside it.** That is
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
| Programme | `Programme` | Matches, minus subsidiary programmes (D5). What it carries is derived below. |
| Portfolio | *no kind* | Deliberate. See D3. |
| Benefit | `Goal` + `KeyResult` + `KPI` + `BeneficiaryGroup` | Decomposed, not absent. See D4. |
| Operation | `Operation` | Matches; names its programmes as of 2026-09-28. |
| Blueprint / target operating model | *nothing* | Out of scope: Cartograph captures, it does not design the future state. |
| Tranche | *nothing* | Scheduling, which belongs to the delivery tool. |
| Business case | `Project.spec.funding` + `mandate` | Partial and deliberate. |

---

## What a Programme carries, derived

Not chosen. A programme coordinates work to deliver beneficial change, so it
holds what is *its own* and nothing that belongs to what it coordinates:

| Because a programme is… | it carries |
|---|---|
| a vehicle for beneficial change | `aim` — the change sought |
| answering something wrong | `problems[]`, citing `Gap` |
| judged on benefits realised | `goals[]`, `kpis[]` |
| a coordinating structure | its **components** — derived from the work that names it |
| governed | `leadTeam`, `supportingTeams` |
| exposed at programme level | `risks[]`, including dependencies on other programmes |
| something people have a stake in | a `StakeholderMap` scoped to it |

Nothing else. Two fields were added on 2026-09-28 by reading the Strategic
Plan's headings rather than this definition, and removed the same day:

- **`description`** — the plan states an Objective *and* a Description for
  every programme, so a field was added for the second. But "what the work
  does" is the aim plus the components inside it; a Description is that
  document's convention, not a programme's property.
- **`partners`** — a list of collaborating bodies, which is a stakeholder
  list under another name. `StakeholderMap` already scopes to a programme
  and accepts any `Resource`, external bodies included. Two places to name
  one party.

**The lesson, and it is the general one:** a source document's section
headings are not a schema. The plan is content. Where it states something
Cartograph has no field for, the first question is whether the concept belongs to
a programme at all — not where to put the text. Outputs belong to the
components; phases are delivery; a Rationale is evidence and belongs in the
Gap register.

---

## Decisions

### D1. Membership of a programme is declared, not derived. *(resolved 2026-09-28)*

*The things inside a programme are its **components**, which is PMI's own word
and what the interface calls the step (2026-09-29). "Membership" below is the
relation; a component is the thing.*

A project names the programmes it belongs to (`Project.spec.alignment.programmes[]`);
the programme side is read back from that index. One end declares, the other
derives — the same rule dependencies follow.

Sharing a goal **corroborates** membership and a check says so when it is
missing, but it does not constitute it. Until 2026-09-28 that check *blocked*,
on the reading that membership is "proven, not asserted". Three things were
wrong with that:

- A programme's work includes **enabling components** that serve no
  programme-level goal of their own — a shared migration, a procurement
  vehicle. Refusing those refuses a normal part of a programme.
- It applied the **portfolio criterion** (common goals) as a membership gate
  one level down.
- It **broke a manifest from outside itself**: editing a programme's goals
  retroactively invalidated every project that named it, so a definition valid
  yesterday blocked today because somebody edited a different file. Nothing
  else in Cartograph does that.

The check is `checkWarn` now, and has a test for the first time.

### D2. An operation names its programmes. *(resolved 2026-09-28)*

`Operation.spec.programmes[]`, mirroring D1 exactly: declared on the
operation, derived on the programme, a plain reference because the link
carries nothing of its own. Both standards put business-as-usual inside the
programme boundary, and until this a programme answered "what is in this
programme?" with only its projects.

### D3. Cartograph has no portfolio kind, on purpose. *(resolved 2026-09-28)*

The Strategic Plan's three pillars do portfolio duty, and they are `Goal`s at
pillar level, not containers. That is coherent: the goal tree carries the
strategic grouping, and the investment decision a portfolio exists to make —
what to fund, what to stop — is a Cabinet and delivery-tool concern, outside
the capture boundary.

**Do not add a Portfolio kind** without revisiting this. If a fourth tier is
ever needed, it is a goal level, not a container.

### D4. There is no Benefit kind, and the parts are already here. *(resolved 2026-09-28)*

A benefit is a measurable improvement, perceived as an advantage by someone.
Cartograph holds each part: `Goal` and `KeyResult` say what improvement, `KPI` says
how it is measured over time, `BeneficiaryGroup` says who perceives it, and
`Project.spec.successCriteria` says what counts as enough.

**Do not add a Benefit kind** — it would duplicate `KPI` and re-open the
question `relatesTo` already settled. If benefits realisation is ever wanted,
it is readings against existing KPIs, not a new noun.

### D5. Subsidiary programmes are not built, and the shape is known. *(deferred)*

PMI lists subsidiary programmes as components. The Strategic Plan is pillar →
programme → project and does not nest, so this is not biting. If it ever does:
`Programme.spec.programmes[]`, declared on the child, derived on the parent,
same as D1 — plus a cycle check, which the dependency work already needs.

### D6. A programme's checks advise; none of them can block. *(resolved 2026-09-28)*

`Programme` has a `Rules` func — the risk list a project and a programme
share, validated without phases, because a programme schedules nothing and a
dependency on one lands by no phase of its own.

It now also has `/manifests/Programme/{id}/checks`, and the contract gives it
**no block state at all**. That is the decision, not an omission. Every
question worth asking about a programme is answered by reading a *different*
manifest — the work that names it, the goals it aligns to, the graph it sits
in — which is exactly the case D1 demoted: a definition that saved yesterday
must not be refused today because somebody edited another file. A project's
checks can block because most of them read only the project.

What it asks follows from the definition rather than from the field list:
whether it coordinates anything (derived from the work), whether there is a
change to judge (problems, and whether each names who it lands on and cites
its evidence), whether the benefit can be measured (goals, KPIs), and whether
it sits in a dependency loop. Required fields are not checked — the schema
already refuses a programme without them, and repeating a refusal as advice
says nothing.

Read against the Strategic Plan vault the day it was built: 28 of 29
programmes coordinate nothing yet, none has a measure, and 7 have no problem
stated. All three are true, and none of them is a reason to refuse a file.

### D7. A budget is a FundingSource, not a Resource. *(resolved 2026-09-29)*

The discipline's word is **funding source**: the budget, grant, vote or
facility a piece of work draws money from. PMI's cost management treats it
as part of the funding limit a project is reconciled against; in public
finance it is a line in a chart of accounts, identified by a code (a Head, a
subhead, a fund).

Cartograph had no kind for it, so `Project.spec.funding[].source` was free text
and, in the example instance, somebody had already filed a budget in the
`Resource` catalogue under category `other` — which is what `other` filling
up always means.

**It is not a Resource.** Resource is what a project draws on to do the work:
a role, unit, party, system or facility. Money is what it draws on to pay for
the work, and it carries things none of those do — a code in somebody else's
ledger, the body that holds it, the period it covers. A `budget` category on
Resource would have meant a `code` field that is meaningless for a
person-role.

**The code lives on the source, not on the funding line.** "Head 26-02-001"
identifies the budget, not this project's use of it, so it is written once
where it cannot be mistyped per project. That empties the funding line of
free text entirely: amount, currency from the ISO 4217 list, a reference to
the source, and a status.

Decided with the Programme Lead, 2026-09-29, against the alternatives of a
`budget` category on Resource and of keeping a per-project allocation
reference.

### D8. Readings are their own file, beside the KPI, not inside it. *(resolved 2026-09-29)*

D4 said benefits realisation, if ever wanted, would be "readings against
existing KPIs, not a new noun". This is those readings.

A KPI says what is measured, in what unit, which way is good, from where and
how often. It is written once and rarely changes. A reading says what the
number was in one period, and a new one arrives every cycle forever. **The
lifetimes are different**, and that is the whole argument: keeping readings
inside the KPI would make every quarterly number a new version of the
definition, and every diff would read as though the definition had changed
when only the world had.

So `KPIReadings` is a kind of its own, scoped to one KPI the way a
`StakeholderMap` is scoped to one piece of work: `spec.kpi` names it, and
`spec.readings[]` is the series. A plural kind name for a kind that is a
series is honest; what it holds is not one reading.

**Not a Measurement kind, one manifest per reading.** Nineteen KPIs read
quarterly for five years is 380 files saying four things each. A series is
one thing that grows, and it belongs in one file.

**Which periods exist is derived, not stored.** The KPI names a
`ReportingCycle`, and the cycle's `periodMonths` and `startMonth` say what
the periods are. Storing them again on the readings would be a second place
to change the cycle.

### D9. What a Gap is, and what it is short of. *(resolved 2026-09-29)*

A gap is the distance between a current state and a desired state, and
Kaufman's test is the one worth keeping: a need is a gap in **results**, not in
resources or methods, and it is a noun, not a verb. Cartograph's Gap today is a
sentence and its provenance — no current state, no desired state, no scope, and
a citation that can only claim the whole of it.

The design, the research behind it and what changed in the building are in
**`GAP_DESIGN.md`** beside this file. In short: a measured gap references the KPI whose baseline and
target already are its two states; a gap enumerates the `Segment`s it was
observed in; and a citation names which of those segments the work addresses,
so a project addressing one quarter of a gap is recorded as addressing one
quarter of it. A gap's statement is never edited to make this work: where a
register quotes a source, the quotation stands and the segments carry the
precision.

**Goals do not reference Gaps, and should not.** A goal says where we want to
be, a gap says how far we are from it, and where both are quantified they meet
at the measure. A direct edge would carry no fact the KPI does not already
carry, and would break the rule that a goal references nothing below it.

### D10. A unit is a declared thing, and the standard ones ship. *(resolved 2026-09-29)*

`KPI.spec.unit` was free text, and the two vaults show what that costs:
`percent` fifteen times, plus `score`, `index`, `count`, `USD`, `rate` and
`hours` — no two of them checkable against each other, and nothing able to
say that two KPIs are measured in the same thing.

A **`Unit` kind**, and the KPI references it. Kubernetes' own shape: the
built-ins are not a special case, they are ordinary objects you are given, so
a standard set ships with every vault and an instance declares its own beside
them with the picker's own add. There is no second code path for a custom
unit, which is the whole point of doing it this way.

A unit carries the **dimension** it belongs to, from the closed set
`KeyResult.kind` already uses — percent, count, money, ratio, duration — so
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

### D11. Labels are already in the contract, and they are Kubernetes'. *(resolved 2026-09-29)*

`metadata.labels` has been on **every** kind since the envelope was written —
a string map, exactly Kubernetes' shape — and nothing has ever read or written
one. It was asked for on KPIs, where nineteen chips in a row is the problem
that makes grouping worth having, but it belongs where it already is: on the
envelope, for every kind, because the reason to group KPIs is the reason to
group anything.

**A label is not a field.** The rule Kubernetes settles and Cartograph keeps: if a
fact belongs to what the thing *is*, it is a property with a name and a check.
A label is for the cuts somebody wants to make *later* and nobody anticipated —
which pillar, whose directorate, which reporting pack. Putting an anticipated
fact in a label loses the check that would have caught it missing; putting an
unanticipated one in a property means editing the contract every time somebody
wants a new view.

So labels stay free-form on purpose, and nothing validates their keys. What
they buy is filtering, and what they cost is that a typo is a new group. That
trade is the right way round for a cut nobody planned.

### D12. The goal tree is alignment, not a Theory of Change. *(open — assessed 2026-09-29)*

`Goal.level` and `Goal.parent` make a three-deep tree, and it is tempting to
read it as a causal pathway: functional leads to strategic leads to pillar. It
is not one, and reading it that way produces a diagram that looks like a Theory
of Change and contains none of the reasoning.

- `parent` is **containment**. Its whole description is "Required for strategic
  and functional goals; forbidden for pillar."
- Alignment allows **one** parent. A pathway routinely has one outcome feeding
  several and several converging on one.
- Nothing on the edge says **why** the lower advances the higher, which is the
  part a Theory of Change exists to make examinable.

So a pathway, if it is built, is a **separate edge set** from the goal tree, and
it belongs on the Programme rather than the Goal: a goal outlives any one
programme's theory of how to reach it, and two programmes may hold different
theories about the same goal.

**`RESULTS_LOGIC.md`** beside this file assesses both frameworks against what
Cartograph holds and plans the remediation. The short version: the measurement
apparatus of a results framework is present and strong — every level labelled,
every result naming where it is read, how often, who tracks and who confirms —
and the causal logic of both frameworks is absent. Nothing says what leads to
what, or what has to be true for it to. The evidence that assumptions have
nowhere to go: `risks[].type` has had an `assumption` value throughout and not
one of the 17 risks across both vaults uses it.

---

## How to use this file

Before adding a kind, a field that links two kinds, or a rule that refuses a
link, check here. If the discipline has a word for what you are building, use
that word and that meaning; if Cartograph needs to depart from it, add a decision
here saying so and why.

### D13. Several preconditions on a pathway step are an "and". *(resolved 2026-09-29)*

*A step may rest on more than one outcome, and all of them have to hold.
There is no "or".*

The shape already allowed it — `pathway[].from` is a list, and the shipped
example has a step resting on two — but nothing on the screen said which
reading a list takes, and a picker that accepts several reads as a menu
(Programme Lead, 2026-09-29). The card now says it: *All of these have to
hold: A and B.*

**Why not an "or".** A step that holds either way is two theories of how the
change happens, and a programme is working to one of them. Recording both
would mean recording which is being pursued, which is a decision the delivery
tool makes and revisits, not a fact about the change. Where two routes are
genuinely live, they are two steps with the same outcome, each with its own
reasoning and its own assumptions — which reads as what it is, and costs
nothing to add.

The practical consequence, which is the reason to state this at all: a
programme with several strands converging is exactly the case the field is
for. Picking three preconditions is not a workaround; it is the shape.

---

## D14 to D22: from the Lean Six Sigma review *(resolved 2026-09-29)*

Derived from the standards, not chosen. The reasoning and sources are in
**`LSS_REVIEW.md`** and **`research/STANDARDS.md`** beside this file; this is
the short form.

- **D14. The first question is whether the work ends.** Work that ends is a
  project or a programme; work that keeps running is an operation (PMI,
  GovS 002, PRINCE2, ITIL). Then: can one team under one sponsor and one budget
  deliver it (MSP)? "New" asks both, by picking.
- **D15. A project can be a component of one other project.** PMI's
  subproject; the World Bank's component under one PDO. `Project.spec.alignment.partOf`,
  declared on the component and read back on the parent (the D1 rule), one
  parent, one level. The parent holds the results framework; components hold
  deliverables and their own key results. A component with a different
  sponsor or funding source from its parent is advised that the standards
  call that a programme. **A results framework sits at project level and a
  theory of change at programme level** (UNSDG, ADB, EU logframe): they are
  two levels, not two flavours of one container.
- **D16. Shared enabling work is its own project**, used by others through a
  dependency, and lands as an operation (ITIL). A component has one parent;
  a second user promotes it.
- **D17. The name of a service belongs to the operation.** A project is
  named for the change it makes to that service, and lands in it; the
  operation's team accepts the handover (GovS 002 §6.4.8).
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

### D23. One vocabulary, the discipline's, everywhere. *(resolved 2026-09-30)*

The Programme Lead found the same thing called different names on screen,
in the contract and in the charter, and some things called by words no
standard uses ("Responsible" as a role, "Escalated" with no one escalated
to, "Landing", "Nearness"). A full audit (`research/` beside this file,
`taxonomy-audit.md` in the session notes) set these, applied in the
contract, both vaults, the interface and the charter at once:

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

### D24. The strategy is read top-down, and a gap closes into an outcome. *(resolved 2026-09-30)*

**The discipline.** Strategic planning states a purpose (vision and mission)
before any goal (balanced scorecard, strategy maps; public-sector plans such
as Scotland's National Performance Framework). Goals and objectives are
aims, written as what the organisation sets out to do; outcomes are changed
states, what will be true (results-based management, logframe: impact,
outcome, output). A need is the distance between current and desired
results (Kaufman), so a gap is only useful once it says which result would
close it.

**What Cartograph does.**
- `Settings.spec.purpose` holds the vision and mission, with their source,
  stated once above every goal. The Strategy view at `/` reads the tree
  top-down: purpose, each goal with its reason, its objectives, the
  outcomes under each, and the gaps each outcome closes (current state to
  desired state) with the work aligned to it. The board that edits the tree
  moved to `/goals`.
- Pillar and strategic goals are aims and start with a verb; outcomes are
  states. The editor's hint changes with the level.
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

**Strategic Plan vault.** The vision and mission are verbatim (Sections 2.2
and 2.3). Pillar aims use the plan's own verbs (ensure, empower,
transform). A gap's outcomes are the outcomes of the programmes the plan
pairs with that finding; 17 of 29 gaps link, and the other 12 stay
unlinked because no programme answers them.

### D25. Goal, objective, outcome; SMART; the classifications audited. *(resolved 2026-09-30)*

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
  A vault may label them in its plan's words (the Strategic Plan says
  Pillar). The kind keeps the id `Goal` for all three; the interface never
  calls an objective or an outcome "a goal". Goals and objectives are
  named as aims (verb first), outcomes as states.
- The tree is the Strategy. The sidebar's separate "Goals" entry is gone;
  the board is Strategy's Arrange view.
- SMART is read, never typed, and shown as five marks on every record,
  filled where met. Specific: a statement with the numbers left to the
  measures. Measurable: a key result or an aligned indicator with a
  target. Achievable: every measure has a baseline (or an admitted
  unknown). Relevant: placed under the right level (a goal under the
  vision or mission) with a rationale. Time-bound: every target dated. It
  applies at every level, including the goal, because the Programme Lead
  asked for goals to be SMART and Doran's own title covers goals.
- The other classifications were audited (research/CLASSIFICATION_AUDIT.md)
  and fixed: KPI shown as Indicator; Means of verification; success
  dimensions from Shenhar and Dvir (efficiency, impact on the customer,
  impact on the team, business success, preparation for the future) plus
  compliance; data source provenance split from category; classification
  a fixed scheme; refresh `irregular`; `dataSteward`; resource category
  `orgUnit`; programme manager and business change manager on Programme;
  the stakeholder approach derived from Mendelow's grid.

**Departures.** Compliance is kept beside Shenhar and Dvir's five: a
requirement that is met or not is a different shape from a measure. The
kind id stays `KPI` and `Goal`; identifiers are not words a person reads.

**Not yet.** Risk proximity (PRINCE2) and milestones as their own
construct (PMBOK charter); the charter derives dated rows from phases.

*Addendum to D24 (2026-09-30).* Order and the gap/outcome distinction. A
gap is measured against the purpose (vision, mission, goals), not against
an outcome; outcomes are then written to close prioritised gaps (Kaufman's
needs assessment; problem tree to objective tree; a plan's situational
analysis before its goals). The outcome names the change; a gap's desired
state is where that change should land on one measure, and may be an
ideal that a dated target approaches over several periods. One outcome can
close several gaps. In practice the two are revisited together, which is
why Cartograph lets either be recorded first and linked from the gap's side.

### D26. Attainable, measures first, and each aim's context: owner and horizon. *(resolved 2026-09-30)*

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
  judge.
- Every goal, objective and outcome may name an `owner` (a Resource role,
  never a person) and a `horizon` (start and end, a year or a year and
  month). An objective or outcome without a horizon takes its parent's.
  Time-bound reads each target against the horizon; a check flags a horizon
  that runs outside the one above it, and asks for an owner.
- The editor shows the level, horizon, owner and SMART marks together, with
  the usual horizon for the level as a hint, and puts Measures (key results
  and aligned indicators) directly under the statement. The Strategy view
  shows each goal's horizon and, in the side panel, the owner.

**Not adopted.** A goal "type" field (time-bound, outcome-oriented,
process-oriented, as some vendors group goals): it is not a standard and
repeats what SMART, the outcome level and operations already express;
labels cover any local grouping. An objective-type field: the kind already
says it. Team and individual goals: the delivery tool's job.
