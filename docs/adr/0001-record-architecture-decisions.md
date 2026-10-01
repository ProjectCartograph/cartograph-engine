# 0001. Record architecture decisions

**Status:** Accepted

## Context

`ARCHITECTURE.md` section 8 gives the reasons behind the founding
decisions in a paragraph each. That works for decisions made together,
before the first release. It does not work for the decisions that come
after: a paragraph appended to section 8 loses its date, the options it
beat, and what it cost, and an edit to an old paragraph can quietly
reverse a decision without anyone seeing that it was one.

The project also commits nothing but the product (`just clean-tree`).
Plans, notes and logs stay out of the tree. A decision record is not a
plan or a log. It explains the code as it stands, and it stays true as
long as the code does.

## Decision

Structural decisions made after 1.0.0 are recorded in `docs/adr/`, one
per numbered file, in the change that makes them. Each record states
its context, the decision, the options considered, and the
consequences. A record is superseded by a later one, never rewritten.
Section 8 of `ARCHITECTURE.md` keeps the founding reasons and points
here.

## Options considered

**Keep extending section 8.** No new convention to learn. But
decisions blur into description, rejected options go unrecorded, and
reversals are invisible.

**Record decisions in pull request descriptions only.** The reasoning
is close to the diff, but it is not in the tree. A reader of a release
tarball, or of a mirror, cannot find it, and it cannot be linked from
the documents that depend on it.

**Records in the tree (chosen).** They travel with the code, can be
linked from `ARCHITECTURE.md`, and are reviewed with the change.

## Consequences

- A contributor making a structural change writes a short record with
  it (`CONTRIBUTING.md` says when one is needed).
- `ARCHITECTURE.md` stays a description of the present; the history of
  how it got there lives here.
- Writing a record takes a few minutes. Keeping it costs nothing,
  because records are superseded rather than updated as the code moves
  on.
