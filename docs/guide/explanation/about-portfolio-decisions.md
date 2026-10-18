# About portfolio decisions

A portfolio exists to make one kind of decision: of everything we could fund, what do we fund, in what order, and what do we stop? Cartograph gives that decision its own record. This page explains what a portfolio is for, what invest, hold and stop mean, why the decisions are kept apart from the portfolio, and what a review is.

To do the work, see [Set up a portfolio](../how-to/set-up-a-portfolio.md) and [Review a portfolio and record the decisions](../how-to/review-a-portfolio-and-record-the-decisions.md). For every field, see [Programme and portfolio fields](../reference/programme-and-portfolio-fields.md).

## What a portfolio is for

A project delivers outputs. A programme brings about one change that its projects share. A portfolio does neither. It holds projects, programmes and other portfolios so that one body can choose between them against the organisation's strategy. [APM puts it as](https://www.apm.org.uk/resources/what-is-portfolio-management/) the selection, prioritisation and control of programmes and projects, balanced against running the business.

So the things in a portfolio need not depend on one another. The cooperative's *Cooperative Investment Portfolio* holds the *Quality Improvement Programme* and the *Market Access Programme*. Neither needs the other. They are in one portfolio because the same body has to decide which one the season's money goes to first.

This is the test Cartograph uses to tell the three apart. If the parts need each other to bring about one change, you have a programme. If they are grouped only so someone can decide what to fund, you have a portfolio. If neither is true, you have a list you keep for your own reading, and Cartograph does not make a record of it. The reasoning is in the engine's design notes (`docs/TAXONOMY.md`, D32), which cite PMI's standards for portfolios and programmes. I could not read those standards at source, so I do not quote them here.

A portfolio has no end of its own. The UK government's guidance on portfolios, [chapter 12 of the Teal Book](https://projectdelivery.gov.uk/?p=14014), says portfolios, unlike programmes and projects, have no fixed end, and that their work runs as two cycles that overlap: defining what the portfolio is for, and delivering it. What ends is a piece of work in it.

## Invest, hold and stop

Each project, programme or portfolio inside a portfolio gets one decision at a review.

- **Invest** means fund it and let it go on.
- **Hold** means keep it, but commit nothing more until the next review.
- **Stop** means end it and release what it holds, its money and its people.

These three are Cartograph's words. The Teal Book speaks of authorising work in phases, tied to gate decisions, and of corrective action when monitoring shows a need. It does not give a fixed list of verbs.

Hold is the useful one. Most of the time a choice is not yes or no. It is "not yet". In the cooperative's example, the *Market Access Programme* is on hold because its theory of change is not written yet, and the portfolio's reason says so. Hold keeps the programme visible without committing money to it, and it gives the reason a place to be written down.

Each decision also carries a priority, a number where 1 is first and two things can share a rank. Priority answers a different question from the decision. Two things can both be invested in, and one still comes first when the money is short.

## Why every decision has a reason

A reason turns a ranking into something you can defend. The cooperative's *Quality Improvement Programme* is first because a rejected delivery costs a member the whole consignment, so quality moves earnings most this season. A reader a year later can disagree with that, but cannot say nobody thought about it.

Write the reason in a sentence a reader would accept: what the thing advances, or what has to change first. "Strategic" is not a reason.

## Criteria and balance

Before a portfolio decides, it needs a way to compare. Cartograph asks for the strategic objectives the portfolio ranks against, and holds the rest of the comparing in your reason. Other methods give more structure, and you may use them outside Cartograph and bring the result in as a priority and a reason.

- **Scoring against weighted criteria.** The [Teal Book describes](https://projectdelivery.gov.uk/?p=14014) scoring each candidate against criteria such as social value, achievability and attractiveness, which gives a ranked list. Not everything on the list is chosen.
- **Balancing.** After ranking, the Teal Book tests the list for gaps and clashes: objectives left uncovered, work that clashes in time, too much risk in one place, more change than the organisation can absorb. It suggests a map of attractiveness against achievability, and involving other functions. APM adds that project sponsors are often asked to set aside their own priorities for the portfolio's benefit.
- **Weighted shortest job first.** The [SAFe description](https://framework.scaledagile.com/wsjf) divides the relative cost of delay by the relative size of the job, so that work that is cheap and urgent goes first. It is built for sequencing a flow of features, so it fits fine-grained ordering better than a choice between large pieces of work. That fit is my judgment, not SAFe's. The scales and worked examples on that page are behind a login, and I could not read them.
- **The efficient frontier.** This comes from finance: the set of mixes that give the best return for a given risk. I read only summaries, not Markowitz's own paper. For public and social work, return and risk are seldom numbers people agree on, so it works better as a way of thinking about balance than as a calculation.

Cartograph does not yet store criteria, scores or a balance view. That is a choice about what to ask first, not a claim that they do not matter. A portfolio of a few things can decide well in words. A large one will want more, and a method the board trusts. A method that ranks only by benefit for cost can mislead. A summary of the 2025 review of the UK Green Book says it found decisions leaning too hard on one ratio. I read that only in a search summary, not at source, so treat it as unconfirmed.

## Why decisions are their own record

A portfolio's definition is what it is for: its aim, the objectives it ranks against, who decides and with which budgets. It changes rarely. Its decisions change at every review.

Cartograph keeps them apart, as it keeps an indicator apart from its readings. The portfolio is one record. Its decisions are another, named for the portfolio, such as *Cooperative Investment Portfolio decisions*, and each save is kept as a version. So you can change a decision without touching the definition, and the definition does not grow every time the board meets.

There is a second reason. Decisions name the things the portfolio holds, and Cartograph writes a record only after the records it names. The portfolio comes first, then the programmes and projects that say they are in it, then the decisions about them.

## Who says what is in a portfolio

Membership is declared by the member, not by the portfolio. A programme says which portfolios it is in. A project says it. A smaller portfolio says which larger one it is part of. The portfolio's *Contents* step then reads the list back.

This keeps one fact in one place, so a portfolio cannot claim something that does not agree. It also means you cannot add a project from the portfolio. See [Put a project or programme in a portfolio](../how-to/put-a-project-or-programme-in-a-portfolio.md). A decision about something that does not name the portfolio is advised against, not blocked.

## What a review is

A review is the moment the portfolio's body looks at what it holds and decides again. In Cartograph it is one save. You set invest, hold or stop, a priority and a reason for each thing, and press **Save decisions**. The save is stamped with today's date on each decision you changed, and it is kept as a version called *Portfolio review*.

A portfolio can name a review cycle, such as the cooperative's *Seasonal Cycle*, to say how often it expects to review. It is a statement of intent. Cartograph does not yet remind anyone when a review is due.

The Teal Book also says to review the portfolio when something changes, not only on a cycle: when new work arrives, or when the setting shifts. A cycle and an event are both reasons.

## Where Cartograph departs from the sources, and what it leaves out

- **No criteria, scores, categories or balance view.** The Teal Book has all four. Cartograph asks for the objectives and a reason.
- **No portfolio-level risk, capacity or appetite.** APM names resources, implementation capacity, investment limits and regulation as risks a portfolio carries. Cartograph holds risks on projects and programmes.
- **No pipeline.** The Teal Book keeps a pipeline of possible work. In Cartograph something appears in a portfolio only once it says it belongs.
- **Money is not rolled up.** A portfolio lists the budgets it hands out. An allocation, which is what a budget gives one project, is recorded on the project's side. Nothing sums what a portfolio holds against its budgets.
