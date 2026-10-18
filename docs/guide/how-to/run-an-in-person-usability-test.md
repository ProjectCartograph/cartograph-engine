# How to run an in-person usability test

This guide shows you how to test Cartograph with real people, in person, on three tasks: define a project, review and merge a change set, and record an indicator reading. It assumes you can run Cartograph on a laptop. For why Cartograph is built as a guided walk with the full workspace one step away, see [About walk-first](../explanation/about-walk-first.md).

Everything the person sees is the fictional produce cooperative in `cartograph-engine/examples/minimal`. No real organisation's name, logo or document goes into a test.

## Before the day

### Choose who comes

Test two kinds of people, because they behave differently.

| Kind | Who | How many |
|---|---|---|
| Non-technical | People who plan, run or approve work but do not work with data or software for a living | 5 |
| Technical | Analysts, programme office staff, data people, administrators | 3 to 4 |

About five people for each group that behaves differently is enough to find most problems. Three to four will do where behaviour overlaps ([NN/g](https://www.nngroup.com/articles/how-many-test-users/)). That finds problems. It does not give statistics, which need at least 20 people (same page).

If you can, include in those numbers someone who uses a screen reader, a magnifier, speech control or only a keyboard, and someone with limited digital skills. Allow time: finding them can take a month ([GOV.UK](https://www.gov.uk/service-manual/user-research/find-user-research-participants)).

### Get consent

Send an information sheet before the day and confirm at the start of the session. Follow [GOV.UK on consent](https://www.gov.uk/service-manual/user-research/getting-users-consent-for-research). Say:

- who you are and why you are testing;
- that you are testing Cartograph, not the person;
- what is kept (see "Know what is recorded") and for how long, and who controls it;
- whether anyone else watches;
- that taking part is voluntary, that they can stop at any time, and that you will delete their data if they do;
- how to complain.

If the person is under 18 or vulnerable, you also need the consent of a responsible adult. Steve Krug's free [recording permission form](https://stevekrug.com/download-files/) is a good start. Keep its copyright line, and link to the page and do not repost the file.

### Know what is recorded

A session is measured only if the trace was on while it ran. The trace is off unless asked, and it never keeps what anybody wrote. It records one line per act: when, what kind, which screen, how long, and the tab's random session id. No text, name, figure or search term is kept ([Evaluating people](../../EVALUATING_PEOPLE.md), "What it records"). Tell people this.

### Switch the trace on

1. In `cartograph-ui`, run the stack with a folder for the session:

   ```
   CARTOGRAPH_TRACE_DIR=~/sessions/2026-10-20 just stack
   ```

   The trace is appended to `ui.jsonl` in that directory and kept after `just stack-down`. On any other deployment, set `CARTOGRAPH_UI_TRACE` to a file the server can write (one per replica, unless on a shared volume) or a `postgres://` URL, and restart it. See [Deployment](../../DEPLOYMENT.md).
2. Check before anyone starts: `GET /api/v1/session` answers `"traceOn": true`. If it does not, stop and fix it.
3. Give each person a fresh browser tab. Each tab is one session in the trace (`session.id`). Write down which tab and which task belong to whom, with the time each task began. With sign-in on, `-person` picks one person's acts instead.

### Prepare the starting state

Reset the example between people so each starts the same.

- **Task 1, define a project.** The example has no cold room project. Give the person the facts card below.
- **Task 2, review and merge a change set.** Prepare a change set for the Quality Check Rollout project, with a colleague's suggested edits. Include one edit that conflicts with what is already there, such as a new cost for the check tool against the one approved.
- **Task 3, record an indicator reading.** The example has the Collection turnaround indicator, read monthly from the Collection Log. Its readings end at 2026-04. The person adds the one for 2026-05.

Print each task on a card. Write each as a short, real need, ask the person to do it, and leave out the words on the screen. [NN/g explains why](https://www.nngroup.com/articles/task-scenarios-usability-testing/).

### Plan the room

Bring two people: a facilitator who talks, and a note-taker who does not, so the facilitator can attend to the participant ([GOV.UK](https://www.gov.uk/service-manual/user-research/taking-notes-and-recording-user-research-sessions)). Plan one hour a person, with at least 15 minutes between sessions and no more than six a day ([GOV.UK](https://www.gov.uk/service-manual/user-research/using-moderated-usability-testing)). Practise the whole session the day before ([GOV.UK](https://www.gov.uk/service-manual/user-research/plan-round-of-user-research)).

## Run a session

### Welcome and consent

Say this, in these words:

> "Thank you for coming. We are testing the software, not you. You cannot do anything wrong. If something is confusing, that is a problem with the software and it helps us to know. Please do not worry about hurting our feelings; we need your honest reactions."
>
> "I will give you three short jobs on cards. Please say out loud what you are thinking as you go: what you are looking for, and what surprises you. I will mostly stay quiet, because I want to see what you would do on your own. If you ask me something I may not answer yet, and I will answer what I can at the end. Tell me if you want a break at any point."
>
> "We record which screens you open and what you press. We do not record anything you type or choose, and we do not record your name. [Name the note-taker] will take notes. Nobody else is watching. You can stop at any time, and if you do, we will delete everything from your session. Are you happy to go ahead?"

Wait for a yes. Then ask what their job is and how they plan work today. Write the answers by participant code (P1, P2), never by name.

This follows the shape of Krug's published [test script](https://stevekrug.com/downloads/test-script.pdf).

### Take a first look

Open Home and say:

> "Please look at this page and tell me what you make of it: what it is for and what you can do here. Just look around and say your thoughts. Please do not press anything yet."

Note what they say it is for and how many choices they name. Then start the tasks. Start every task from Home.

### Give the tasks

For each task: hand over the card, read it aloud once, write the time, say "Please say your thoughts out loud as you go", and stay quiet.

**Task 1: define a project.**

> "You work for a cooperative whose members grow produce. Produce sometimes waits in the heat at the depots before it is collected, and the board has agreed to try cold rooms at two depots. You have been asked to write this down so the board can approve it. Use the facts on the next card. Get it to the point where you would send it to the board."

Facts card, seven lines at most: a cold room pilot at two depots; the depot network team leads it; produce waits in the heat before collection and some is lost; two goals, less harvest lost after picking and collection within a day; a rough end date of the end of next season; the board approves it.

Done when a project exists with those facts, the person has reached the review step, and they have saved or submitted it.

**Task 2: review and merge a change set.**

> "A colleague has suggested some edits to the cooperative's quality check project. Look at what they suggest, decide what to do with each edit, and make the project reflect your decision."

Done when the change set is merged or declined, with a decision on the conflicting edit. Both count as finished. Note whether the person understood what was changing and what conflicted.

**Task 3: record an indicator reading.**

> "It is the start of June. The collection log shows that last month the typical wait between a delivery arriving and being collected was 29 hours. Make sure this is written down where the board will find it."

Done when a reading of 29 is saved against Collection turnaround for May 2026.

### Help only when you must

[Let the person struggle](https://www.nngroup.com/articles/thinking-aloud-the-1-usability-tool/), and step in when they are completely stuck ([GOV.UK](https://www.gov.uk/service-manual/user-research/using-moderated-usability-testing)). Go up one rung at a time and write down the highest you reached.

| Rung | When | Say |
|---|---|---|
| 0 | They go quiet | "What are you thinking now?" |
| 1 | They ask you something | "What do you think it does?" or "What would you try?" |
| 2 | They have stopped making progress, or look upset | "What are you looking for?" |
| 3 | Still stuck after that | Point to an area only: "Have a look around here." |
| 4 | Still stuck, or they ask to stop | "Let's leave that one. Thank you." Mark it given up. |

Never name a button, a screen or a step. Never say "right" or "wrong". If they ask "did I do it right?", say "I'll tell you at the end. What would you do next?" If your prompt may have changed what they did, note it and discard that part of the data.

### Ask one question after each task

Ask right away ([MeasuringU](https://measuringu.com/seq10/)), and show the scale on paper:

> "Overall, how difficult or easy was the task to complete?" From 1 (very difficult) to 7 (very easy).

Write the number. If it is below 5, ask: "Briefly, what made it difficult?" This is the Single Ease Question.

### Finish with the usability scale

After the third task, give the person the ten statements of the [System Usability Scale](https://measuringu.com/sus-items/) on paper. Each is answered from "strongly disagree" to "strongly agree" on five points. Print them exactly as MeasuringU gives them.

To score: for each odd-numbered item subtract 1 from the answer, for each even-numbered item subtract the answer from 5, add the ten, and multiply by 2.5 ([MeasuringU](https://measuringu.com/sus/)). The result is out of 100, and it is not a percentage. Across about 500 products the average is 68 ([MeasuringU](https://measuringu.com/10-things-sus/)). With a handful of people, treat it as a flag, and report it with the number of people.

Then ask "What was the best part?" and "What was the most confusing part?", thank them, say what happens next, and ask what they thought of the session.

## Make it accessible for the person

Run the same tasks for everyone, and change the setting and not the task ([GOV.UK](https://www.gov.uk/service-manual/user-research/running-research-sessions-with-people-with-disabilities)).

- Ask people who use assistive technology to bring their own device or settings, or visit them. Their settings are hard to recreate.
- Ask before the day what would help: large text, a quiet room, extra time, someone to read aloud.
- Check the person can use and understand the consent sheet. Offer it aloud or in large print.
- Talk to the person, not to a helper. Use plain words. Do not guess at unclear speech; ask them to write it down.
- Do not hurry. Skip the help ladder's prompts for rungs 2 and 3 unless they ask for help. Offer a break between tasks.
- Read the Single Ease Question and the statements aloud if they prefer, and write their answers. They may skip the scale.
- In your notes, add which assistive technology they used, whether focus went where they expected, whether a message was announced, whether they could reach each control, and anything they could not read. These map to the [WCAG 2.2](https://www.w3.org/TR/WCAG22/) criteria in [About walk-first](../explanation/about-walk-first.md).
- Report their results next to the others, and not averaged in.

A test with a few people does not show that a screen meets the standard. It does not replace an accessibility audit.

## Take notes

The note-taker writes one observation per line, by participant code, task and time. Write what was seen or heard, not what you think it means ([GOV.UK](https://www.gov.uk/service-manual/user-research/taking-notes-and-recording-user-research-sessions)).

For each task record one outcome:

| Outcome | Meaning |
|---|---|
| Finished unaided | Done at rung 0 or 1 only |
| Finished with help | Done after rung 2 or 3 |
| Given up | The person or the facilitator stopped it, or time ran out |

Also note where the person hesitated, went back, pressed something that did nothing, or gave up (use the screen's heading); a short exact quote; whether they used the walk or the full workspace, and whether they switched; the highest rung; the start and end times; the Single Ease Question number; and for technical people, whether they looked for a shortcut or a way around the walk.

Do not write names, organisations or what the person typed. Strike out anything you slip in.

## Read the result

1. Debrief the same day. Agree the top problems and rank them by how many people met each and how bad it was. Giving up is worse than being slowed. Allow about one hour of analysis for every two hours of sessions ([GOV.UK](https://www.gov.uk/service-manual/user-research/plan-round-of-user-research)).
2. Read the trace:

   ```
   cartograph ux ~/sessions/2026-10-20/ui.jsonl
   ```

   `-from` and `-to` take one task's window, and `-json` gives the figures to a script. Read the task table (started, finished, given up, median and p95 time by kind), the dead ends, dead clicks and repeated presses, back in a walk and screens per task, and any slow answer. See [Evaluating people](../../EVALUATING_PEOPLE.md).
3. Join your notes to the trace by tab and time. Check that "given up" in your notes matches the table.
4. Compare with the old design where you have it. The earlier interface's median screen showed 49 controls and 2 to 3 primary actions, and the setup walk showed 7 and 1. Your question is whether people who finished the setup walk now finish the three tasks unaided.
5. Report each task plainly: how many of how many finished unaided, with help, or gave up, by kind of person; the Single Ease Question values; the usability scale flag; and the top three problems with where they happened. Do not report percentages for groups this small.
6. Fix what you found, then run another round with fresh people.

## Related

- Explanation: [About walk-first](../explanation/about-walk-first.md)
- Reference: [Evaluating people](../../EVALUATING_PEOPLE.md) lists every figure `cartograph ux` prints.
