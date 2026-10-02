# 0014. Reporting is a port, not the core

**Status:** Accepted

## Context

Reports are tables over the record for people, spreadsheets and BI
tools. At scale they belong somewhere else: a columnar warehouse
answers aggregate queries far better than the database that keeps the
record, a small deployment wants them from the instance it already
runs, and an organisation with its own warehouse wants none at all.
The first version of ADR 0013 put the reports in the engine and their
SQL views in the Postgres store's migrations, so every deployment
carried them whatever it wanted.

The core is what every deployment needs: definitions, their history,
series, the event log, access and the rules. Anything a distribution may
replace or leave out stays outside it, so the core does not grow with
every feature someone might want.

## Decision

Reporting is a port of its own, `internal/reporting`: a `Reporter`
names the reports it answers and runs one into a table. The engine does
not import it; the dependency test forbids it.

| Adapter | `CARTOGRAPH_REPORTS` | What it is |
|---|---|---|
| `reporting/computed` | `computed` (default) | Works each report out when asked from the engine's ordinary reads (every manifest of a kind, who references a kind, project states, series items). Stores nothing; the same on every store, a vault included. |
| `reporting/postgres` | `postgres` | Views in the deployment's own Postgres, created by this adapter when chosen (never by the store's migrations), for SQL and BI tools on the same instance. |
| none | `off` | No reporter. `/reports` answers 404, `cartograph report` refuses. |
| a distribution's own | | A columnar store or warehouse, fed from the event log (ADR 0013) and answering the same port. |

`reporting/conformance` holds any adapter to another's rows over the
same record; the postgres views are held to the computed reports.

What the core keeps is what reporting needs and others need too: the
event log, which a warehouse feed reads and which MCP and the fan-out
read; the set reads, which the goal tree and agents use; and series
items, which make recording a reading O(1) and keep its history.

## Consequences

- A deployment pays for reporting only if it chooses it. With `off`
  there is no reporting code on any request path.
- The four standard reports and their columns are the contract's; an
  adapter answering them must match the computed rows.
- The views are the postgres reporter's surface, versioned with it, not
  part of the store's layout.
