# About components and large projects

A large piece of work is rarely one thing. It has parts, some of which other work also needs, and sometimes it has grown into something that is no longer a project at all. This page explains how Cartograph treats parts, why a project has one objective, how to tell a large project from a programme, and what research on very large projects suggests a person should do.

A note on sources. Where a claim rests on a standard or a study, the link is beside it. I read the UK government's project delivery standard (GovS 002), the Association for Project Management's public pages, the Infrastructure and Projects Authority's Routemap handbook, Flyvbjerg's 2014 overview of megaprojects and HM Treasury's Green Book at source. I could not read the Project Management Institute's standards, ISO 21502 or the PRINCE2 manual at source, because they are paid or refused access. Where this page mentions them, it says so.

## Work lists what it depends on

In Cartograph a project or programme lists its **components**: the projects and programmes it depends on, each with a short reason. The component says nothing about who uses it. That is read back from the other side, and shows as **Used by**.

The direction is the one a programmer uses for libraries: a program names the libraries it needs, and a library does not name its users. It matters for two reasons.

- **Nothing has to be edited twice.** When a new project needs an existing part, you change the new project. The part stays as it was.
- **A part can serve many.** A data platform that three projects need is a component of each. There is no single parent to give it to.

The cost of this choice is that the structure is a graph and not a tree. A graph can have long chains, parts that much of the work leans on, and loops that nobody meant to write. Cartograph reads the whole graph and shows each of these. [Find the critical path and fix a loop](../how-to/find-the-critical-path-and-fix-a-loop.md) shows how.

- A **loop** is a chain that comes back to where it started, so the work could never begin. Cartograph refuses it as a definition.
- The **critical path** is the longest chain on the calendar. A delay anywhere on it delays everything that depends on it. A project's span comes from its milestones, and a programme has no dates of its own.
- A **shared component** is one that the most work depends on, directly or through others, so its failure reaches furthest.

