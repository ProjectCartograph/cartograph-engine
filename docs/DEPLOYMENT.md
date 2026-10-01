# Deploying Cartograph

Cartograph is one binary that serves a directory, or a Postgres
database. Everything a deployment
decides is an environment variable; a flag overrides it; a default works
on a laptop. This page is the operator's view. `ARCHITECTURE.md` says why
things are shaped this way.

## The shortest version

```
cartograph serve ./my-vault
```

Open http://localhost:8080. The directory may be empty; the first save
creates `vault.yaml` and the index under `.cartograph/`. Stop it with Ctrl-C
or SIGTERM; in-flight requests finish first.

## Configuration

Precedence is flag, then environment, then default. `cartograph serve -h`
prints the same table.

| Variable | Flag | Default | Meaning |
|---|---|---|---|
| `CARTOGRAPH_ADDR` | `-addr` | `:8080` | Listen address. A bare port (`8080`) is accepted, so platforms that hand out `PORT` can pass it straight through |
| `CARTOGRAPH_VAULT` | `-vault` | `.` | The vault directory, or a SQLite file. A positional argument wins over both |
| `CARTOGRAPH_STORE` | `-store` | empty | A `postgres://` URL. When set, the database is the store and `CARTOGRAPH_VAULT` is ignored. See "A stateless deployment" |
| `CARTOGRAPH_FANOUT` | `-fanout` | see meaning | How replicas tell each other a document changed: `memory` (one replica) or `postgres`. Empty means `postgres` with a Postgres store and `memory` with a vault |
| `CARTOGRAPH_WATCH` | `-watch` | `true` | Reload manifests when their files change. Turn off on a read-only or network filesystem where inotify misbehaves |
| `CARTOGRAPH_CHROMIUM` | `-chromium` | empty | Browser to print PDFs with. Empty means the vault's `Settings.spec.chromium`, then `CHROMIUM`, then the PATH |
| `CARTOGRAPH_LOG_FORMAT` | `-log-format` | `text` | `text` or `json`. Always stdout |
| `CARTOGRAPH_LOG_LEVEL` | `-log-level` | `info` | `debug` logs every request |
| `CARTOGRAPH_SHUTDOWN_TIMEOUT` | `-shutdown-timeout` | `10s` | Grace period after SIGTERM |
| `CARTOGRAPH_AUTH` | `-auth` | `none` | `none` or `proxy` (below) |
| `CARTOGRAPH_AUTH_PROXY_HEADER` | `-auth-proxy-header` | `X-Forwarded-User` | Header the proxy adapter reads |
| `CARTOGRAPH_AUTHZ` | `-authz` | `allow` | `allow` (every principal may do everything) or `roles` (below). `roles` needs an authenticator |
| `CARTOGRAPH_READ_ROLES` | `-read-roles` | empty | Comma-separated roles that may read. Empty: any authenticated principal may read |
| `CARTOGRAPH_WRITE_ROLES` | `-write-roles` | empty | Comma-separated roles that may write. Empty: nobody may write |
| `CARTOGRAPH_CODEC` | `-codec` | `yaml` | The manifest syntax, `yaml` or `json`. A vault is written in one; see "Changing the manifest syntax" |
| `CARTOGRAPH_IMPORT` | `-import` | empty | Import this directory once at start-up |

Nothing is read from a configuration file. If an organisation wants one,
the place to add it is `internal/config`, reading the file first and
letting the environment override it, and nowhere else.

## Health

| Path | Means |
|---|---|
| `GET /healthz` | The process is up. Always 200 once listening |
| `GET /readyz` | The process will take requests. 503 during shutdown, so a load balancer drains it |
| `GET /api/v1/health` | The API answers. Part of the contract; the web interface uses it |

Probes are not logged.

## Logs

One line per event on stdout, text by default, JSON with
`CARTOGRAPH_LOG_FORMAT=json`. Requests are logged at `debug`, and at `warn`
when the status is 5xx. Whatever runs the process decides where stdout
goes; Cartograph never opens a log file.

## The container image

The image is built by the flake, so it is the same build as the binary,
on either architecture, and there is nothing to keep in step with it:

```
just image                   # nix build .#image, loaded into docker as cartograph:<version> and cartograph:local
nix build .#image-chromium   # the same plus Chromium, for PDF printing (about 400 MB larger)
```

Releases publish both to the organisation's container registry as
multi-architecture tags (`<version>` and `<version>-chromium`), built
on x86_64 and aarch64 runners from the same flake. The image runs as
uid 65532, owns `/vault`, sets `CARTOGRAPH_VAULT=/vault` and
`CARTOGRAPH_LOG_FORMAT=json`, exposes 8080, and checks itself with `cartograph
ready` (there is no shell and no curl in it). Mount a volume at
`/vault`; that volume is the only state. `compose.yaml` does the same
with the health check, on `cartograph:local` (or `CARTOGRAPH_IMAGE`).

