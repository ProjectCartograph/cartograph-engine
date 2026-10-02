# 0019. Said once, as the taxonomy says

**Status:** Accepted

## Context

Writing guidance for every kind (ADR 0017) read each schema, check and
screen against TAXONOMY.md and found them disagreeing: with the taxonomy,
with each other, and with the fictional cooperative the example holds.
TAXONOMY.md is the canonical definition. The example is not: it is what
an organisation's first capture looks like, errors included.

## Decision

**The name is `metadata.name`**, as Kubernetes keeps it. Eleven kinds
also required `spec.name`, the same text a second time, and the two
drifted whenever only one was edited. `spec.name` is no longer required or
written; one saved before 2.6.0 is folded into `metadata.name` on read,
the metadata's winning where both are set. The property stays, deprecated,
so every manifest that saved before still does.

**Aims are aligned at the level the taxonomy allows.** A KPI may measure,
and a programme be judged on, an aim at any level: SMART's Measurable is
met through an aligned indicator at every level (D25), and a programme is
judged on benefits realised (What a Programme carries). A project aligns to
outcomes (D24). A project serves a programme when one of its goals is one
of the programme's aims or sits beneath one; the checks on both sides
walk the goal tree rather than compare ids.

**Every measure has a baseline or an admitted unknown** (D25, D26): a
KPI's baseline may now be an unknown with its reason, as a key result's
could.

**A requirement met or not is a success criterion of compliance** (D25).
A project's separate compliance list is deprecated; the charter still
prints one an older project holds, and a check asks for each line to be
recorded as a criterion, with the role that confirms it.

**The structure has a top.** A team may not sit beneath itself, as a
segment already could not; the rule is one, shared by both.

**Words follow the taxonomy everywhere.** An operation's service owner is
the role accountable for it and accepts a project's handover (D23); the
check and the screen now ask for that role, not the team or the sponsor.
A segment is a slice of what the organisation serves, not a group of
people (D9). A gap's evidence is its source (D9). Outcomes are not
"functional goals" (D25). A beneficiary group is described, never counted
(D4). A risk is not an assumption, which is its own kind. Stakeholders are
placed by influence and interest, which the stakeholder map's own checks
ask for. Examples on screen come from the cooperative.

**The example stays as it is.** Its budget filed as a resource, its
programme aims written as actions, its gaps without measures are what a
first capture holds. The checks show them, and fixing them, with a person
or an agent, is how the discipline is learnt.

## Consequences

- One name per manifest; renaming changes it everywhere at once.
- Checks that were too strict for programmes judged on an objective, or
  for components, now agree with the taxonomy and with each other.
- Interfaces stop asking for or showing fields the taxonomy has no place
  for, and an agent's guidance never names them.
