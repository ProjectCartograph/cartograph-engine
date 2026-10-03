# 0022. Change sets

**Status:** Accepted

## Context

Agents drafted into the shared drafts and proposed what they drafted.
Two agents working for the same person, or for two people, drafted over
each other: the shared draft of a goal was whichever agent saved last.
And an agent proposing a piece of work proposed it as one set of
everything it had touched, which its person reviewed as a flat list:
one set held ninety manifests, every one shown at once.

Software has the answer: work on a branch, review it as a pull request,
merge it whole.

## Decision

**A change set is a piece of work kept apart** from the record and from
every other piece of work until it is accepted: a title, what it is
for, who works in it, the person it is for, and its own draft of every
manifest it touches, each with the version it started from
(`store.ChangeSetStore`; memory, SQLite, Postgres, the vault's index).

**Agents always work in one.** Each agent connection works in its own,
opened on its first draft, or a new one per piece of work
(`start_work`). Everything it reads, drafts, checks and is told to do
next is as the manifests stand in its change set. Proposing proposes the
change set (`propose`; `propose_save` and `propose_set` do the same, for
agents that know them).

**People keep the shared drafts**, live, as before. They may open a
change set to review it or work in it: an edit there sends what the
editor had and what it has now, and only the fields that changed are
applied, so an agent's work in other fields meanwhile is kept.

**Review is one page per change set**: each item, what it changes
against the version it started from, its checks with the rest of the
change set, and whether the record has moved on since.

**Accepting is whole, after trimming.** The person may trim items
from the next acceptance; they stay in the change set as drafts. The
rest are saved in one transaction, in the order their references need,
each still on the version it started from. A change set is claimed
before anything is saved (proposed to merging), so of two acceptances
one wins. With trimmed items left, it is open again; with none, merged.

**Proposals stay readable.** A store keeps what was proposed before
2.7.0, and the proposals API answers for it.

## Options considered

**Everything in change sets, people included.** Like git, stricter; a
person could no longer simply edit a record, which is most of what
people do.

**Accept item by item.** Most flexible; links between items end up half
saved, an outcome accepted without the gap that closes it.

## Consequences

- Two agents never draft over each other.
- A person reviews one piece of work at a time, in one place.
- A change set's drafts are not live documents yet: people and agents
  in one see each other's work on the next read, and each edit applies
  only the fields it changed. Live editing within a change set is a
  later step.
