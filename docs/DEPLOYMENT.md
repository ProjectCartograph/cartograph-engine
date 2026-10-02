# Deploying Cartograph

Cartograph is one binary that serves a directory, or a Postgres
database. Everything a deployment decides is an environment variable; a
flag overrides it; a default works on a laptop. This page is the
operator's view. `ARCHITECTURE.md` says why things are shaped this way.

## The shortest version

```
cartograph serve ./my-vault
```

Open http://localhost:8080. The directory may be empty; the first save
creates `vault.yaml` and the index under `.cartograph/`. Stop it with
Ctrl-C or SIGTERM; in-flight requests finish first.

## Configuration

Precedence is flag, then environment, then default. `cartograph serve -h`
prints the same table.

| Variable | Flag | Default | Meaning |
|---|---|---|---|
| `CARTOGRAPH_ADDR` | `-addr` | `:8080` | Listen address. A bare port (`8080`) is accepted, so platforms that hand out `PORT` can pass it straight through |
| `CARTOGRAPH_VAULT` | `-vault` | `.` | The vault directory, or a SQLite file. A positional argument wins over both |
| `CARTOGRAPH_STORE` | `-store` | empty | A `postgres://` URL. When set, the database is the store and `CARTOGRAPH_VAULT` is ignored. See "A stateless deployment" |
| `CARTOGRAPH_FANOUT` | `-fanout` | see meaning | How replicas tell each other a document changed: `memory` (one replica) or `postgres`. Empty means `postgres` with a Postgres store and `memory` with a vault |
| `CARTOGRAPH_FANOUT_URL` | `-fanout-url` | the store URL | A direct Postgres URL for the fan-out's listening connection. Set it when the store URL goes through a transaction-mode pooler such as PgBouncer, where `LISTEN` does not work |
| `CARTOGRAPH_METRICS_ADDR` | `-metrics-addr` | empty (off) | Serve Prometheus `/metrics` on this address, a listener of its own kept off the address users reach, for example `:9090` |
| `CARTOGRAPH_SYNC_PING` | `-sync-ping` | `20s` | How often the sync socket pings an idle peer; `0` turns pings off. Keep it below the shortest idle timeout between users and the replica (60 s for nginx and an AWS load balancer by default) |
| `CARTOGRAPH_SYNC_IDLE` | `-sync-idle` | `10m` | Close a sync socket that has changed no document for this long, with code 4000, so an unattended window does not hold a replica up (ADR 0015). Pings and presence do not count. `0` never closes |
| `CARTOGRAPH_DOC_CACHE` | `-doc-cache` | `1000` | Shared documents a replica keeps in memory, least recently used dropped first. A cache only: the store holds every change |
| `CARTOGRAPH_COMPACT_AFTER` | `-compact-after` | `24h` | With a Postgres store, how old a version (never a manifest's latest) must be before it is kept as a patch against the one before it instead of whole. It is also how long a rolling upgrade from 2.2 has to finish, since a 2.2 replica cannot read a compacted version. `0` turns compaction off |
| `CARTOGRAPH_REPORTS` | `-reports` | `computed` | Reporting (ADR 0014): `computed` from the engine on any store, `postgres` as views in a Postgres store, or `off` |
| `CARTOGRAPH_MCP` | `-mcp` | `off` | `on` serves agents over MCP at `/api/v1/mcp` (ADR 0016) |
| `CARTOGRAPH_MCP_AUTH` | `-mcp-auth` | `proxy` | Who authorizes agents: `cartograph`, Cartograph's own authorization server, or `proxy`, whatever authenticates every other request ("Agents" below) |
| `CARTOGRAPH_MCP_ISSUER` | `-mcp-issuer` | empty | The authorization server MCP clients sign in with, published as protected resource metadata at `/.well-known/oauth-protected-resource`. With `CARTOGRAPH_MCP_AUTH=cartograph`, Cartograph's own public address |
| `CARTOGRAPH_AGENT_KEY` | none | empty | With `CARTOGRAPH_MCP_AUTH=cartograph`: the secret its tokens are signed with, at least 32 bytes, the same on every replica |
| `CARTOGRAPH_DRAIN_DELAY` | `-drain-delay` | `0` | After SIGTERM, how long the replica keeps serving with `/readyz` at 503 so load balancers stop sending it work. Set it to at least the load balancer's health-check interval |
| `CARTOGRAPH_WATCH` | `-watch` | `true` | Reload manifests when their files change. Turn off on a read-only or network filesystem where inotify misbehaves |
| `CARTOGRAPH_CHROMIUM` | `-chromium` | empty | Browser to print PDFs with. Empty means the vault's `Settings.spec.chromium`, then `CHROMIUM`, then the PATH |
| `CARTOGRAPH_LOG_FORMAT` | `-log-format` | `text` | `text` or `json`. Always stdout |
| `CARTOGRAPH_LOG_LEVEL` | `-log-level` | `info` | `debug` logs every request |
| `CARTOGRAPH_SHUTDOWN_TIMEOUT` | `-shutdown-timeout` | `10s` | Grace period after SIGTERM |
| `CARTOGRAPH_AUTH` | `-auth` | `none` | `none` or `proxy` (below) |
| `CARTOGRAPH_AUTH_PROXY_HEADER` | `-auth-proxy-header` | `X-Forwarded-User` | Header the proxy adapter reads |
| `CARTOGRAPH_AUTHZ` | `-authz` | `allow` | `allow` (every principal may do everything), `roles` or `access` (below). `roles` and `access` need an authenticator |
| `CARTOGRAPH_ACCESS_FILE` | `-access-file` | empty | For `access`: the mapping from directory groups to roles and teams. Empty: no group grants anything |
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

Probes are not logged. Neither probe checks the database, on purpose. A
liveness probe that did would restart every replica during a database
blip, and a readiness probe that did would take every replica out of
service at once, exactly when the database is recovering. A request that
needs the database fails on its own and says so.

## Logs

One line per event on stdout, text by default, JSON with
`CARTOGRAPH_LOG_FORMAT=json`. Requests are logged at `debug`, and at
`warn` when the status is 5xx. Whatever runs the process decides where
stdout goes; Cartograph never opens a log file.

## The container image

The image is built by the flake, so it is the same build as the binary,
on either architecture, and there is nothing to keep in step with it:

```
just image                   # nix build .#image, loaded into docker as cartograph:<version> and cartograph:local
nix build .#image-chromium   # the same plus Chromium, for PDF printing (about 400 MB larger)
```

Releases publish both to the organisation's container registry as
multi-architecture tags (`<version>` and `<version>-chromium`), built on
x86_64 and aarch64 runners from the same flake. The image runs as uid
65532, owns `/vault`, sets `CARTOGRAPH_VAULT=/vault` and
`CARTOGRAPH_LOG_FORMAT=json`, exposes 8080, and checks itself with
`cartograph ready` (there is no shell and no curl in it). Mount a volume
at `/vault`; that volume is the only state. `compose.yaml` does the same
with the health check, on `cartograph:local` (or `CARTOGRAPH_IMAGE`).

Seed the volume as the runtime user before the first start, and never
mount a tracked example directory in place (the server writes
`.cartograph/` and `vault.yaml` into whatever it serves):

```
docker volume create cartograph-vault
docker run --rm -v cartograph-vault:/vault -v "$PWD/examples/minimal:/src:ro" busybox \
    sh -c 'cp -r /src/. /vault/ && chown -R 65532:65532 /vault'
docker run -d -p 8080:8080 -v cartograph-vault:/vault cartograph:<version>
```

## Identity

Cartograph records who did what but does not verify anyone itself. Two
adapters exist.

With `CARTOGRAPH_AUTH=none` (the default), every request is anonymous
and every write is recorded as the vault's `Settings.spec.operator`
(default `local`). This is right for one operator on a trusted network,
and for the command line, which always runs this way.

With `CARTOGRAPH_AUTH=proxy`, Cartograph sits behind an authenticating
reverse proxy (oauth2-proxy, Pomerium, Authelia, an ingress controller
with OIDC) that verifies the person and forwards their identity in a
header. Cartograph trusts `X-Forwarded-User` (the subject),
`X-Forwarded-Preferred-Username` (a display name, shown to others on the
same screen), `X-Forwarded-Email` (the organisation address, which an
access list knows people by) and `X-Forwarded-Groups` (comma-separated
groups). These are the headers oauth2-proxy sets. A request without the
subject header gets 401.

This is only safe when the proxy is the only route to the port. Bind
`CARTOGRAPH_ADDR` to a private interface or a container network the
proxy alone can reach. A client that can reach Cartograph directly can
set the header and be anyone. The same caveat applies to every system
that uses this pattern, not only to Cartograph.

Live editing uses a WebSocket at `/api/v1/sync`, behind the same
authentication and policy as every other request. The proxy must pass
WebSocket upgrades on that path and allow long-lived connections there.

For authorization, `CARTOGRAPH_AUTHZ=allow` (the default) lets every
principal do everything, which is right when the proxy already decides
who may reach Cartograph at all. `CARTOGRAPH_AUTHZ=roles` applies a
policy from the groups the authenticator forwards: anyone in
`CARTOGRAPH_WRITE_ROLES` may read and write, anyone in
`CARTOGRAPH_READ_ROLES` may read, everyone else gets 403 with a message
naming the actor and the verb, and an anonymous request is refused
outright (so pair it with `CARTOGRAPH_AUTH=proxy`). An empty read list
means any authenticated person may read. The policy is one check in
front of the whole API, so the command line, which runs in-process with
no identity, is unaffected. Finer policies are adapters; see
`EXTENDING.md`.

```
CARTOGRAPH_AUTH=proxy CARTOGRAPH_AUTHZ=roles CARTOGRAPH_READ_ROLES=staff CARTOGRAPH_WRITE_ROLES=planners,admins cartograph serve /vault
```

### Access by role and team

`CARTOGRAPH_AUTHZ=access` is the policy an organisation with many teams
needs (ADR 0011, TAXONOMY.md D27). Only people on the access list may
sign in. Each holds roles: a *reader* sees everything; a *contributor*
changes the projects, programmes, operations and data sources of their
teams and the teams beneath them, and keeps the shared registers; a
*strategy editor* shapes the goals and the vision and mission; an
*administrator* does everything, for every team, and manages access
from the Access page. The engine checks each write against the teams
the manifest belongs to, before and after the change, including edits
on the sync socket.

People get there two ways. An administrator adds them by address and
grants roles and teams. Or their directory groups do it when they sign
in, by the mapping in `CARTOGRAPH_ACCESS_FILE`:

```yaml
roles:
  reader: [all-staff]
  contributor: [curriculum-division, early-grades-team, assessment-unit]
  strategyEditor: [planning-unit]
  administrator: [cartograph-administrators]
teams:
  - group: curriculum-division
    name: Curriculum division
  - group: early-grades-team
    name: Early grades
    parent: Curriculum division
  - group: assessment-unit
    name: Assessment
```

A person whose groups grant a role is listed at their first sign-in.
At every sign-in their groups' roles and teams are refreshed; what an
administrator granted is kept apart and never undone by it. A team is
known by its name, which is unique among teams; its parent is another
team's name.

The mapped teams are created by one command, run once per deployment
before the replicas start, so two replicas never both create one:

```
cartograph access apply /etc/cartograph/access.yaml -store "$CARTOGRAPH_STORE"
```

Where no group grants the administrator role, name the first
administrator the same way:

```
cartograph access grant someone@example.org -roles administrator -store "$CARTOGRAPH_STORE"
```

The groups come from the identity provider through the proxy. With
OIDC, the provider must put them in a `groups` claim and the proxy must
forward it; an identity broker such as Dex does both for Entra ID,
LDAP, SAML and other providers. `cartograph-oidc` is a complete,
tested example: Dex, oauth2-proxy, a directory, compose files and Helm
values.

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

A vault is written in one syntax, YAML unless `CARTOGRAPH_CODEC=json`.
The engine parses none of it itself; the codec does, and the store keeps
the text byte for byte, which is what makes versions and diffs the
person's own text. So the syntax is not a flag to flip on live files. To
move a vault:

```
cartograph export /tmp/vault-json /path/to/vault --codec json
CARTOGRAPH_CODEC=json cartograph serve /tmp/vault-json
```

Every manifest is decoded and re-encoded; `vault.yaml` is regenerated on
first open (it is the adapter's own file and stays YAML in both).
Version history does not travel. The export is a new vault at version 1
of everything, as any export is.

## Backups and restore

For a Postgres store, see "A stateless deployment". For a vault, the
directory is the whole state. `.cartograph/` inside it is a cache. A
backup is a copy of the directory, or a git commit of it. A restore is
putting the directory back and starting the server; the index rebuilds.
Version numbers restart from what the files say, which is why versions
that matter are snapshotted (`cartograph snapshot`) and why a vault
under git loses nothing.

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
  time. There is no volume, no sticky session and no index to rebuild.
- Each replica holds one extra connection to the database, which only
  listens. That connection is the fan-out. When one replica stores a
  change to a shared draft, it sends a notification, and every replica
  tells the people connected to it. If that connection drops, the
  replica reconnects and listens again. A notification lost meanwhile
  costs latency, never an edit, because each client compares its state
  with the server's on every exchange.
- Size the database's `max_connections` for the pool of every replica
  (pgx's default is the larger of 4 and the number of CPUs) plus one
  listening connection each.
- Any parameter in the URL that the driver does not know is passed to
  the server, so `?search_path=cartograph` keeps the tables in a schema
  of their own.
- There is no apply gate. Every row is live, and the state endpoints
  answer that this store has no state manifest. Files come in through
  `cartograph import` or `CARTOGRAPH_IMPORT`, and go out through
  `cartograph export`.
- `CARTOGRAPH_FANOUT=memory` with a Postgres store is for one replica
  that wants no listening connection. With more than one replica,
  people on different replicas would then see each other's edits only
  when they next sync.

A backup is the database's own (`pg_dump`, or the provider's
snapshots). `cartograph export` writes the manifests as files as well.

### How the database keeps manifests

Each manifest is a document, `jsonb`, whatever codec the deployment
writes (ADR 0012). `manifests` has one row per manifest: its current
version, name, labels, team, and document, and its working copy's
document when one is pending. Earlier versions are kept as a keyframe
every 16 versions and a JSON Patch for each one between, once they are
older than `CARTOGRAPH_COMPACT_AFTER` (ADR 0013). A series, such as a
KPI's readings, is also kept one row per item in `series_items`, with
who recorded it and when. `events` lists every save, state change and
reading in order. References, project state, the access list and the
shared drafts are tables of their own. Every read the interface makes is
an index lookup bounded by what it returns.

## Agents

Agents connect over MCP, off unless `CARTOGRAPH_MCP=on`. An agent acts
for the person who connected it, with that person's access and no more,
and it reads and proposes: it may read, validate, run checks and edit
drafts, where people on the same manifest see it working, but saving a
version, recording a reading, moving or handing off a project and
deleting are proposals its person accepts or declines under Proposals in
the interface (ADR 0016).

An agent is held to what a person meets in the editor (ADR 0017): it
reads the kind's guide first, which joins the contract's guidance to the
organisation's own records, and a proposal whose checks are open is
refused unless the agent says why each is left open. Manifests that
reference each other are proposed and accepted as one set. Every
proposal is its manifests' draft, opened in the editor, and is read on
its review page before it is decided.

A person follows their agents as they work (ADR 0018): each step goes to
a feed only they, or an administrator, may open, and following moves
their view with the agent. Following needs the agent on the same
deployment as the interface, over HTTP (`/api/v1/mcp`); an agent on
stdio (`cartograph mcp`) runs in its own process and has no one to tell.

With an access list, the mapping's `agents:` key names the roles that
may use one, and an administrator can turn one person's agents off on
the Access page:

```yaml
agents: [contributor, strategyEditor]
```

### How an agent signs in

A person adds Cartograph's MCP address (`https://cartograph.example.org/api/v1/mcp`)
to their client. The client opens a browser, the person signs in as
usual, sees what the agent may do, and clicks Allow. That flow is the
MCP specification's, and who runs it is your choice:

**Cartograph runs it** (`CARTOGRAPH_MCP_AUTH=cartograph`). Nothing is
registered anywhere beforehand: a client registers itself with
Cartograph, by a Client ID Metadata Document or dynamic registration,
whichever it supports. Sign-in still goes through your identity
provider, with its MFA and policies, because the consent page sits
behind your proxy like every other page. The agent then holds a
one-hour token and a refresh token that Cartograph signs; each grant is
listed on the Access page, where its person or an administrator
disconnects it at once. For a client with no browser flow, a person
creates a token to paste on the same page.

```sh
CARTOGRAPH_MCP=on
CARTOGRAPH_MCP_AUTH=cartograph
CARTOGRAPH_MCP_ISSUER=https://cartograph.example.org
CARTOGRAPH_AGENT_KEY=...   # openssl rand -base64 48, from a secret store
```

The proxy passes these paths through unchecked: Cartograph checks the
token on each, and on `/api/v1/mcp` it reads no header the proxy sets.
Keep `/oauth/authorize` behind sign-in.

```
/api/v1/mcp
/oauth/register
/oauth/token
/.well-known/oauth-authorization-server
/.well-known/oauth-protected-resource
/.well-known/oauth-protected-resource/api/v1/mcp
```

An agent's requests carry no directory groups, so its person's roles
are the ones recorded at their last sign-in, and only for 30 days after
it. A code or refresh token used twice ends its grant, since one of the
two was not the agent's.

**Your own stack runs it** (`CARTOGRAPH_MCP_AUTH=proxy`, the default).
Set `CARTOGRAPH_MCP_ISSUER` to the authorization server, and have the
proxy check its bearer tokens on `/api/v1/mcp` and pass the same
identity headers as for a browser (oauth2-proxy: `skip_jwt_bearer_tokens`
with the issuer in `extra_jwt_issuers`). Leave
`/.well-known/oauth-protected-resource` public. Choose this when your
authorization server registers clients itself (Keycloak, Okta, Auth0,
Laravel Passport) or you want agents governed there. With Dex or Entra
ID, an administrator registers each client.

For an agent on your own machine, `cartograph mcp ./vault` speaks MCP
over stdio as the operator, held to the same rules.

## Reports

Reporting is optional and replaceable (ADR 0014). `CARTOGRAPH_REPORTS`
chooses it: `computed` (the default) works each report out when asked,
the same on every store, a vault included; `postgres` answers from
views in a Postgres store's own database, for SQL and BI tools; `off`
leaves reporting out, for a deployment with a warehouse of its own,
which can follow the event log. The four standard reports:

| Report | One row per |
|---|---|
| `projects` | project: its team, state, version, and the goals it serves |
| `kpi-readings` | reading in force: its KPI, period, value, whether provisional, and who recorded it when |
| `alignment` | manifest that references a goal |
| `teams` | team, with every team above it |

```
GET /api/v1/reports/kpi-readings?format=csv
cartograph report projects ./vault -format csv
```

With `CARTOGRAPH_REPORTS=postgres` each report is a view,
`report_projects`, `report_kpi_readings`, `report_alignment` and
`report_teams`, created when the replicas start; a test holds every view
to the computed rows. The documents can be queried too, read-only:

```sql
-- Goals at the outcome level, through the GIN index on doc
SELECT id, title FROM manifests
WHERE kind = 'Goal' AND doc @> '{"spec": {"level": "outcome"}}';
```

Write only through Cartograph. A row written around it skips
validation, the reference index, the series and the history.

Recording one reading is `POST /api/v1/manifests/KPIReadings/{id}/series/readings`
with the reading and a reason: it costs the same however long the
series is, and recording a period again restates it.

### Upgrading from 2.2

The new tables are added while the replicas run. The new release keeps
writing the text the earlier one reads, and triggers in the database
keep `manifests` and `events` right whichever release wrote, so a
rolling upgrade needs no stop. In the background, at start and then
every minute, a new replica gives a document to every manifest an
earlier one stored as YAML (the log says `repaired documents`) and
records the series items the rows lack (`recorded series items`).
Compaction starts on versions older than `CARTOGRAPH_COMPACT_AFTER`,
24 hours by default, and a 2.2 replica cannot read a compacted version,
so finish the rollout within that time.

## Large-scale, highly available

The stateless deployment above scales out. This section says how to run
it for many people, with no single replica that matters.

### The topology

```
clients ── load balancer ── replica 1 ─┐
           (no affinity)    replica 2 ─┼── PgBouncer ── Postgres primary (+ standbys)
                            replica N ─┘        └──── direct: one LISTEN per replica
```

- N stateless replicas, all with the same `CARTOGRAPH_STORE`, behind any
  load balancer. There are no sticky sessions. Any replica serves any
  request, and a sync socket that moves to another replica resumes from
  heads.
- Postgres is the one stateful part, and its high availability is the
  operator's. Use a managed service, or an operator such as
  CloudNativePG, with a standby and automatic failover. Cartograph
  reconnects after a failover; a change refused meanwhile stays with
  the client, which sends it again.
- Through PgBouncer in transaction mode, two things change. LISTEN does
  not work through such a pooler, so set `CARTOGRAPH_FANOUT_URL` to a
  direct Postgres URL; only the one listening connection per replica
  uses it. And pgx prepares statements, which transaction pooling
  breaks, so add `default_query_exec_mode=exec` to the store URL, or
  turn on PgBouncer's prepared statement support
  (`max_prepared_statements`, PgBouncer 1.21 or later).

`compose.ha.yaml` rehearses this on one machine: Postgres, a one-shot
import of the example, two replicas, and nginx in front (`just image`,
then `just ha-up`, and `just ha-down` to stop). The Helm chart in
`deploy/helm/cartograph` deploys it on Kubernetes.

The settings this shape tunes (`CARTOGRAPH_STORE`,
`CARTOGRAPH_FANOUT_URL`, `CARTOGRAPH_METRICS_ADDR`,
`CARTOGRAPH_SYNC_PING`, `CARTOGRAPH_DOC_CACHE`,
`CARTOGRAPH_DRAIN_DELAY`) are in the configuration table above.

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
- nginx without active health checks (the open-source build) finds a
  stopped replica only by connecting to it, and waits up to 60 seconds
  (`proxy_connect_timeout`) before trying the next. Lower it to a second
  or two on a local network; `compose.ha.yaml` uses `1s`. On Kubernetes,
  readiness takes a draining pod out of the Service before it stops.
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
  or stops. It deliberately does not check the database either. If it
  did, a short database blip would mark every replica unready at once,
  and the load balancer would turn a partial outage into a total one.
  While the database is away, replicas still answer, refuse writes they
  cannot store, and recover on their own.
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

- KEDA on Prometheus. Scale on open sync connections, which is what
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

- A HorizontalPodAutoscaler on CPU. It is simpler, and right when most
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
