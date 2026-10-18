# About planning ahead

When you write a project's charter in Cartograph, you are asked to look ahead: a few milestones, what the money is made of, what could go wrong, what the project waits on, which of scope, schedule and cost gives first. You are not asked for a full plan. This page explains why the line sits where it does, and what the ideas behind it are.

## A charter is the first draft of a plan

Plans get better as the work goes on, because you learn things. Project management has a name for that. The Project Management Institute's [Lexicon of Project Management Terms](https://www.pmi.org/-/media/pmi/documents/registered/pdf/pmbok-standards/pmi-lexicon-pm-terms.pdf) calls it progressive elaboration: adding detail to the plan as better information and better estimates arrive.

One way to do it is rolling wave planning. You plan the work that is close in detail, and the work that is far off at a coarser level. The same Lexicon defines it that way.

PRINCE2 reaches a similar place by another route. It plans the whole project coarsely, then plans each management stage in detail near its start, and the project board approves each stage plan before the work goes on. The sources I found for this are study guides, not the official manual, so treat it as a general picture, for example [this course text](https://cduebooks.pressbooks.pub/prince2/?p=261).

A charter sits at the very start of that. Its job is to let someone decide whether to go ahead, and to let the team begin. Everything beyond that can wait until you know more.

## Ready to decide, and ready to start

Two different questions are easy to blur.

- **Ready to decide.** Can a sponsor say yes or no? They need to know what the project is for, roughly what it will cost, the main things that could stop it, and what it depends on.
- **Ready to start.** Can the team begin on Monday? They need to know the first steps, who does them, and what data they need.

Neither needs a task list, a resource plan or day-level dates. The Scrum Guide has a related idea for a team's own work: an item is ready for selection when the team can finish it in a sprint, and items usually get there through refinement, which adds detail, order and size over time ([Scrum Guide](https://scrumguides.org/scrum-guide.html)). The guide does not use the phrase "definition of ready". The point carries over anyway: readiness comes in stages, and detail follows the need for it.

## Ranges, not single points

Early estimates are least reliable, and people tend to be hopeful about them. HM Treasury's guidance on [optimism bias](https://www.gov.uk/government/publications/green-book-supplementary-guidance-optimism-bias) says appraisers tend to be too optimistic, so estimates of costs, benefits and timescales should be adjusted using evidence from comparable projects.

The UK government's guide to benefits management makes a related point in plain words: express forecasts as ranges ([Government Project Delivery, benefits workbook](https://projectdelivery.gov.uk/library-products/benefits-practitioner-workbook/part-a-an-overview-of-benefits-management-in-government/)).

So a good early answer is often a range, or an honest "not known yet". Cartograph already lets you say this in several places:

- A baseline can be marked **Not known yet**, with **Why it is not known yet** and **Known by**.
- A target can be due **By a month**, **In a window**, or **Set when something happens**.
- A cost line has **How the amount is worked out**, and a status that includes **Being costed**.

Cost amounts are still single numbers. Say what the number is made of, and say in the line that it is a first estimate.

## Fixing the rule, not the plan

Scope, schedule and cost pull against each other. When one has to give, which one gives first? It is much easier to answer this at the start, when nobody is under pressure, than in the middle of a crisis.

That is why the scope step asks you to **Hold**, **Adjust** or **Concede** each of the three. Holding means it cannot move. Adjusting means it moves first. Conceding means it can give if the other two hold. You are not planning anything yet. You are agreeing how later plans will be changed.

## What could go wrong, asked sideways

Risk lists often come out generic ("delays", "budget cuts"). The people who know the real weaknesses tend to stay quiet.

Gary Klein's [premortem](https://hbr.org/2007/09/performing-a-project-premortem) is a way round that. The team imagines the project has already failed, then works out why. Klein argues that this gives people with real doubts a sanctioned task: naming a weakness becomes part of the exercise rather than an act of opposition.

You can use it when you fill in the risks step. Ask yourself, or the group: "It is a year from now and this failed. What happened?" Write each answer as a risk (something that might go wrong), or as an assumption.

Those two are different things. A risk is something you reduce or accept. An assumption is something that has to be true for the reasoning to hold, and when it turns out false, the answer is a different plan, not a contingency. In Cartograph an assumption is its own kind of record, with **What has to be true**, **How sure** and **If it is false**.

## What the charter leaves alone

Cartograph can hold more than a charter needs. It can link milestones so that one waits on another, and it works out the last date. That is there for the people who want it, one step away from the walk.

The charter itself does not need an activity list, estimates in hours, a month-by-month budget or a full network of dependencies. If a screen seems to ask for those, it is asking too much for this stage. The team can add them when the next piece of work is about to start.

## Why Cartograph does not become a scheduling tool

A scheduling tool answers "who does what, on which day". A charter answers "should we do this, and on what terms". The first needs a lot of detail that goes stale quickly. The second needs a few well-chosen answers that stay true.

If a question does not help someone decide, or help the team start, it can wait. This is also why the walk shows one question at a time and keeps the larger workspace one step away.

## Where to go next

- To set an indicator's baseline and target honestly, see [About indicator specifications](about-indicator-specifications.md).
- For every field the planning and measurement screens use, see the [Indicator and data fields reference](../reference/indicator-and-data-fields.md).
