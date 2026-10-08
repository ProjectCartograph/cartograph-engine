# 0026. The semantic layer is a port, and dbt its first adapter

**Status:** Accepted.

## Context

People catalogue their KPIs in Cartograph and their data in a
warehouse. The number a KPI names is built in the warehouse by an
analytics engineer, who until now read the KPI's words and decided
again what each number sums, over which rows and out of what.

dbt's semantic layer (MetricFlow) is the most used open definition of
metrics: semantic models over warehouse models, with entities,
dimensions and measures, and metrics of four types built from them. A
definition written in its terms can be used as it stands.

## Decision

**A KPI's metric and a data source's semantic model are recorded in the
semantic layer's terms** (TAXONOMY.md D57), optional on both kinds,
validated by the contract and checked (`kpi-metric`).

**They leave through a port.** `internal/semantic` holds the layer in
neutral terms (`Layer`, `Model`, `Measure`, `Metric`) and two
interfaces: `Exporter`, which writes a layer in one tool's syntax, and
`Importer`, which reads one back. The engine builds the layer from the
record as a change set reads it and hands it to an exporter chosen by
configuration (`CARTOGRAPH_SEMANTIC`: `dbt`, the default, or `off`). It
never knows a syntax.

**dbt is the first adapter.** `internal/semantic/dbt` writes one YAML
file of `semantic_models` and `metrics`, the shape dbt 1.6 and later
read, held to `internal/semantic/conformance`. A ratio becomes dbt's
ratio of two simple metrics, each over its own measure, since dbt
divides metrics, not measures.

**Import comes later, through the same port.** An importer reads a dbt
project's semantic layer back, so a team whose metrics are defined in
the warehouse records them as KPIs without writing them twice. No
adapter implements it yet.

## Consequences

- An engineer exports the layer from Cartograph and puts it in the dbt
  project; the names are the record's ids, so a metric is traced back
  to its KPI by name.
- Another semantic layer (Cube, LookML, a warehouse's own) is another
  adapter; nothing in the engine changes.
- Exporting costs nothing until asked; `off` removes it.
