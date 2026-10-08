# 0027. Agents draft to a strict profile, and settle a record in one call

**Status:** Accepted. Extends [0016](0016-agents-read-and-propose-people-decide.md) and
[0022](0022-change-sets.md).

## Context

Agents porting a charter into Cartograph built shapes the discipline
refuses: six objectives on one project (TAXONOMY.md D54), people's
names on roles, fields the schema does not have, kept as "not valid
yet" drafts that only a check or a refused proposal would catch later.
Small agents also ran out of room working one check at a time, and
reported work they had never saved.

Tightening the stored schemas (a `maxItems` on objectives) would stop
a deployed vault that already holds such a project from saving: a
narrowed type, which VERSIONING.md makes a major change.

## Decision

**A strict profile beside the schemas.** `contract/schemas/strict.<kind>.schema.json`
is a kind's schema with the shapes the discipline refuses refused
outright: a project's objectives at most one, a resource's name never
led by a person's title. The engine compiles it with the schemas.

**An agent's draft is held to it, and to its schema, when it is
written.** Every write an agent makes in a change set (a save, an edit,
a batch, the drafts start_work writes) is refused, and nothing saved,
when it fails the strict profile or its schema in a way more writing
cannot mend. Only what is not finished yet (a required field, too few
items, an empty text) is left to the checks. A person's drafts are not
held to it: they reach the record through the blocking checks as
before, and a deployed vault opens and saves as it did.

**One call settles one record.** `settle` sets every field the
documents give, leaves open each check they do not answer with its
reason, and answers with what comes next across the work: the next
record and every check open on it. `start_work` with the pieces of work
decides the structure and drafts every record, so a port is one call to
start, one per record, and one to propose. `work_summary` says what the
change set holds; an agent reports from it, not from memory.

## Consequences

- An improper structure cannot be saved by an agent, however it is
  sent, so a port is right or refused, never wrong and kept.
- The next major makes the strict profile the schema for everyone.
