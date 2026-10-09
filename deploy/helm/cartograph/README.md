# Cartograph Helm chart

Runs Cartograph on Kubernetes in one of two shapes:

- **Stateless** (the default): any number of replicas on a Postgres
  store, behind a Service, with no volume and no sticky sessions. This
  is the shape for a large or highly available install.
- **Vault**: one replica on a persistent volume, with no database. This
  is the shape for a small install.

The chart refuses to render more than one replica, or an autoscaler,
without a Postgres store, because a vault cannot be shared between
replicas. It also refuses two autoscalers at once, KEDA without a
Prometheus address, a termination grace period too short to drain, and
`authz.mode=roles` without an authenticator.

`docs/DEPLOYMENT.md` in the engine repository explains the topology,
the load balancer settings and the database sizing this chart assumes.

## Install

Releases publish the chart next to the image, at the engine's version:

```
helm install cartograph oci://ghcr.io/projectcartograph/charts/cartograph \
  --version 2.10.0 \
  --set store.existingSecret=cartograph-db
```

where `cartograph-db` is a Secret with the Postgres URL under `url`:

```
kubectl create secret generic cartograph-db \
  --from-literal=url='postgres://cartograph:...@db:5432/cartograph?sslmode=require&pool_max_conns=10'
```

The database starts empty. Load manifests with `cartograph import -db
"$CARTOGRAPH_STORE" -reason "first import" ./my-vault` from anywhere that
can reach the database.

One replica on a volume instead:

```
helm install cartograph oci://ghcr.io/projectcartograph/charts/cartograph \
  --version 2.10.0 --set replicaCount=1 --set vault.enabled=true
```

The volume claim is kept when the release is removed.

## What to set

- **The store.** `store.existingSecret` (with `store.urlKey`), or
  `store.url` and the chart makes the Secret. Add `pool_max_conns` to
  the URL to size each replica's pool.
- **PgBouncer.** When the store URL goes through PgBouncer in
  transaction mode, LISTEN does not work through it. Set
  `store.fanoutUrl` (or `store.fanoutUrlKey`) to a direct Postgres URL
  for the fan-out's one listening connection per replica.
- **Identity.** `auth.mode=proxy` behind an authenticating proxy that
  is the only route to the pods, and `authz` for roles, or
  `authz.mode=access` with `authz.access.mapping` for access by role
  and team from directory groups. `cartograph-oidc` puts Dex and
  oauth2-proxy in front of this chart.
- **The ingress.** The sync socket at `/api/v1/sync` is a long-lived
  WebSocket. With ingress-nginx, set
  `nginx.ingress.kubernetes.io/proxy-read-timeout: "3600"` and
  `nginx.ingress.kubernetes.io/proxy-send-timeout: "3600"`. With
  another controller, raise its idle timeout above `config.syncPing`.
- **Autoscaling.** `autoscaling.enabled` for a CPU
  HorizontalPodAutoscaler, or `keda.enabled` for a KEDA ScaledObject on
  a Prometheus query. Not both.

## Values