Seed the volume as the runtime user before the first start, and never
mount a tracked example directory in place (the server writes `.cartograph/`
and `vault.yaml` into whatever it serves):

```
docker volume create cartograph-vault
docker run --rm -v cartograph-vault:/vault -v "$PWD/examples/minimal:/src:ro" busybox \
    sh -c 'cp -r /src/. /vault/ && chown -R 65532:65532 /vault'
docker run -d -p 8080:8080 -v cartograph-vault:/vault cartograph:<version>
```

## Identity

Cartograph records who did what but does not verify anyone itself. Two
adapters exist.

**`CARTOGRAPH_AUTH=none`** (default). Every request is anonymous and every
write is recorded as the vault's `Settings.spec.operator` (default
`local`). Right for one operator on a trusted network, and for the
command line, which always runs this way.

**`CARTOGRAPH_AUTH=proxy`.** Cartograph sits behind an authenticating reverse proxy
(oauth2-proxy, Pomerium, Authelia, an ingress controller with OIDC) that
verifies the person and forwards their identity in a header. Cartograph trusts
`X-Forwarded-User` (the subject), `X-Forwarded-Preferred-Username` (a
display name) and `X-Forwarded-Groups` (comma-separated roles). A request
without the subject header gets 401.

This is only safe when the proxy is the only route to the port. Bind
`CARTOGRAPH_ADDR` to a private interface or a container network the proxy
alone can reach. A client that can reach Cartograph directly can set the
header and be anyone. The same caveat applies to every system that uses
this pattern; it is not special to Cartograph.

Live editing uses a WebSocket at `/api/v1/sync`, behind the same
authentication and policy as every other request. The proxy must pass
WebSocket upgrades on that path and allow long-lived connections there.

**Authorization.** `CARTOGRAPH_AUTHZ=allow` (default) lets every principal do
everything, which is right when the proxy already decides who may reach
Cartograph at all. `CARTOGRAPH_AUTHZ=roles` applies a policy from the groups the
authenticator forwards: anyone in `CARTOGRAPH_WRITE_ROLES` may read and
write, anyone in `CARTOGRAPH_READ_ROLES` may read, everyone else gets 403
with a message naming the actor and the verb, and an anonymous request
is refused outright (so pair it with `CARTOGRAPH_AUTH=proxy`). An empty read
list means any authenticated person may read. The policy is one check in
front of the whole API, so the command line, which runs in-process with
no identity, is unaffected. Finer policies are adapters; see
`EXTENDING.md`.

```
CARTOGRAPH_AUTH=proxy CARTOGRAPH_AUTHZ=roles CARTOGRAPH_READ_ROLES=staff CARTOGRAPH_WRITE_ROLES=planners,admins cartograph serve /vault
```

## PDF printing

The engine never shells out. The `printer.Printer` port has one adapter,
headless Chromium, found in this order: `CARTOGRAPH_CHROMIUM`, the vault's
`Settings.spec.chromium`, the `CHROMIUM` environment variable, then
`chromium`, `chromium-browser`, `google-chrome` or
`google-chrome-stable` on the PATH. With none found the server logs a
warning at start and refuses PDF requests with a 422 that says so; HTML
and JSON documents still render, and a handoff still goes through
without the PDF.

## Changing the manifest syntax

A vault is written in one syntax, YAML unless `CARTOGRAPH_CODEC=json`. The
engine parses none of it itself; the codec does, and the store keeps the
text byte for byte, which is what makes versions and diffs the person's
own text. So the syntax is not a flag to flip on live files. To move a
vault:

```
cartograph export /tmp/vault-json /path/to/vault --codec json
CARTOGRAPH_CODEC=json cartograph serve /tmp/vault-json
```

