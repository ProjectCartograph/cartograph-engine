# 0024. Every change goes through a change set

**Status:** Accepted. Supersedes the part of [0022](0022-change-sets.md)
that kept people on the shared drafts.

## Context

0022 put agents in change sets and left people editing the shared
drafts, saving a version when they chose. It weighed putting people in
change sets too and turned it down: a person could no longer simply
edit a record.

Two things changed the weighing. Organisations using Cartograph want
the record to change only through a route someone can review and hold
to rules, the same for a person as for an agent: a pull request, not a
save button. And a review page that lists field paths and JSON, as the
first change set page did, is no review at all for most people; a
change set is only worth asking for if reviewing it is as easy as
working (see the design "Reviewing change sets in place").

## Decision

**Every change is made in a change set.** A person editing in any flow,
from a goal's statement to a project's hand-off, edits a change set's
draft of the record, never the record and never a workspace-wide
working copy. The first edit of a piece of work opens a change set the
person names, like a branch; they may have several open and switch
between them. Agents work as 0022 says.

**Change sets are live.** A change set's draft of a record is an
Automerge document, as the shared drafts were (0007), keyed by the
change set as well as the record. Everyone working in the change set
edits it together and sees who is there. Each edit lands in the change
set's item, with the version it started from.

**Rolling in is reviewing.** A change set is rolled into the record
whole, after trimming, in one transaction, as 0022 says. Its author may
roll it in. Review happens in the ordinary screens: while a person
reviews, every screen reads the workspace as if the change set were
accepted and marks what is new or changed. The checks, and the decision
model's judgements where one is configured, are shown as they are for
any draft.

**Policies decide what may be rolled in.** A workspace's settings hold
its change-control policy: whether direct saves are refused, and what a
change set needs before it is rolled in (every check met, nothing left
open, a reviewer other than its author). The engine enforces them at
roll-in, for people and agents alike.

**Direct saves end by policy, not by removal.** The contract's direct
writes (a version, a working copy, a snapshot, a delete, a project's
state) stay in `/api/v1` as VERSIONING.md requires. When the policy
requires change sets, the engine refuses them with a problem that names
change sets as the way. A new workspace starts with the policy on; an
existing one keeps working as it did until an administrator turns it
on. The interface always uses change sets, whatever the policy.

## Options considered

**Remove direct saves in a new major (`/api/v3`).** Cleanest contract;
every deployed v2 client breaks at once, and two contracts are served
side by side for a release. The policy reaches the same place without
breaking anyone.

**One running change set per person.** Simpler to start; unrelated
changes are then reviewed and rolled in together, which is what a
change set exists to prevent.

**Always a second reviewer.** Strict from the start; most workspaces
have one or two people. Left to a policy.

## Consequences

- The record changes only by rolling in a change set, so every change
  has an author, a reason, a review and the checks it passed with.
- The shared drafts keyed by record alone give way to change sets'
  drafts; a record's working copy is no longer where people work.
- Deletes, snapshots and a project's state changes become kinds of item
  in a change set, checked and rolled in with the rest.
- Every read a screen makes can be asked as if a change set were
  accepted, so review needs no screens of its own.
- An existing workspace changes nothing until it turns the policy on.
