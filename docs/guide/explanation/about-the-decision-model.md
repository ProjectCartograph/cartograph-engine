# About the decision model

Some things code cannot judge, and Cartograph can ask a **decision model** about them. It is off by default, and Cartograph works fully without it. This page explains what the model adds, and why it may only advise. It uses the produce cooperative in Cartograph's example.

## What code cannot judge

Code is right about numbers and links and wrong about meaning. Is an outcome written as a state or as an action? Does an aim say one thing or two? Is a gap a shortfall in results or a missing resource? Rules built on lists of words fail on the words a plan really uses ([ADR 0023](../../adr/0023-a-decision-model-behind-a-port.md)).

A decision model answers questions like these about a piece of text. It scores the text. It does not write anything for you.

## What it adds

When it is on, Cartograph asks it for four things.

1. **Advisory checks.** A judgement shows as a suggestion on the step that holds the field. For example, an outcome written as "Train depot staff in grading" reads as an action, and the suggestion says so. It is never a block.
2. **Reading a rough idea** on Home, to pick out the part of what you typed that answers each question a walk asks.
3. **Ranking** the records a picker offers, so the likely ones come first. The rest stay searchable, and you still choose.
4. **Matching** what you typed to a record that already says the same, and suggesting which kind of record to define it as.

It runs beside Cartograph as a separate service. If it is slow or down, Cartograph answers as it would without it. Matching then falls back to the words two texts share, and the screen says so with **Same words** in place of **Same meaning**.

## Why it can only advise

Because it is sometimes wrong, and the project measured how often. The measurements are in [ADR 0023](../../adr/0023-a-decision-model-behind-a-port.md), on small samples. The result is that it makes a useful shortlist and is not reliable enough to refuse anyone. So it ranks and never filters, and the final choice is always the person's.

The same reasoning holds for the way it is asked. The measurements showed the model gives very different quality of answer depending on how a question is put, so Cartograph only asks in the forms that were measured to work. A new question is not added without the same test.

## Related

- Explanation: [About checks](about-checks.md), [About change sets](about-change-sets.md)
- For developers: [ADR 0023](../../adr/0023-a-decision-model-behind-a-port.md)