Every manifest is decoded and re-encoded; `vault.yaml` is regenerated on
first open (it is the adapter's own file and stays YAML in both). Version
history does not travel: the export is a new vault at version 1 of
everything, which is the same as any export.

## Backups and restore

For a Postgres store, see "A stateless deployment". For a vault, the
directory is the whole state. `.cartograph/` inside it is a cache.
A backup is a copy of the directory, or a git commit of it. A restore is
putting the directory back and starting the server; the index rebuilds.
Version numbers restart from what the files say, which is why versions
that matter are snapshotted (`cartograph snapshot`) and why a vault under git
loses nothing.

## Several replicas

Two processes can serve the same vault directory read-mostly; the
watcher keeps them in step. Two processes writing to one directory are
not arbitrated across processes. For more than one writer, use a
Postgres store.

## A stateless deployment

Set `CARTOGRAPH_STORE` to a Postgres URL and the replicas keep nothing
of their own:

```
CARTOGRAPH_STORE=postgres://cartograph:secret@db.internal/cartograph?sslmode=require
cartograph serve
```

- One database holds everything that outlives a request: versions,
  working copies, references, exclusions, project state history,
  handoff bundles and the shared drafts. The tables are created, and
  later migrated, when a replica starts. Replicas that start together
  take turns, so each migration runs once.
- Any replica serves any request. Start, stop or kill replicas at any
  time; no volume, no sticky sessions, no index to rebuild.
- Each replica holds one extra connection to the database, which only
  listens. That is the fan-out: when one replica stores a change to a
  shared draft, it sends a notification, and every replica tells the
  people connected to it. If that connection drops, the replica
  reconnects and listens again. A notification lost meanwhile costs
  latency, never an edit, because each client compares its state with
  the server's on every exchange.
- Size the database's `max_connections` for the pool of every replica
  (pgx's default is the larger of 4 and the number of CPUs) plus one
  listening connection each.
- Any parameter in the URL that the driver does not know is passed to
  the server, so `?search_path=cartograph` keeps the tables in a schema
  of their own.
- There is no apply gate: every row is live, and the state endpoints
  answer that this store has no state manifest. Files come in through
  `cartograph import` or `CARTOGRAPH_IMPORT`, and go out through
  `cartograph export`.
- `CARTOGRAPH_FANOUT=memory` with a Postgres store is for one replica
  that wants no listening connection. With more than one replica,
  people on different replicas would then see each other's edits only
  when they next sync.

A backup is the database's own (`pg_dump`, or the provider's
snapshots). `cartograph export` writes the manifests as files as well.

## Large-scale, highly available

The stateless deployment above scales out. This section is how to run
it for many people, with no single replica that matters.

### The topology

```
clients ── load balancer ── replica 1 ─┐
           (no affinity)    replica 2 ─┼── PgBouncer ── Postgres primary (+ standbys)
                            replica N ─┘        └──── direct: one LISTEN per replica
```

- N stateless replicas, all with the same `CARTOGRAPH_STORE`, behind
  any load balancer. No sticky sessions: any replica serves any
  request, and a sync socket that moves to another replica resumes
  from heads.
- Postgres is the one stateful part, and its high availability is the
  operator's. Use a managed service, or an operator such as
  CloudNativePG, with a standby and automatic failover. Cartograph
  reconnects after a failover; a change refused meanwhile stays with
  the client, which sends it again.
- Through PgBouncer in transaction mode, two things change. LISTEN
  does not work through such a pooler, so set `CARTOGRAPH_FANOUT_URL`
  to a direct Postgres URL; only the one listening connection per
  replica uses it. And pgx prepares statements, which transaction
  pooling breaks: add `default_query_exec_mode=exec` to the store URL,
  or turn on PgBouncer's prepared statement support
  (`max_prepared_statements`, PgBouncer 1.21 or later).

`compose.ha.yaml` rehearses this on one machine: Postgres, a one-shot
import of the example, two replicas, and nginx in front (`just image`,
then `just ha-up`, and `just ha-down` to stop). The Helm chart in
`deploy/helm/cartograph` deploys it on Kubernetes.

### Settings for this shape

| Variable | Default | Meaning |
|---|---|---|
| `CARTOGRAPH_FANOUT_URL` | the store URL | A direct Postgres URL for the listening connection, when the store URL goes through a transaction-mode pooler |
| `CARTOGRAPH_METRICS_ADDR` | empty (off) | Serve Prometheus `/metrics` on this address, a listener of its own, for example `:9090` |
| `CARTOGRAPH_SYNC_PING` | `20s` | How often the server pings an open sync socket. Below the 60 second idle timeout of nginx and of an AWS load balancer |
| `CARTOGRAPH_DOC_CACHE` | `1000` | Shared documents cached per replica. A cache only: the store is the truth |
| `CARTOGRAPH_DRAIN_DELAY` | `0` | On SIGTERM, how long `/readyz` answers 503 while the replica still serves, before shutdown begins |

### Connection budget

Each replica opens a pool and one listening connection. So the
database needs at least

```
replicas × (pool_max_conns + 1)
```

connections, plus room for migrations, backups and people. Set the
pool size in the URL: `?pool_max_conns=10`. pgx's default is the larger
of 4 and the number of CPUs it sees, which on a large node is more than
a replica needs. With PgBouncer, the pools count against PgBouncer and
only the listening connections reach Postgres directly. Size an
autoscaler's maximum from this sum, not the other way round.

### Load balancers and WebSockets

The sync socket at `/api/v1/sync` is a WebSocket that stays open as
long as the page does. Every proxy in the path must pass the upgrade
headers (`Upgrade`, `Connection`) and allow long idle periods.

- nginx closes an idle proxied connection after 60 seconds
  (`proxy_read_timeout`). Raise it to an hour; with ingress-nginx, set
  `nginx.ingress.kubernetes.io/proxy-read-timeout` and
  `proxy-send-timeout` to `"3600"`.
- An AWS Application Load Balancer has a 60 second idle timeout by
  default. The ping keeps an idle socket under it; raising it does no
  harm.
- A Google Cloud backend service's timeout is the longest a WebSocket
  may live, idle or not. Set it to the longest session you want before
  a reconnect, for example an hour.

When a socket closes anyway, the client reconnects and resumes from
heads. Nothing is lost; a reconnect costs one round trip. The server's
ping (`CARTOGRAPH_SYNC_PING`) keeps idle sockets open through proxies
that count idle time.

### Probes

- `/healthz` is liveness. It says the process runs, and it never
  touches the database. A database outage must not make the platform
  restart every replica.
- `/readyz` is readiness. It answers 503 only while the replica drains
  or stops. It deliberately does not check the database either: if it
  did, a short database blip would mark every replica unready at once,
  and the load balancer would turn a partial outage into a total one.
  While the database is away, replicas still answer, refuse writes
  they cannot store, and recover on their own.
- A startup probe on `/healthz` covers the first start, which compiles
  the schemas and the Automerge module.

### Graceful rollout

On SIGTERM a replica answers 503 on `/readyz` for
`CARTOGRAPH_DRAIN_DELAY` while it keeps serving, so load balancers
that poll readiness stop sending it new requests. Then it stops
accepting, and in-flight requests get `CARTOGRAPH_SHUTDOWN_TIMEOUT` to
finish. Open sync sockets close, and their clients reconnect to another
replica.

On Kubernetes, set the pod's `terminationGracePeriodSeconds` above the
drain delay plus the shutdown timeout, or the kubelet kills a replica
that is still draining. The image has no shell, so a `preStop` hook
that sleeps is not possible; the drain delay does that job. A
PodDisruptionBudget (`minAvailable: 1`, or `maxUnavailable: 1`) keeps
node drains from taking every replica at once, and a rolling update
with `maxUnavailable: 0` adds a replica before it removes one. The
chart sets all of this.

### Autoscaling

Two choices, never both on one Deployment:

- **KEDA on Prometheus.** Scale on open sync connections, which is what
  costs memory and file descriptors. For example, one replica per 200
  connections:

  ```yaml
  apiVersion: keda.sh/v1alpha1
  kind: ScaledObject
  metadata:
    name: cartograph
  spec:
    scaleTargetRef:
      name: cartograph
    minReplicaCount: 2
    maxReplicaCount: 20
    triggers:
      - type: prometheus
        metadata:
          serverAddress: http://prometheus-operated.monitoring.svc:9090
          query: sum(cartograph_sync_connections)
          threshold: "200"
  ```

- **A HorizontalPodAutoscaler on CPU.** Simpler, and right when most
  traffic is plain API requests.

Keep the minimum at 2 or more, so one replica can go away.

### Metrics

With `CARTOGRAPH_METRICS_ADDR` set, each replica serves Prometheus
metrics on that address:

| Metric | Type | Means |
|---|---|---|
| `cartograph_sync_connections` | gauge | Open sync sockets on this replica |
| `cartograph_sync_documents_open` | gauge | Documents those sockets have open |
| `cartograph_shared_documents_cached` | gauge | Shared documents in this replica's cache |
| `cartograph_http_requests_total` | counter | Requests served |
| `cartograph_http_request_duration_seconds` | histogram | Request latency |
| `cartograph_fanout_*` | counters | Fan-out activity: messages sent and received, and the listener's reconnects |
| `go_*`, `process_*` | | The Go runtime and the process |

The metrics listener is separate from the API, so it can stay off
the load balancer and away from the public network.

### Several regions

There is one Postgres primary, so writes go to one region. Replicas in
other regions work, at the cost of the round trip to the primary on
every write. Fan-out over `LISTEN/NOTIFY` needs every replica connected
to that primary. Fan-out within each region, with a broker between
regions, needs a fan-out adapter for such a broker (NATS, for example).
That is a distribution's concern, one adapter behind the `fanout.Bus`
port (ADR 0008), and nothing else changes.

## Upgrading

Replace the binary and restart. The contract is versioned (`/api/v1`);
a vault written by an older binary opens in a newer one, and the schemas
are in the binary, so a manifest that no longer validates shows as a
problem on its next save, not as a refusal to start.
