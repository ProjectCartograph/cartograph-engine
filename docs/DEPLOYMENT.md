# Deploying Cartograph

Cartograph is one binary that serves a directory. Everything a deployment
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
just image                   # nix build .#image, loaded into docker as cartograph:<version>
nix build .#image-chromium   # the same plus Chromium, for PDF printing (about 400 MB larger)
```

Releases publish both to the organisation's container registry as
multi-architecture tags (`<version>` and `<version>-chromium`), built
on x86_64 and aarch64 runners from the same flake. The image runs as
uid 65532, owns `/vault`, sets `CARTOGRAPH_VAULT=/vault` and
`CARTOGRAPH_LOG_FORMAT=json`, exposes 8080, and checks itself with `cartograph
ready` (there is no shell and no curl in it). Mount a volume at
`/vault`; that volume is the only state. `compose.yaml` does the same
with the health check.

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

The vault directory is the whole state. `.cartograph/` inside it is a cache.
A backup is a copy of the directory, or a git commit of it. A restore is
putting the directory back and starting the server; the index rebuilds.
Version numbers restart from what the files say, which is why versions
that matter are snapshotted (`cartograph snapshot`) and why a vault under git
loses nothing.

## Several replicas

Two processes can serve the same directory read-mostly; the watcher keeps
them in step. Two processes writing to one directory are not arbitrated
across processes yet. Until the Postgres index exists, run one writer.

## Upgrading

Replace the binary and restart. The contract is versioned (`/api/v1`);
a vault written by an older binary opens in a newer one, and the schemas
are in the binary, so a manifest that no longer validates shows as a
problem on its next save, not as a refusal to start.
