# 0020. The purpose is the top of the strategy

**Status:** Accepted

## Context

The vision and mission were `Settings.spec.purpose` (TAXONOMY.md D24):
stated once, read at the top of the Strategy view, but edited on the
Settings page beside the operator name and the Chromium path. People
looking for them looked in the strategy, where they are read, and did
not find them. And as a field of Settings they had none of what the
rest of the strategy has: no versions of their own, no proposals, no
draft an agent could write with its person.

## Decision

**Purpose is a kind**: vision, mission and their source, one per
workspace with id `default` (a rule, as Settings has). It sits at the
top of the strategy: the Strategy view shows it above the goals and
edits it there, a strategy editor may change it as they may change a
goal, and it is a node of the workspace graph.

**Settings.purpose is deprecated, not removed.** The engine reads the
Purpose manifest where there is one, and the Settings field where there
is none, so a workspace saved before 2.7.0 reads as it did. Every reader
of the vision and mission (the guide, SMART's Relevant, the settings an
interface reads) goes through that one place. Nothing writes the
Settings field any more; saving a Purpose supersedes it.

## Options considered

**Keep it in Settings, edit it from Strategy.** Smallest; the vision and
mission would still have no history, no proposal and no agent draft.

**A goal level above goal.** A purpose is not an aim: SMART and the
level checks would apply to something they do not fit.

## Consequences

- The vision and mission are versioned, proposed and drafted like
  everything else in the strategy.
- An interface edits them where it shows them.
- One more kind: flows, guidance and the access policy know it.
