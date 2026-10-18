# Kinds of record

Every record in a Cartograph workspace has a kind. This page lists the kinds, one entry each, in the same shape. Each entry gives what the kind is, what it holds (named as the interface names it), what it links to, where you edit it, and an example from the produce cooperative in `cartograph-engine/examples/minimal`.

This page describes. To learn by doing, see [the tutorial](../tutorials/charter-your-first-project.md). For why the kinds are cut this way, see [About purpose, goals, objectives and outcomes](../explanation/about-purpose-goals-objectives-and-outcomes.md).

## How to read an entry

| Line | Meaning |
|---|---|
| **Kind** | The identifier the contract and the command line use. |
| **What it is** | One line. |
| **What it holds** | The labels on its screens or form. Where a kind is edited on screens, the screens come first. |
| **Links to** | Other kinds it names, and kinds that name it. |
| **Edit it in** | The place in the interface. |
| **Example** | A record in the cooperative's vault. |

"Sheets" is the rail item that opens a list of the simpler kinds, each as one form. The rail items are Graph, Home, Define, Work, Strategy, Goals, Projects, Portfolios, Programmes, Operations, Gaps, Indicators, Changes, Sheets and Glossary.

An organisation can rename the three levels of aim and the word for a project in Settings. The names below are the defaults.

## Strategy

### Purpose

- **Kind:** `Purpose`
- **What it is:** Why your organisation exists: a vision of where it wants to get to and a mission saying what it does to get there.
- **What it holds:** Organisation name, Vision, Mission, Source, Logo.
- **Links to:** Nothing. Goals are checked against it.
- **Edit it in:** Strategy, then **State the purpose** (or **Edit the purpose** once it is set).
- **Example:** Vision: every member family earns a fair living from what it grows. Mission: we collect, check and sell our members' produce together.

### Goal, Objective, Outcome

- **Kind:** `Goal`. One kind with three levels.
- **What it is:** Something your organisation sets out to achieve. A goal is a broad direction, an objective is one concrete change made for a goal, and an outcome is what is true once an objective works.
- **What it holds:** A name, its level, and the place it sits under. The contract's steps are Aim, Measures, Owner and horizon, Rationale and Also leads to. Strategy shows Rationale, Gaps and Work for each, and marks the SMART tests it has not yet met (Specific, Measurable, Attainable, Relevant, Time-bound).
- **Links to:** The aim it sits under (an objective names its goal, an outcome its objective); the gaps an outcome closes; the outcomes projects, programmes and portfolios serve; indicators that measure it.
- **Edit it in:** Strategy (select a row, then **Open**), Goals, and **Arrange** on Strategy.
- **Example:** Goal: Raise produce quality. Objective: Hold every depot to one standard. Outcome: Depot staff apply the standard alike.

## Evidence

### Gap

- **Kind:** `Gap`
- **What it is:** The difference between how things are today and how they should be, in results.
- **What it holds:** Current state, Desired state, Quote, Indicator, Affected groups, Outcomes, Segments, Source, Data source, Data sources, Note. Its steps are Shortfall, Scope and Evidence.
- **Links to:** The indicator that tracks it; the outcomes that close it; beneficiary groups and segments it affects; data sources. Named by the problems of projects and programmes.
- **Edit it in:** Gaps, or Sheets, then Gaps.
- **Example:** Faults are found after dispatch. Today the depot finds one fault in five and buyers find the rest; the aim is for the depot to find most faults before produce leaves.

### Assumption

- **Kind:** `Assumption`
- **What it is:** Something your plans rely on that you expect to be true but have not proved.
- **What it holds:** What has to be true, How sure, If it is false, How to check it, Note.
- **Links to:** Named by a step in a programme's theory of change and by a project's success criterion.
- **Edit it in:** Sheets, then Assumptions.
- **Example:** Produce that fails a check at the depot can be sorted, repacked or cooled the same day instead of thrown away.

### Beneficiary group

- **Kind:** `BeneficiaryGroup`
- **What it is:** A group of people your work is meant to help. It says who they are, not how many.
- **What it holds:** Name, Description, Source.
- **Links to:** Named by projects (their beneficiaries) and by programme problems. Gaps name the groups they affect.
- **Edit it in:** Sheets, then Beneficiary groups.
- **Example:** Depot staff.

### Segment

