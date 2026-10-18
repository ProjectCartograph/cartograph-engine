# About programmes, portfolios and projects

People use these three words for groups of work, and often use them loosely. Cartograph keeps them apart, because each one answers a different question and needs different things written down. This page explains the difference, the questions Cartograph asks to tell them apart, how the three fit inside one another, and the places where Cartograph departs from the standards.

## Three purposes

The simplest way to tell them apart is to ask what each one is for.

- A **project** is for delivering something. It is temporary, it has a start and an end, and it produces outputs: a checked delivery process, a survey, a system people use. A project does not, on its own, make anyone better off. It hands over something that others use.
- A **programme** is for bringing about one change, and the benefit that change gives to someone. It coordinates projects (and sometimes services that keep running) so that together they reach an outcome none of them reaches alone. A programme delivers nothing itself. Its projects do.
- A **portfolio** is for deciding where to invest. It holds projects and programmes so that someone can choose between them: what to fund, in what order, and what to stop. The things in it need not depend on one another.

So the three words answer three questions: *what does it hand over?*, *what change does it bring about?* and *what shall we fund?*

The standards sort them the same way. APM describes a programme as a [temporary strategic endeavour aimed at beneficial change](https://www.apm.org.uk/resources/what-is-project-management/what-is-programme-management/), and its [introduction to programme management](https://www.apm.org.uk/media/lm2fdobv/apm-introduction-to-programme-managementsample.pdf) says programme management is about delivering change, not about delivering large and complex projects. The UK government's functional standard [GovS 002](https://projectdelivery.gov.uk/library-products/government-functional-standard-govs-002-project-delivery/) defines a programme as a temporary, flexible organisation that coordinates a set of projects and related work to deliver outcomes and benefits, and says a portfolio is made of work components that can include programmes. HM Treasury's [guidance on developing business cases](https://assets.publishing.service.gov.uk/media/6a4390675b6406df58c14006/Guidance_on_Developing_Business_Cases.pdf) puts the four levels in order: policy, portfolio, programme, project, each setting its objectives against the level above (clause 3.11). The Project Management Institute's standards draw the same lines, but pmi.org could not be read for this page, so that match rests on secondary summaries.

## Cost and size do not decide it

A big project is still a project. The Treasury says it is not the cost of a proposal that makes it a programme: the decision rests on the work needed and how best to manage it (clause 3.8). A programme also does not remove the need to make the case for each project in it (clause 3.9).

Cartograph follows this. It never asks how big the work is. A hobbyist's allotment group and a national government use the same three words in the same way.

## The questions Cartograph asks

People bring documents with their own words for their work, and the words are often wrong. A document might call something a programme that is really one project in several parts, or call a grouping a programme when it only shares a budget. Cartograph does not trust the word. It asks the same ordered questions of everything, and the first one answered with yes decides (decision D56 in the engine's `docs/TAXONOMY.md`).

On the **New** screen, which you reach from **Create** and **Not sure which?**, the questions read as a list, under the line *Read from the top and pick the first that fits. What your documents call it does not decide.* They are:

1. **Run by another body.** Does another organisation lead and pay for it, so your work only relies on it?
2. **A standing policy.** Is it a standing policy or rule, with no end date, that your work puts into effect?
3. **A service.** Does it keep running with no end date, like a service or a team's regular work?
4. **A portfolio.** Does it group projects or programmes only to decide which to fund, and in what order?
5. **A programme.** Does it coordinate several projects, each with its own sponsor or budget, towards one change?
6. **A component project.** Does it make a change of its own that other work relies on, such as a survey that sets a baseline, a system people use, or a study?
7. **A deliverable.** Is it something another piece of work hands over, such as a report, materials, a training or an event?
8. **A project.** None of these. It is work with a start and an end.

The first three questions find work that is not a project at all. The fourth finds a portfolio. The order matters. Something that groups work only to choose between it is a portfolio even if the people running it call it a programme. Something that is grouped only by topic, place or who looks at it, with no decision across it and no shared change, is a collection. Cartograph does not keep collections as records, so you define each project on its own.

The test that separates a programme from a portfolio is the one the Project Management Institute gives, as the engine's taxonomy records it: work that does not jointly contribute to common benefits, or is related only by shared funding, technology or stakeholders, is better managed as a portfolio. Cartograph turns it into one question. Does each project need the others to bring about the same change? If yes, it is a programme, and it must be able to say how the steps lead to the change (see [Write a programme's theory of change](../how-to/write-a-programmes-theory-of-change.md)). If the projects could each go ahead without the others, and someone ranks them to decide what to fund, it is a portfolio.

## How they nest

The three nest in one direction, and each link is recorded on the smaller thing.

- A **portfolio** can hold programmes, projects and other portfolios. A programme or project names the portfolios it belongs to, and a portfolio names the portfolio it sits in. A programme can belong to more than one portfolio.
- A **programme** can sit inside a larger programme. The smaller one names the larger one. A loop, where a programme would contain itself, is refused.
- A **programme** lists its **components**: the projects and programmes it depends on, each with the reason. A component can serve any number of others. This is how a shared piece of work, such as a data platform every other project uses, is written once and used by several.
- A **project** can also list components, and a long chain of components has a longest path through it, which Cartograph shows as the critical path.
- A **service** (an operation that keeps running) names the programmes it is part of. A programme with a service in it can then say what carries the benefit on after the projects end.

Goals sit beside this tree, not inside it. A programme, project or portfolio says which outcomes it serves, and that is how the strategy and the work meet. The goal tree is containment; the programme's own theory of change is where the reasoning from work to outcome lives. [About purpose, goals, objectives and outcomes](about-purpose-goals-objectives-and-outcomes.md) says more.

## Where Cartograph departs from the standards

Cartograph is a tool for writing down what an organisation has decided. It is not an implementation of any one method. Where it departs, it does so on purpose.

- **No benefit record.** The standards make the benefit the organising idea of a programme. Cartograph has no benefit record of its own. A benefit is held as its parts: an outcome that says what improves, an indicator that says how it is measured, a group that says who feels it, and readings that say what the number was. This keeps one place for each fact.
- **No tranche.** MSP and APM divide a programme into tranches, which are steps that each end in a decision. Cartograph does not hold them, because dates and order of delivery belong to the delivery tool. [About long horizons](about-long-horizons.md) says what a programme does hold for a long life, and what is missing.
- **No blueprint.** MSP asks for a model of the organisation as it is, in between and at the end. Cartograph does not design the future state. The steps in a programme's theory of change play part of that role.
- **A business case, in part.** The Treasury asks for a full business case at programme level. Cartograph holds the programme's aim, problems, outcomes and indicators, and holds money on the project and the portfolio. It does not hold an options appraisal or an economic case.
- **Components may serve many.** Some methods put each project in one programme. Cartograph lets a piece of work serve several, because shared work is common.
- **The checks advise.** A programme's checks never block saving. They say what is missing: nothing inside it yet, no theory of change, a problem that names nobody. This is because every question about a programme is answered by reading other records, and a definition that saved yesterday should not be refused today because someone edited a different file. [About checks](about-checks.md) says more.
- **"And", never "or".** A step in a theory of change can rest on several things that all have to hold. It cannot rest on "this or that". If two routes are both live, they are two steps.

MSP's own text could not be read for this page: the official guidance is paid for, and the sources used were a training provider's summary of the 2011 edition and AXELOS's and PeopleCert's public pages. Where this page names an MSP idea, it rests on that summary.

## See also

- [Start a programme](../how-to/start-a-programme.md)
- [About long horizons](about-long-horizons.md)
- [Kinds of record](../reference/kinds-of-record.md)
