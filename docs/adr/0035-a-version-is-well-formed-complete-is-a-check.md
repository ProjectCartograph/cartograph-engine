# 0035. A version is well formed; complete is a check

**Status:** Accepted. Changes how [0024](0024-every-change-goes-through-a-change-set.md) rolls a change set in.

## Context

A change set could not be merged while any record in it lacked a field
its schema requires: the review named each as a blocking check, and
rolling in refused. That works against incremental change:

- A person defines a project over days. Nothing they did is in the
  record, and nobody else sees it, until every required field is filled.
- A small correction to a record already incomplete cannot be merged,
  because the record's old gaps travel with the edit.
- An agent's change set holding a new project half defined could only
  be accepted whole or not at all.

The rules already separate the two questions. A check never blocks a
working save; a handoff refuses while a blocking check stands.
Completeness is what a handoff is for. A version only needs to be
readable.

## Decision

- **A version is well formed.** Saving one (directly, by import, or by
  rolling a change set in) refuses what is malformed: a value of the
  wrong type or form, one outside its list, a property the schema does
  not have, a reference to nothing, what a kind's rules refuse, and a
  record with no id or name.
- **Complete is a check.** A property the schema requires and the record
  lacks, or gives empty (no text, no items), is a field not filled in
  yet. When it is the only fault, it is not a reason to refuse a
  version; a property missing from a choice of forms counts too when
  that form only lacks it, but never in a reference. The envelope
  (`apiVersion`, `kind`, `metadata`, `spec`) and `metadata` are never
  left to fill.
- **It is said where people read checks.** Each field not filled in is a
  check `required:<pointer>`: in a change set's review (as before, but no
  longer a lock on the merge), and in a project's checks, where it
  blocks the handoff. Goals, programmes, operations, portfolios and gaps
  list it as advice, their checks having no blocking state.
- **What locks a merge** in a review is a check `malformed:<pointer>`:
  the record could not be saved as it stands.
- **A strict workspace stays strict.** With the roll-in policy
  `checksMet`, a change set holding a record not filled in is refused,
  whatever was waived: such a workspace merges whole records.
- **`ChecksOf`, which agents and the decision rounds read, is unchanged.**
  What an agent meets changes only as `docs/EVALUATING.md` has it
  measured; an agent's change set is held to its open checks as before.
- **`cartograph validate` stays strict:** valid there means complete. An
  export holding records not filled in still imports.
- The manifest schemas keep `required`: it says what complete is.

## Options rejected

- **Removing `required` from the schemas.** Every interface and agent
  reads the schemas to know what a record needs; the list would move to
  prose, and an interface's types would say every field may be absent.
- **A draft state on the version.** A second kind of version every
  reader must know, where a check already says the same.
- **Required checks in `ChecksOf`.** The rounds and agents read it, so
  what agents are asked would change without the measured runs that
  change needs.

## Consequences

- Every reader of versions reads one with fields missing: charters,
  diffs, the graph, reports, export and import are tested on a version
  of every kind saved with an empty spec.
- A stored version may fail its kind's published schema on `required`
  alone. A consumer that validates exports against the schemas sees
  those as incomplete records, not as broken ones.
- Old interfaces still lock Merge on `required:` checks, which is
  today's behaviour; they do not lock on `malformed:` ones, which the
  server refuses anyway.