- **Kind:** `Segment`
- **What it is:** A way of splitting up the people, places or things your work is about, so you can compare the parts.
- **What it holds:** Name, Part of, Description.
- **Links to:** Gaps and indicators name the segments they split by. A segment can sit inside another.
- **Edit it in:** Sheets, then Segments.
- **Example:** Region, with each growing region under it.

## Measures

### Indicator

- **Kind:** `KPI`
- **What it is:** A number you track over time to see whether an aim is being met.
- **What it holds:** Its pages are Definition, Metric and Actuals. The contract's steps are Definition, Result, Baseline and target, How to check, Breakdown, How it is computed and Holding the gain.
- **Links to:** A unit, a reporting cycle, the data sources it is read from, the goals it measures, segments it is split by. Gaps name the indicator that tracks them. It has one list of readings.
- **Edit it in:** Indicators.
- **Example:** Quality pass rate: 62 percent today, aiming for 75 percent by June 2027.

### Readings

- **Kind:** `KPIReadings`
- **What it is:** The figures an indicator actually recorded, one for each reporting period.
- **What it holds:** A table with Period, Reading and Provisional. A period with no reading has not been read yet; it is not zero.
- **Links to:** The indicator it belongs to.
- **Edit it in:** Indicators, then the Actuals page of the indicator.
- **Example:** Quality pass rate: 63 percent in November 2025, 64.5 in February 2026, 66 in May 2026.

### Unit

- **Kind:** `Unit`
- **What it is:** What a number is counted in, such as percent, hours or tonnes.
- **What it holds:** Name, Symbol, Kind of quantity, Description.
- **Links to:** Named by indicators.
- **Edit it in:** Sheets, then Units.
- **Example:** Hours, as in the hours between produce arriving at a depot and being collected.

### Reporting cycle

- **Kind:** `ReportingCycle`
- **What it is:** How often numbers are checked and when each report is due.
- **What it holds:** Name, Period (in months), Period in days, First day, Start month, Named periods, Due offset.
- **Links to:** Named by indicators.
- **Edit it in:** Sheets, then Reporting cycles.
- **Example:** Monthly Cycle. The cooperative also has a Seasonal Cycle and an Annual Cycle.

### Data source

- **Kind:** `DataSource`
- **What it is:** Where a number comes from, such as a system, a register, a ledger or a survey.
- **What it holds:** Name, Category, Provenance, Team, Kept by, Access, Refresh, How records get in, Classification, Quality issues, One entry is, Metrics. Only the team is required.
- **Links to:** The team that uses it. Named by indicators, key results, gaps and success criteria.
- **Edit it in:** Sheets, then Data sources, as one form.
- **Example:** Sales Ledger, where member season earnings are read from.

## Work

### Portfolio

- **Kind:** `Portfolio`
- **What it is:** A group of projects, programmes and other portfolios that your organisation chooses between and funds against its strategy. What it holds need not depend on one another.
- **What it holds:** Its pages are Aim, Strategy, Contents and Governance. The scrubber names the third page Holds. It also shows Budgets, Review cycle and Part of.
- **Links to:** The goals and objectives it ranks against, the team that governs it, its budgets (see Budget), and the portfolios it sits inside. Projects, programmes and smaller portfolios name the portfolios they belong to. What a budget gives one project is an Allocation, recorded on the project's side.
- **Edit it in:** Portfolios.
- **Example:** Cooperative Investment Portfolio.

### Portfolio decisions

- **Kind:** `PortfolioDecisions`
- **What it is:** What a portfolio decided about each project, programme or portfolio in it at its latest review.
- **What it holds:** For each: Priority, Decision (Invest, Hold or Stop), Reason, Decided.
- **Links to:** The portfolio.
- **Edit it in:** Portfolios, then the Contents page, then **Save decisions**.
- **Example:** Invest in the Quality Improvement Programme first; hold the Market Access Programme until its theory of change is written.

### Programme

- **Kind:** `Programme`
- **What it is:** Projects run together to bring about one change.
- **What it holds:** Its pages, in the order the interface shows them, are Aim, Problems, Alignment, Theory of change, Risks, Components, Stakeholders and Governance.
- **Links to:** Goals, portfolios, indicators, its lead and supporting teams, and smaller programmes. Projects and operations name the programme they belong to.
- **Edit it in:** Programmes.
- **Example:** Quality Improvement Programme, which runs the Quality Check Rollout towards one quality standard.

