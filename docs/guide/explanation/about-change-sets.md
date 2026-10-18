# About change sets

When you edit something in Cartograph, you do not change the workspace. You change a **change set**: a piece of work kept apart until someone reviews it and merges it. This page explains why, and what it costs. The examples come from the produce cooperative in Cartograph's example.

## The idea

If you have used a code review tool, you know it. Work happens on a branch, someone reviews it as a pull request, and it is merged whole. A change set is the same idea for plans.

Each one has a title, a reason, the people working in it, and its own draft of every record it touches. Each draft remembers the version it started from, so Cartograph can tell when the record has moved on since ([ADR 0022](../../adr/0022-change-sets.md)).

The comparison with code review breaks in one place. Plans are read by people who have never used a review tool, so a change set is only worth having if reviewing it is as easy as working. Cartograph reviews a change set in the ordinary screens, which show what is new or changed ([ADR 0024](../../adr/0024-every-change-goes-through-a-change-set.md)).

## Why not just save?

Because a plan is read by other people. Three things go wrong with a save button.

**Half-finished work shows.** A project defined over several days would be visible to everyone the moment you typed its name. A change set keeps it yours until you say it is ready.

**Nobody can review it.** An organisation often wants every change to a record to have an author, a reason and a second pair of eyes. A save button gives none of those. Say a colleague wants the check tool's cost in the Quality Check Rollout project changed. A change set shows the record as it was, as it is now, and its open checks, so a reviewer who is not a specialist can follow it ([ADR 0024](../../adr/0024-every-change-goes-through-a-change-set.md)).

**Two people, or two agents, overwrite each other.** Agents are the sharpest case. Agents once drafted into shared drafts, and two working for the same person drafted over each other, so whichever saved last won. Giving each agent a change set ended that ([ADR 0022](../../adr/0022-change-sets.md)). People working in the same change set edit it together and see each other there ([ADR 0024](../../adr/0024-every-change-goes-through-a-change-set.md)).

## Why not a draft?

A draft is one person's private work on one record. A change set is a group of records that belong together: a new project, the goal it serves and the gap it closes. Reviewing and merging them as a group means a link between them is never half saved, with an outcome merged and the gap that closes it left behind. That is why [accepting item by item was rejected](../../adr/0022-change-sets.md).

## Why agents always work in one

An agent reads and proposes. It never merges. Its work arrives as a change set that waits for you. This is deliberate: the person who is accountable for the record is the person who decides ([ADR 0025](../../adr/0025-an-agent-works-in-its-persons-change-set.md)). An agent may work in a change set you opened and named, which stays yours, and it never proposes one it was brought into.

An agent may take decisions you did not ask it to, such as a date or a figure. Cartograph asks it to say so, and shows them in their own place, **Decided for you**, so you can check each one before you accept. That list is only as good as the agent's honesty. An agent that decides without saying so is not caught ([ADR 0033](../../adr/0033-decisions-taken-for-the-person-are-kept-apart.md)).

## Merging

Merging saves the included records in one step, in the order their references need. You may leave some out, and they stay in the change set as drafts. If two people try to merge it at the same moment, one wins ([ADR 0022](../../adr/0022-change-sets.md)).

You can merge when nothing is malformed: a value of the wrong form, or a reference to nothing. A record with fields still to fill can be merged, and what is missing stays as a check. This is deliberate. A project is defined over days. If nothing could be merged until everything was complete, nothing would be shared, and a small correction to an old record would be blocked by its old gaps ([ADR 0035](../../adr/0035-a-version-is-well-formed-complete-is-a-check.md)). An organisation that wants the stricter rule can turn it on.

## Starting something new

A new record starts a change set as well. When you start a programme, a
change set called *Changes to* the programme's name opens at once, and
the record stays in it until you merge. If the record has unfinished
items, **Merge** lists them and asks you to say in a few words why you
are merging before they are finished. A step that touches several
records, such as putting a project in a portfolio, lands the same way,
as one set of changes that is reviewed together.

## The rules an organisation can set

An administrator can set what a change set needs before it is merged: whether every change must go through one, whether every check is met, whether anything may be left open, and whether someone other than the author must merge. The rules apply to people and agents alike, because the reason is the same for both: the record should change by a route someone can review. The decision is in [ADR 0024](../../adr/0024-every-change-goes-through-a-change-set.md).

## What it costs

You make one more decision: when to merge. A person who wants to fix a typo makes the change and merges it. Cartograph starts a change set on your next edit and names it for what you changed, and you can rename it. You can have several open at once, so unrelated changes stay unrelated. A workspace with one or two people may find a second reviewer more than it wants, so that rule is off unless you turn it on.

Comments are not part of a change set. They are conversations about a record and stay with the record.

## For developers

The decisions are in the engine repository: [0022](../../adr/0022-change-sets.md), [0024](../../adr/0024-every-change-goes-through-a-change-set.md), [0025](../../adr/0025-an-agent-works-in-its-persons-change-set.md), [0033](../../adr/0033-decisions-taken-for-the-person-are-kept-apart.md) and [0035](../../adr/0035-a-version-is-well-formed-complete-is-a-check.md).

## Related

- Explanation: [About checks](about-checks.md), [About walk-first](about-walk-first.md)
