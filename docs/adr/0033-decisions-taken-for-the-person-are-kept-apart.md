# 0033. Decisions taken for the person are kept apart

**Status:** Accepted. Extends [0032](0032-decisions-are-asked-in-rounds.md).

## Context

In rounds (0032) the person decides and the agent asks. The evaluation
runs of 0032 showed agents still deciding for the person without asking:
a figure dated December 2025 where the document gives no month,
reporting cycles started in January, an objective given a key result.
Each was reasonable; none was the person's, and none showed as such.

Nothing can tell, from the server, that an agent decided rather than
read or asked: a value written from a document and one made up look the
same. There is no deterministic check.

## Decision

The agent declares what it decided, and the change set keeps it apart
from everything else, for the person to review:

- **An assumption** is a decision an agent took for its person with no
  document and no answer behind it: the record (`on`), the field it
  wrote (`field`, a JSON pointer), what it took (`took`) and why it did
  not ask (`why`).
- The agent declares it with `assumed` on `settle` or on a `port`
  record. One per field: a later one replaces it, and an empty `took`
  takes it back.
- The change set keeps them (`ChangeSet.assumptions`, stored with it in
  every adapter); `work_summary` and `propose` list them as
  `decidedForYourPerson`; the interface shows them in their own place in
  the change set, each leading to the field it changed.
- The round's `how` and the instructions say it: a settle item the
  documents do not state is a question; a decision still taken without
  a document or an answer goes in `assumed`.

## Consequences

- Best effort, and said so: an agent that decides without declaring it
  is not caught. What the person gets is every decision an agent did
  declare, in one place, rather than none.
- An assumption is not a waiver: the field is written and its check may
  pass. The person reviews it as a decision, not as something missing.
- The evaluation can count them, but not score them: a run with none
  declared may have decided nothing, or declared nothing.
