# Programme and portfolio fields

Every field of a programme, a portfolio and a portfolio's decisions, in the order of the screens. Each field has the same four parts: the label you see, what it holds, whether it is required, and its limits or choices.

This page describes. To do the work, see [Set up a portfolio](../how-to/set-up-a-portfolio.md), [Put a project or programme in a portfolio](../how-to/put-a-project-or-programme-in-a-portfolio.md) and [Review a portfolio and record the decisions](../how-to/review-a-portfolio-and-record-the-decisions.md). For why they are shaped this way, see [About portfolio decisions](../explanation/about-portfolio-decisions.md). For the other kinds, see [Kinds of record](kinds-of-record.md).

"Required" means required to merge. Everything else is optional, and a check may still advise you to fill it in. A person is never a value in any of these fields: roles and teams are chosen from the workspace.

## Programme

A programme has eight steps, shown in this order in the interface: Aim, Problems, Alignment, Theory of change, Risks, Components, Stakeholders and Governance. The step before them, *New programme*, asks for a **Name** and a **Lead team**, and its button reads **Start**.

### Aim

| Label | What it holds | Required | Limits or choices |
|---|---|---|---|
| **Name** | The programme's name. | Yes | |
| **Short reference** | A short name to quote in reports and meetings. | No | |
| **Intended change** | How things will be once the programme has worked. | Yes | Up to 600 characters. |
| **Benefit** | Who is better off, and how. | No | Up to 600 characters. A programme that quotes a published aim may have none. |
| **Source** | Where the programme is set out, precisely enough to find again. | No | Up to 160 characters. |

### Problems

One card for each problem the programme answers. The button is **Add a problem**.

| Label | What it holds | Required | Limits or choices |
|---|---|---|---|
| **Affected groups** | The beneficiary groups who feel the problem. | No | Chosen from the workspace's beneficiary groups. |
| **Problem** | What is going wrong for those groups now. | Yes | |
| **Potential cause** | What you think lies behind the problem. | No | |
| **Intended change** | What will be different once the change has worked. | Yes | |
| **Benefit** | What the groups gain. | No | |
| **Gaps** | The gaps this problem is part of. | No | Chosen from the gap register. **Which parts of** narrows a gap to some of its segments. Leave it empty to claim the whole gap. |

### Alignment

| Label | What it holds | Required | Limits or choices |
|---|---|---|---|
| **Outcomes** | The goals the programme is judged on, at any level. | No | Chosen from Strategy. |
| **Indicators** | The indicators the programme moves. | No | Chosen from the workspace's indicators. |
| **Part of programmes** | The larger programmes this one is part of. | No | Use it only when this programme shares the larger one's outcomes and theory of change. A loop is refused. |
| **Portfolios** | The portfolios that select and fund the programme. | No | Chosen from the workspace's portfolios. |

### Theory of change

One card for each step of the pathway, starting from the outcome. The buttons are **Add another outcome** and **Add what has to be true first**.

| Label | What it holds | Required | Limits or choices |
|---|---|---|---|
| **Outcome** | The outcome the programme is for. Each earlier step is labelled **Precondition**: an outcome that has to be true first. | Yes | Chosen from Strategy, so an outcome is the same thing an indicator can measure. |
| **Because** | Why the preconditions lead to this outcome. | No | Up to 400 characters. |
| **Assumptions** | What has to be true for this step to work. | No | Chosen from the workspace's assumptions, or recorded on the spot. Things you will manage go under Risks. |
| **Also needs** | A precondition already on the pathway that this step also rests on. | No | Several preconditions on one step are read as all of them being needed. |

### Risks

A grid of impact against likelihood, and a list. The button is **Add**.

| Label | What it holds | Required | Limits or choices |
|---|---|---|---|
| **Type** | What kind of entry it is. | Yes | Risk, Issue, Dependency or Constraint. |
| **Description** | What it is. | Yes | Up to 160 characters. |
| **Impact** | How bad it would be. | No | Low, Medium or High. |
| **Likelihood** | How likely it is. | No | Low, Medium or High. |
| **Mitigation** | What reduces it. | No | Up to 160 characters. |
| **Owned by** | The role that manages it day to day. | No | A role, never a person. |
| **Needs a decision above the project** | Whether it must go up. | No | If on, say what is needed in **Decision needed**, and from whom in **Decision needed from**. |
| **Direction**, **Linked to** | For a dependency, which way it points and what it points at. | Only for a Dependency | Waits on, or Waited on by. |

### Components

The projects, operations and programmes the programme depends on, and why. The step shows a table of what is in the workspace, a filter, and for each one a box saying what the programme needs from it. Components are declared here and read back on each component, and one component may serve many.

