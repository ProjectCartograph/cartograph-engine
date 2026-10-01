# 0009. Operating at scale: probes, shutdown, metrics, and a chart

**Status:** Accepted

## Context

ADR 0008 made every replica stateless, so any number of them can serve
one database. Running many replicas behind load balancers for a long
time raises questions the code alone does not answer: what the probes
mean, how a replica leaves without dropping anyone, how idle sync
sockets survive proxies, how memory stays bounded, how a pooler fits,
what operators scale and alert on, and what they install.

## Decision

**Probes.** `/healthz` reports that the process runs. `/readyz` reports
503 only while the replica is draining. Neither checks the database.
Kubernetes's guidance, and the cascading failures it describes, both
say why: a liveness probe tied to a dependency restarts every replica
during a dependency blip, and a readiness probe tied to one removes all
capacity at once, just when the dependency is recovering.

**Shutdown.** On SIGTERM a replica:

1. Answers 503 on `/readyz` and keeps serving for `CARTOGRAPH_DRAIN_DELAY`,
   so load balancers stop routing to it first. The image has no shell,
   so a `preStop` sleep is not available.
2. Sends every sync peer a WebSocket "going away" close, so each
   reconnects at once to a replica that stays. This happens for all
   peers together, bounded at one second.
3. Gives in-flight requests `CARTOGRAPH_SHUTDOWN_TIMEOUT`.

Requests run on a context cancelled only after that. Before this, the
server's base context was the signal context, so SIGTERM cancelled
every request immediately.

**Idle connections.** The sync socket pings every
`CARTOGRAPH_SYNC_PING` (20 s), under the 60 s idle timeout of nginx and
of an AWS ALB, and drops a peer that stops answering. A Google Cloud
load balancer's backend timeout caps a WebSocket's whole lifetime
rather than its idle time; that is set on the load balancer, and
clients reconnect when it expires.

**Memory.** Each replica's shared-document cache is bounded by
`CARTOGRAPH_DOC_CACHE`, evicting the least recently used. An evicted
document is reloaded from the store. A sync state follows a document
reloaded into another module instance instead of failing the peer.

**Poolers.** PgBouncer in transaction mode does not support `LISTEN`, so
`CARTOGRAPH_FANOUT_URL` gives the fan-out's listening connection a
direct route to Postgres while everything else uses the pooler.
`NOTIFY` works through it either way.

**Metrics.** `/metrics` is served in the Prometheus text format on a
listener of its own (`CARTOGRAPH_METRICS_ADDR`). That format suits
KEDA: its Prometheus scaler is built in and production ready, while its
OpenTelemetry scaler is an experimental add-on. KEDA's metrics-api
scaler reads one URL, and behind a Service that is one pod, not the sum.
OpenTelemetry users are not left out, because the Collector's Prometheus
receiver scrapes the same endpoint. The scaling signal is
`sum(cartograph_sync_connections)`. The format is written with the
standard library (ADR 0010), and `promtool check metrics` accepts it.

**What operators install.** There are three artefacts:

- a Helm chart, `deploy/helm/cartograph`, with an HPA or a KEDA
  ScaledObject, a PodDisruptionBudget, spreading across zones, and a
  values schema. It refuses to run several replicas on a vault.
- `compose.yaml` for one local instance.
- `compose.ha.yaml`, which rehearses the HA topology on one machine.

CI lints the chart against Kubernetes schemas. It also installs the
chart with two replicas on a kind cluster, and deletes a pod while
requests keep arriving.

## Options considered

**Readiness checks the database.** This was rejected for the cascade
described above. A request that needs the database fails on its own
and reports why.

**OpenTelemetry metrics over OTLP.** This was rejected for now. KEDA's
support for it is experimental, every deployment would need a
Collector, and its SDK is large. The Prometheus endpoint feeds a
Collector anyway.

**Kustomize, or documentation only, instead of a chart.** Helm is what
most operators expect to install. The chart's schema and its refusals
also catch configurations that cannot be highly available.

## Consequences

- An operator sets environment variables, or chart values, and gets an
  HA deployment. The defaults stay right for one person on a laptop.
- Five settings join the configuration table in `DEPLOYMENT.md`.
- `just helm-lint` and `just helm-kind` join CI. The kind test takes
  about a minute and a half and runs outside the ten-second gate.
