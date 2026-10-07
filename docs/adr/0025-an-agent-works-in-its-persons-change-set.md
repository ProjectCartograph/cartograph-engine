# 0025. An agent works in its person's change set when asked

**Status:** Accepted. Extends [0022](0022-change-sets.md) and
[0024](0024-every-change-goes-through-a-change-set.md).

## Context

0022 kept every agent in a change set of its own, so two agents never
draft over each other. That is right for work an agent starts. It
leaves out the work people most often want help with: a change set they
opened themselves, with checks still open, that they want refined
before they merge it. Under 0022 the agent had to copy the drafts into
a change set of its own, and the person had to review the copy and
throw away their own.

0024 made change sets live: any person may work in an open one, and
everyone in it sees each edit as it lands. An agent's edits already
reach the live drafts the same way.

## Decision

**An agent may work in any change set its person may call their own**:
one opened for that person, by them or by another of their agents. It
works there only when it names the change set; left to choose, it
works in its own as 0022 says. `change_sets` lists the person's open
change sets with their drafts and the checks still open on them, and
every tool that reads or drafts takes the change set by id.

**The change set stays the person's.** The agent drafts, edits, checks
and leaves checks open for the person, as it does in its own. It never
proposes one it was brought into: the engine refuses that, and the
person proposes and merges it, under the workspace's policy as any
change set is.

**Another person's change set stays out of reach**, as before, even
though its person could join it: bringing an agent into someone else's
work is that person's call, and they bring their own.

## Options considered

**Invite an agent by name into a change set.** Precise, and it would
let a person bring an agent into a colleague's change set. It needs a
new field on every change set store and a screen to manage invitations,
for a case nobody has asked for yet.

**Any open change set its person may work in.** Simplest, and the same
reach the person has. An agent could then edit a colleague's work
unasked by that colleague.

## Consequences

- A person asks an agent to help with a change set by naming it; the
  agent's edits appear live beside theirs.
- Two agents of the same person may be brought into one change set.
  Edits by field (`edit_draft`) keep each other's changes; a whole save
  replaces the draft, so the instructions steer agents to `edit_draft`
  there.
- Nothing about merging changes: the record still changes only by a
  person rolling in a change set.