UK government guidance points the same way. GovS 002 asks for dependencies between parts of the work to be kept to a minimum, so that each part is as self-contained as it can be, and for interdependencies between parts with different owners to be recorded ([GovS 002 v2.1](https://projectdelivery.gov.uk/library-products/government-functional-standard-govs-002-project-delivery/), clauses 5.2 and 7.2.4). Making the chain visible helps with the first. Asking for a reason on each link helps with the second.

Earlier versions of Cartograph let a project name one bigger project it was part of. That is still read, but the editor now writes only the list of components.

## Why a project has one objective

A project is work that ends, with one team, one sponsor, one budget and one objective: the change it exists to make. A second change is either a key result of the first, or a project of its own that this one lists as a component.

The reason is accountability. A project with two objectives has two answers to "did it work?", and nobody is clearly answerable for either. Giving each change its own project, with its own objective, puts the question where someone can answer it. The parent then holds the results that the parts add up to.

The standards frame this as a hierarchy in which each level is the sum of the levels below it. GovS 002 describes work components that way: a higher level is the sum of the lower ones that connect to it (clause 3.3). Cartograph's part of that idea is the key result: a project with components should have one that its parts add up to.

## One sponsor and one budget, or a programme

Cartograph's test for whether a part belongs inside a project is whether one person is in charge of all of it, with one budget. If so, the parts are components of one project. If a part needs its own sponsor or its own budget, it is a project of its own, and the group is a programme.

That test comes from the Project Management Institute's idea of a subproject and a component. I could not read PMI's text. I know it from a summary of PMI's own library page and from the passages that Cartograph's decision record quotes, so treat the wording as second-hand.

## How to tell a large project from a programme

No source I read gives a size, a budget or a number of parts at which a project becomes a programme. The sources give tests instead.

- **Outputs or outcomes.** The Association for Project Management says a project delivers specific one-off outputs, and a programme brings related projects together to achieve beneficial change, delivering outcomes ([APM information sheet](https://apm.org.uk/media/d4piyeyg/information-sheet-programmes-project-portfoliofinal.pdf)). A good test is the nature of the objective: a new building is an output, staff working from one place is an outcome, and lower travel costs is a benefit.
- **One deliverable or many.** GovS 002 calls a project a temporary management environment, run in stages, that delivers one or more products or outcomes. It calls a programme a temporary, flexible organisation that coordinates a set of projects and related work to deliver outcomes and benefits linked to strategic objectives (glossary).
- **How the work is released.** A project moves through stages, and a gate before each one commits the money. A programme can run in tranches, so that outcomes and benefits arrive step by step.
- **What is inside.** A programme can include business as usual alongside its projects, so that the change becomes ordinary working ([APM, what is programme management](https://www.apm.org.uk/resources/what-is-project-management/what-is-programme-management/)).
- **Benefits need their own management.** GovS 002 asks for each benefit to have a named owner and for benefits to be reassessed throughout (clause 7.3).
- **The parts need one another for the same change.** This is the test Cartograph asks when you create something and are not sure which kind it is.

The Project Management Institute is reported to say that a large project split into related projects, with the benefits managed explicitly, becomes a programme. I could not confirm that at source.

These are signs, not a switch. APM says projects, programmes and portfolios are best seen as points on a gradual scale, not as separate things. The Infrastructure and Projects Authority goes further in its Routemap handbook, which uses the word project to cover programmes ([IPA Routemap handbook](https://assets.publishing.service.gov.uk/media/6297200fe90e0703a2fed794/Handbook__-_FINAL.pdf)). Cartograph's questions therefore advise. They do not block.

When you are unsure, [Create, then Not sure which?](../how-to/split-a-large-project-into-components.md) asks the questions in order.

## What the evidence on very large projects says to do

Bent Flyvbjerg's overview of megaprojects reports that most of the very large projects in his data went over budget, and that shortfalls in benefits were also common, with no sign of improvement across the decades the data cover. He calls this the iron law of megaprojects: over budget, over time, over and over again ([Flyvbjerg, 2014](https://arxiv.org/pdf/1409.0003)). His data are about projects of a very large kind. The finding is evidence of a pattern, not a rule for projects of every size, and Cartograph does not attach a size to it.

The paper gives reasons that a person can act on.

- **Estimates lean one way.** Optimism and strategic misrepresentation explain outcomes better than older theories do.
- **Delay costs money.** A longer implementation phase goes with a higher percentage overrun. The paper's advice is to keep delays small, and not to rush the decision stage to do it.
- **Fix the front end.** Projects tend to break when reality meets hopeful estimates, then are paused and reorganised. The remedy the paper offers is to get the early planning right.

A later study of the Olympic Games finds that costs follow a heavy-tailed distribution, so an average can hide a very large overrun, and it names tight coupling and long planning horizons among the causes ([Flyvbjerg, Budzier and Lunn](https://arxiv.org/abs/2009.14682)). I read only the abstract, and it covers one kind of project.

From these, the practices for a person running a large project are these.

- **Use ranges, not single points.** HM Treasury's Green Book requires appraisers to adjust for optimism bias: raise estimated costs and time, and lower estimated benefits. The size of the adjustment should come from the organisation's own record of forecast errors on similar work, and generic values are a fallback. The adjustment shrinks as specific risks are prevented, and what is left becomes contingency ([The Green Book, 2026, paragraphs 6.78 to 6.84](https://assets.publishing.service.gov.uk/media/698dbcd17da91680ad7f4308/The_Green_Book_2026.pdf)).
- **Compare with a reference class.** That adjustment is reference class forecasting: predict from how a group of similar past projects turned out. The sources I read give no figure that fits every kind of work, so use your own organisation's history first.
- **Work in stages.** GovS 002 requires a project to run in stages, each opened by a gate that can approve, revise, defer or stop the work, with plans updated before significant gates (clauses 6.3 and 7.2.5). I could not read the PRINCE2 manual, but a summary of its guidance describes the same idea, with a board approving each stage and a replan when a part is forecast to drift too far.
- **Keep parts loosely tied.** The Routemap handbook lists twelve factors that make a project's delivery environment complex. Interfaces, dependencies and interconnectedness are among them.

## Where Cartograph departs

Cartograph does not yet model stages and gates. It has milestones, and a plan can be read as phases. It does not give a part a tolerance, the room it has to drift before it must ask the level above. Costs and dates are single values, though a date can be a window, and nothing yet keeps planned against actual for finished work so that a later project can use it as a reference class. The critical path is a chain by calendar time. It does not weigh cost or the chance of slipping, and Cartograph does not simulate a forecast for you. A person decides.

## See also

- [Add a project or programme as a component](../how-to/add-a-project-or-programme-as-a-component.md)
- [Split a large project into components](../how-to/split-a-large-project-into-components.md)
- [Find the critical path and fix a loop](../how-to/find-the-critical-path-and-fix-a-loop.md)
- [About planning ahead](about-planning-ahead.md)
- [About purpose, goals, objectives and outcomes](about-purpose-goals-objectives-and-outcomes.md)