A programme's **components** are the work it depends on: projects, operations and other programmes. A component is not a kind of record. It is a project, operation or programme that the programme names, and the Components page reads the link back.

### Project

- **Kind:** `Project`
- **What it is:** Work with a start and an end that makes a change, then hands what it built to the people who will run it. One team, one sponsor, one budget.
- **What it holds:** Eight stages: Context, Problem, Objectives, Governance, Scope and Deliverables, Plan, Success and handover, Approval. The steps within them are Alignment, Beneficiaries, Problem, Objectives, Resources, Stakeholders, Scope, Deliverables, Schedule, Costs and procurement, Data, Risks, Success criteria, Handover and Approval.
- **Links to:** The team that runs it; goals, programmes and portfolios it serves; resources; beneficiary groups; gaps; indicators; data sources; budgets; the operation that takes over. It also links to a stakeholder map. Success criteria name assumptions.
- **Edit it in:** Projects, then a stage. **Charter** in a stage's header opens the charter. **Hand off** is on the project's record page.
- **Example:** Quality Check Rollout, which gets every depot checking deliveries against one standard, then hands the checks to the Quality Check Service.

### Operation

- **Kind:** `Operation`
- **What it is:** Work that keeps running with no end date, such as a service you provide every day.
- **What it holds:** Its pages are Service, Alignment and Service levels. The contract's steps add Data and Mandate.
- **Links to:** Its team, its owner, the programmes it is part of, its indicators. A project names the operation that takes over its result.
- **Edit it in:** Operations.
- **Example:** Collection Service, which moves produce from every depot to the buyer, every day of the season.

## People and money

### Team

- **Kind:** `Team`
- **What it is:** A group of people in your organisation who own and carry out work. Teams sit under other teams.
- **What it holds:** Name, Parent team, Description.
- **Links to:** Its parent team. Named by projects, operations, data sources and programmes.
- **Edit it in:** Sheets, then Teams.
- **Example:** Depot Network Team, under the Operations Team.

### Resource

- **Kind:** `Resource`
- **What it is:** Something work needs to get done: a role someone fills, an organisational unit, an outside party, a system, a facility or a governance body. Never a named person.
- **What it holds:** Name, Category (Role, Organisational unit, External party, System, Facility, Governance body or Other), Description.
- **Links to:** Named by projects as their roles and by stakeholder maps.
- **Edit it in:** Sheets, then Resources.
- **Example:** Data analyst (a role), analytics partner (an external party), cold store (a facility).

### Budget

- **Kind:** `FundingSource`
- **What it is:** A pot of money that pays for work, such as a budget line or a grant.
- **What it holds:** Name, Code, Owner, Period, Note.
- **Links to:** Named by project costs and by allocations.
- **Edit it in:** Sheets, then Budgets.
- **Example:** Cooperative operating budget.

### Allocation

- **Kind:** `Allocation`
- **What it is:** What a budget gives one project, as the budget's holder decides it.
- **What it holds:** Name, Budget, Project, Amount, Currency, Decision, Decided on, Note.
- **Links to:** The budget and the project.
- **Edit it in:** Sheets, then Allocations.
- **Example:** The operating budget allocates USD 42,000 to the Quality Check Rollout, approved on 20 August 2025.

### Stakeholder map

- **Kind:** `StakeholderMap`
- **What it is:** The people and organisations who care about a project, programme or operation, and how much say each has.
- **What it holds:** Its scope, then its stakeholders. The contract's steps are Scope, Stakeholders and Influence and interest.
- **Links to:** The work it covers, and the resources and beneficiary groups it lists.
- **Edit it in:** The Stakeholders step of a project or programme.
- **Example:** Quality Check Rollout stakeholders: the analytics partner cares a lot and has some say; the transport contractor has little of either.

## Workspace

### Settings

- **Kind:** `Settings`
- **What it is:** How the workspace is set up, such as what it calls each level of aim.
- **What it holds:** The names of the three levels of aim and of a project, the author name used when no one is signed in, the browser used to make PDFs, the workspace's own words, and its change control.
- **Links to:** Nothing.
- **Edit it in:** Access, in **Vocabularies** and **Change control**. Both show only to administrators.
- **Example:** None in the cooperative's vault.