| Value | Default | Meaning |
|---|---|---|
| `replicaCount` | `2` | Replicas when no autoscaler is on. More than 1 needs a store |
| `image.repository` | `ghcr.io/projectcartograph/cartograph` | The image |
| `image.tag` | the chart's `appVersion` | The image tag. Use the `-chromium` tag for PDF printing |
| `image.pullPolicy` | `IfNotPresent` | |
| `imagePullSecrets` | `[]` | |
| `serviceAccount.create` | `true` | A service account of its own. Its token is never mounted |
| `store.url` | empty | Postgres URL (`CARTOGRAPH_STORE`), kept in a Secret the chart makes |
| `store.fanoutUrl` | empty | Direct Postgres URL for the listening connection (`CARTOGRAPH_FANOUT_URL`). Empty: the store URL |
| `store.existingSecret` | empty | A Secret holding the URLs. When set, `store.url` and `store.fanoutUrl` are ignored |
| `store.urlKey` | `url` | The store URL's key in `existingSecret` |
| `store.fanoutUrlKey` | empty | The fan-out URL's key in `existingSecret`. Empty: not set |
| `store.fanout` | empty | `memory` or `postgres` (`CARTOGRAPH_FANOUT`). Empty: `postgres` with a store |
| `vault.enabled` | `false` | One replica on a volume at `/vault`, no database |
| `vault.persistence.existingClaim` | empty | Use this claim instead of making one |
| `vault.persistence.storageClass` | empty | The cluster default when empty |
| `vault.persistence.accessModes` | `[ReadWriteOnce]` | |
| `vault.persistence.size` | `1Gi` | |
| `auth.mode` | `none` | `none` or `proxy` (`CARTOGRAPH_AUTH`) |
| `auth.proxyHeader` | empty | The identity header (`CARTOGRAPH_AUTH_PROXY_HEADER`). Empty: `X-Forwarded-User` |
| `authz.mode` | `allow` | `allow`, `roles` or `access` (`CARTOGRAPH_AUTHZ`) |
| `authz.readRoles`, `authz.writeRoles` | empty | Comma-separated roles, for `roles` |
| `authz.access.mapping` | `{}` | For `access`: the directory group mapping, as the access file holds it (`docs/DEPLOYMENT.md`, "Access by role and team"); rendered into a ConfigMap |
| `authz.access.existingConfigMap` | empty | Instead of a mapping: a ConfigMap with an `access.yaml` key |
| `authz.access.applyJob` | `true` | With a Postgres store, a post-install and post-upgrade Job that runs `cartograph access apply`, creating the mapped teams once. With a vault there is no Job: run `cartograph access apply /etc/cartograph/access.yaml` in the pod |
| `config.logFormat` | `json` | `text` or `json` |
| `config.logLevel` | `info` | `debug`, `info`, `warn` or `error` |
| `config.codec` | empty | `yaml` or `json`. Empty: the engine's default |
| `config.syncPing` | `20s` | WebSocket ping interval (`CARTOGRAPH_SYNC_PING`). Keep it below every proxy's idle timeout |
| `config.docCache` | `1000` | Shared documents cached per replica (`CARTOGRAPH_DOC_CACHE`) |
| `config.drainDelaySeconds` | `5` | On SIGTERM, how long `/readyz` answers 503 while the pod still serves (`CARTOGRAPH_DRAIN_DELAY`) |
| `config.shutdownTimeoutSeconds` | `10` | Grace for in-flight requests once shutdown begins (`CARTOGRAPH_SHUTDOWN_TIMEOUT`) |
| `terminationGracePeriodSeconds` | drain + shutdown + 10 | Must be greater than drain + shutdown |
| `extraEnv`, `extraEnvFrom` | `[]` | More environment for the container |
| `metrics.enabled` | `true` | Prometheus metrics on a port of their own (`CARTOGRAPH_METRICS_ADDR`) |
| `metrics.port` | `9090` | The metrics port, named `metrics` on the Service |
| `metrics.serviceMonitor.enabled` | `false` | A `monitoring.coreos.com/v1` ServiceMonitor |
| `metrics.serviceMonitor.interval`, `.scrapeTimeout`, `.labels` | `30s`, `10s`, `{}` | |
| `service.type`, `service.port` | `ClusterIP`, `8080` | No session affinity |
| `ingress.enabled` | `false` | |
| `ingress.className` | empty | |
| `ingress.annotations` | `{}` | Set the WebSocket timeouts here (above) |
| `ingress.hosts`, `ingress.tls` | one host, `[]` | |
| `resources` | 100m CPU, 128Mi requested; 512Mi limit | |
| `podSecurityContext`, `securityContext` | uid and gid 65532, non-root, read-only root, no capabilities | `/tmp` is an emptyDir |
| `livenessProbe` | `/healthz` | The process. Never depends on the database |
| `readinessProbe` | `/readyz` | 503 only while draining. Does not check the database |
| `startupProbe` | `/healthz`, up to 60 s | |
| `podDisruptionBudget.enabled` | `true` | Rendered with more than one replica or an autoscaler |
| `podDisruptionBudget.minAvailable` | `1` | |
| `podDisruptionBudget.maxUnavailable` | empty | Wins over `minAvailable` when set |
| `autoscaling.enabled` | `false` | A CPU HorizontalPodAutoscaler |
| `autoscaling.minReplicas`, `.maxReplicas` | `2`, `10` | |
| `autoscaling.targetCPUUtilizationPercentage` | `70` | |
| `keda.enabled` | `false` | A KEDA ScaledObject with a Prometheus trigger |
| `keda.minReplicaCount`, `.maxReplicaCount` | `2`, `10` | |
| `keda.pollingInterval`, `.cooldownPeriod` | `30`, `300` | Seconds |
| `keda.prometheus.serverAddress` | empty | Required with `keda.enabled` |
| `keda.prometheus.query` | `sum(cartograph_sync_connections)` | |
| `keda.prometheus.threshold` | `200` | Sync connections per replica |
| `topologySpreadConstraints` | across zones, then hosts | Preferred, never blocks scheduling |
| `affinity` | prefer hosts without another replica | |
| `nodeSelector`, `tolerations`, `priorityClassName` | empty | |

`values.schema.json` checks types and enumerations before anything
renders.

## Testing the chart

From the engine repository:

```
just helm-lint   # lint, refusals, and kubeconform on every file in ci/; no cluster
just helm-kind   # kind cluster, the flake's image, two replicas on Postgres, probes, lose a pod
```
