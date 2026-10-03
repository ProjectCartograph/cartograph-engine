# 0021. Evidence from several sources

**Status:** Accepted

## Context

A gap named one data source it could be watched in (`measuredBy`), and a
KPI one it was read from (`source`). Evidence often comes from several
at once: a shortfall seen in an intake log and in a member survey, a
rate computed from a count kept in one system and a total kept in
another. With room for one, people picked one and the rest went
unrecorded, or went into a note nobody could follow.

## Decision

**The lists are the fields.** `Gap.spec.dataSources` and
`KPI.spec.sources` name every data source, each a reference, so each can
be opened and checked. A KPI needs at least one.

**The single fields are deprecated, not removed.** `Gap.spec.measuredBy`
and `KPI.spec.source` are folded into the lists wherever a manifest is
read or imported, and a manifest that names only the single field still
saves, so an interface or a vault from before 2.7.0 works as it did.
Nothing writes them any more.

A gap's citation (`source`, the document a finding was written in) is
text, not a data source, and stays one field.

## Consequences

- An interface offers several data sources wherever it offered one, and
  says them in the plural.
- A report lists every source of a KPI.
- Key results and success criteria still name one source each; they
  can follow the same way when someone needs it.
