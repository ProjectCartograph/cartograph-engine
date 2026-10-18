# About walk-first

Every core task in Cartograph starts as a guided walk: one question on the screen, plain words, one next step. The full workspace, where every record and every control is visible at once, stays one step away. This page explains why the interface is built that way, what it costs, and where else you could have gone.

## What people told us

We tested an earlier interface with people in person. Two groups took part: people who plan and approve work but do not work with data for a living, and people who do. The first group found the interface "too cascading". Every screen showed too much information and too many buttons, and several gave up at the first screen after setup. Home was too vague, because it asked one open question. The one part they finished, and liked, was the setup walk.

A screen audit by the project gives the same picture in numbers. The median screen showed 49 controls and 2 to 3 primary actions. The setup walk showed 7 controls and 1 primary action. The decision to start every core task as a walk is recorded in [cartograph-ui#136](https://github.com/ProjectCartograph/cartograph-ui/issues/136).

## Why a walk helps someone new

Three well-studied ideas point the same way.

**Choice has a cost.** The more options a screen offers, the longer a person takes to choose, and too many can make them give up. [Nielsen Norman Group (NN/g) describes the cost of choice](https://www.nngroup.com/articles/simplicity-vs-choice/), and the underlying finding, [Hick's law](https://pmc.ncbi.nlm.nih.gov/articles/PMC5990552/), says reaction time grows with the number of alternatives. The law has limits, and it fades with practice, so treat it as a direction and not a formula.

**Mental effort is limited.** [NN/g's account of cognitive load](https://www.nngroup.com/articles/minimize-cognitive-load/) separates the effort a task needs from the effort the screen adds, and says to cut the second: remove clutter, use familiar patterns, and replace reading, remembering and deciding with defaults and answers already given. A screen with 49 controls adds a great deal of the second kind.

**One thing at a time works for forms.** The UK government's design system asks for [one question per page, with the question as the page heading](https://design-system.service.gov.uk/patterns/question-pages/), a back link, and the same answer never asked twice. The setup walk already follows that shape. So does the project walk.

A walk is also a way of teaching without a lecture. NN/g found that [tutorials pushed at new users are mostly skipped and forgotten](https://www.nngroup.com/articles/onboarding-tutorials/), and that help works better when it sits beside the step where it is needed. A walk puts the help in the question. Seen through [Diátaxis](https://diataxis.fr/start-here/), a walk is shaped like a tutorial (a guided route, small visible results, a supportive voice) that ends in real work, not a practice exercise.

## Why the full workspace stays one step away

A walk alone would fail the people who already know what they want. NN/g says that [wizards suit rare tasks and newcomers, and feel controlling to people who repeat a task](https://www.nngroup.com/articles/wizards/). It also says that a system built only for novices [slows repeat users with hand-holding, and one built only for experts is hard to learn](https://www.nngroup.com/articles/flexibility-efficiency-heuristic/). The usual answer is [a simple default and a more powerful version](https://www.nngroup.com/articles/novice-vs-expert-users/), and that is what Cartograph does.

This is [progressive disclosure](https://www.nngroup.com/articles/progressive-disclosure/): a small core first, the specialist parts when asked for. NN/g's advice is to keep to two levels, because three or more tend to confuse people. Cartograph has two: the walk, and the workspace. The link between them is visible and says what it leads to. In the project walk it reads **Skip to the full page**.

Both levels reach the same records. A person who starts a project in the walk and carries on in the workspace does not do anything twice, and an analyst who opens a record directly does not have to go through a walk to reach it.

## Where a walk would be the wrong tool

NN/g's account of staged disclosure is clear about the limit. [A linear sequence works when a task splits into distinct steps with little interaction between them, and fails when steps depend on each other and people keep flipping between them](https://www.nngroup.com/articles/progressive-disclosure/). Defining a project mostly splits well. Planning how an indicator's data will be captured depends more on earlier answers, so a walk there needs to show what has been said so far beside the question. Comparing many records at once is workspace work, and no walk helps with it.

The other limit is time. A long walk that is interrupted loses progress unless it saves as it goes. NN/g lists saving and resuming as a basic requirement of a wizard, which is why Home offers **Pick up where you left off**.

## Home

Home is the first screen after setup, and the place where people gave up. Two rules shape its redesign.

It offers a short list of things to do, each named by what it does, and what is unfinished comes first. [The GOV.UK task list pattern](https://design-system.service.gov.uk/patterns/complete-multiple-tasks/) names tasks with verbs, lets people do them in any order, and uses as few statuses as it can. It is meant for longer jobs done over several sessions, which fits defining a project, and does not fit a short job such as recording one reading.

It does not rest on one open question. A box that asks what you are working on suits someone who already knows the answer, and gives someone who does not nothing to start from. A short list of tasks gives each choice a visible label, which NN/g calls "information scent". The box can stay as one of the ways in.

## Loops, and jobs done over several sittings

Two kinds of task strain a walk. One is a loop, such as a programme's
theory of change, where each step rests on the one before and the number
of steps is not known at the start. A page that asks one question at a
time has to show what has been said so far, and has to say when to stop.
The other is a job done over several sittings, such as setting up a
portfolio and reviewing it each season. The [GOV.UK task
list](https://design-system.service.gov.uk/patterns/complete-multiple-tasks/)
is made for that, and Home's **Pick up where you left off** is the same
idea.

Today the programme and portfolio steps are a row of steps, each a page
of several fields, and not one question per page. They are the part of
Cartograph where walk-first has furthest to go. [Define your first
programme](../tutorials/define-your-first-programme.md) takes the
programme as it is now.

## Checking before you save

A walk ends in a review of what you said, before it is saved. The GOV.UK [check answers pattern](https://design-system.service.gov.uk/patterns/check-answers/) sets out the shape: answers grouped by section, a change link beside each, a return straight to the review after a change, and "Not provided" for an optional question left blank. The button names the action it performs. In Cartograph the same idea applies to reviewing a change set before it is merged: see [About change sets](about-change-sets.md).

## Fast routes for people who know the product

An accelerator is [an optional shortcut to an action that already exists](https://www.nngroup.com/articles/ui-accelerators/): a keyboard shortcut, a bulk action, a saved view. NN/g's advice is that the product must be learnable without them, that they are shown gradually and not all at once, and that a tip appears after someone does a thing the long way. Cartograph's aim is the same. For analysts and developers there are also routes with no screen at all, such as the command line and the agent connection, which reach the same records through the same rules.

## Reading the walk without sight, a mouse or quick recall

Making a walk simple helps people with a cognitive or learning disability. It does not make the walk accessible to a screen reader, a magnifier, speech control or a keyboard unless it is built to be. The UK government requires [Level AA of the Web Content Accessibility Guidelines (WCAG) 2.2](https://www.gov.uk/service-manual/helping-people-to-use-your-service/making-your-service-accessible-an-introduction) for its services, with testing against screen readers, magnifiers and speech recognition, and disabled people in research. Cartograph is not a government service, and this is the bar its walks aim at, for both levels.

In a walk, the criteria that matter most are these.

- [Focus order](https://www.w3.org/WAI/WCAG22/Understanding/focus-order.html) and visible focus: moving to the next question must land the person somewhere that makes sense.
- [Labels or instructions](https://www.w3.org/WAI/WCAG22/Understanding/labels-or-instructions.html) and [error identification](https://www.w3.org/WAI/WCAG22/Understanding/error-identification.html): every input says what it wants, and a mistake is named in text.
- [Status messages](https://www.w3.org/WAI/WCAG22/Understanding/status-messages.html): "Saved" reaches a screen reader without moving focus.
- [Redundant entry](https://www.w3.org/WAI/WCAG22/Understanding/redundant-entry.html): the product, not the browser, supplies what the person already gave.
- [Timing](https://www.w3.org/WAI/WCAG22/Understanding/timing-adjustable.html): no timers that cut a person off, and no lost answers if a session ends.
- [Target size](https://www.w3.org/WAI/WCAG22/Understanding/target-size-minimum.html): controls at least 24 by 24 CSS pixels, or spaced.

The W3C's guidance on [making content usable for people with cognitive and learning disabilities](https://www.w3.org/TR/coga-usable/) goes further, and agrees with the walk's plain wording: clear words, one point per sentence, a summary before final submission, going back without losing work, and making each step clear.

The workspace's densest parts are the graph and the large tables. WCAG exempts two-dimensional content from its reflow rule, so those may scroll both ways. They still have to work with a keyboard, and the same records have to be reachable some other way.

## What it costs

A walk is slower than a form for someone who knows all the answers, and it hides how the parts fit together. It adds a second way of doing each task, which has to be kept the same as the first. The test of whether the cost is worth paying is whether people finish, and how they find it. [Run an in-person usability test](../how-to/run-an-in-person-usability-test.md) explains how the project measures that.

## Other ways to do it

- **One simpler workspace for everyone.** It would cut the controls, and it would cut the power with them, which the people who work with data every day would feel.
- **A tour on first use.** NN/g's finding on tutorials counts against it.
- **A mode switch in settings.** It puts the choice in a place a person who is already lost is unlikely to look.