| Label | What it holds | Required | Limits or choices |
|---|---|---|---|
| **Chosen** | The projects and programmes this one depends on. | No | A chain that comes back to where it started is refused. |
| **Used by** | What depends on this programme. | Read only | Shown for each component. |
| **Related work** | Work that shares an outcome with the programme but is not part of it. | Read only | Open one to add it. |

### Stakeholders

A grid headed **Influence and interest**. It is a record of its own, a stakeholder map, saved separately from the programme. See [Kinds of record](kinds-of-record.md).

### Governance

| Label | What it holds | Required | Limits or choices |
|---|---|---|---|
| **Sponsor** | The role accountable for the programme and its benefits. | No | A role from the workspace, never a person. |
| **Programme manager** | The role that runs it day to day and coordinates its projects. | No | A role. |
| **Business change manager** | The role that keeps the change in use after projects hand over and makes sure the benefits arrive. | No | A role. |
| **Lead team** | The team that leads the programme. | Yes | A team. |
| **Supporting teams** | Other teams that carry out its work. | No | Teams. |
| **Escalation route** | Who an issue goes to when it cannot be settled here, nearest first. | No | Add each next step up in order. Leave out groups that only receive reports. |

A programme record also has a mandate, a list of the documents that authorise it. The programme screens do not show it.

## Portfolio

A portfolio has four steps: Aim, Strategy, Contents and Governance. The scrubber names the third one **Holds** and its page is headed **Contents**. The step before them, *New portfolio*, asks for a **Name** and **Governed by**, and its button reads **Create**.

### Aim

| Label | What it holds | Required | Limits or choices |
|---|---|---|---|
| **Name** | The portfolio's name, as people refer to it. | Yes | Up to 160 characters. |
| **What it invests in, and why** | The strategic purpose its choices serve, in a sentence. | Yes | Up to 400 characters. It is not a change the portfolio brings about; that is a programme's aim. |

### Strategy

| Label | What it holds | Required | Limits or choices |
|---|---|---|---|
| **Strategic objectives** | The goals and objectives the portfolio ranks work against. | No, but a check advises it | Chosen from Strategy. Outcomes cannot be chosen. |

### Contents

The projects, programmes and portfolios that name this portfolio. The step has no field of its own. It reads membership back, and holds the decisions described under *Portfolio decisions* below. To add something, see [Put a project or programme in a portfolio](../how-to/put-a-project-or-programme-in-a-portfolio.md).

### Governance

| Label | What it holds | Required | Limits or choices |
|---|---|---|---|
| **Governed by** | The team that selects what goes in, sets the order and decides. | Yes | A team, never a person. |
| **Budgets** | The budgets the portfolio hands out. | No | Chosen from the workspace's budgets. Things in the portfolio may draw on their own as well. |
| **Review cycle** | How often the portfolio's decisions are reviewed. | No | Chosen from the workspace's reporting cycles. Without one, the decisions' own dates say when they were last made. |
| **Part of** | The larger portfolios this one sits inside. | No | Chosen from the other portfolios. A portfolio cannot contain itself, and a loop is refused. |

### Checks

| Check | What it says |
|---|---|
| Strategic objectives | *No strategic objective yet: nothing to select and prioritise against.* |
| Contents | *Nothing names this portfolio yet, so it holds nothing to decide on.* |
| Decisions | *No decision yet on* each thing held: invest, hold or stop. |

## Portfolio decisions

One record for each portfolio, named for it (*Cooperative Investment Portfolio decisions* is the pattern; the cooperative's own is named *2025 season review*). It lives on the **Contents** step. For each project, programme or portfolio the portfolio holds there is one entry.

| Label | What it holds | Required | Limits or choices |
|---|---|---|---|
| Item | The project, programme or portfolio decided on. Its name links to it. | Yes | A decision about something that does not name this portfolio is advised against. |
| **Invest**, **Hold**, **Stop** | The decision. | Yes | One of the three. Invest funds it and lets it go on. Hold commits nothing more until the next review. Stop ends it and releases what it holds. |
| **Priority** | Its rank. | No | A whole number from 1. 1 is first. Two items may share a rank. |
| **Reason** | Why. | No | Up to 400 characters. |
| Decided | The date of the decision. | Set for you | Today's date, put on each decision you change. It is not a box you fill in. |

**Save decisions** saves every entry on the page together, as one review, in the change set you are working in.

## Related records

The budgets a portfolio hands out are *Budgets*, and what a budget gives one project is an *Allocation*. Both are described in [Kinds of record](kinds-of-record.md). Allocations are recorded on the project's side, not on a portfolio screen.
