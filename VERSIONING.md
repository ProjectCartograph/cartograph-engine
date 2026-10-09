# Versioning

Cartograph follows [Semantic Versioning 2.0.0](https://semver.org/spec/v2.0.0.html).
The first release is `1.0.0`, which means the promise below holds from
the first day: a deployed version is not broken by a later minor or
patch release.

## What the version covers

The public surface is everything a deployment or another repository
can depend on. A change to any of it is classified by the rules below.

| Surface | Where | Breaking (major) | Additive (minor) | Patch |
|---|---|---|---|---|
| The HTTP contract | `contract/openapi.yaml`, served at `/api/v1` | A path, operation, response or field removed or renamed; a request field made required; a type changed | A path, operation, optional field or response added | A description |
| The manifest schemas | `contract/schemas/*.schema.json`, `apiVersion: cartograph/v1` | A property or enum value removed; a property made required; a type narrowed; a kind removed | A kind, property or enum value added | A title or description |
| The flows | `contract/flows/*.flow.json` | A step or field removed or re-keyed | A step or field added | Words |
| The guidance | `contract/guidance/<locale>/*.guidance.json`, served by `GET /guides/{kind}` and the MCP `guide` tool | A field path, link or check id it is keyed by removed or renamed | A language, field, link, check or vocabulary added | Words and examples |
| The checks | the ids of every check, as `ChecksOf` reports them | An id removed or renamed | A check added | Messages |
| The Go packages | `pkg/client`, `pkg/uiconformance`, module `github.com/ProjectCartograph/cartograph-engine/v2` | An exported identifier removed or its signature changed; a scenario's expectation tightened | An identifier or scenario added | Internals |
| The sync socket | `/api/v1/sync`: the automerge-repo network protocol, version 1, over a WebSocket | A move to another protocol version; a message type no longer answered | | |
| The presence payload | `contract/schemas/presence.schema.json` | As for the manifest schemas | As for the manifest schemas | As for the manifest schemas |
| The command line | `cartograph` subcommands, flags, `CARTOGRAPH_*` settings | A subcommand, flag or setting removed or its meaning changed; a default changed | One added | Help text |
| The vault layout | `<Kind>/<id>.<ext>`, `vault.yaml`, `.cartograph/` | A vault written by one version not opening in a later one | New files a later version writes and an earlier one ignores | The index (always rebuildable) |
| The Postgres layout | the tables `store/postgres` migrates on start, and the `report_*` views `reporting/postgres` creates when chosen | A database written by one release not opening in a later one; a replica of the previous minor no longer working against it during a rolling upgrade | Tables, columns and indexes added, with the previous minor still working | |
| The MCP tools | `internal/mcp`: tool and prompt names and their inputs | A tool or an input removed or renamed; a tool allowed to make the record | A tool or optional input added | Descriptions |
| The images and binaries | `cartograph:<version>`, `cartograph-linux-{amd64,arm64}` | A runtime user, port, volume or entrypoint changed | | |

The sync socket speaks a protocol that is automerge-repo's, not
Cartograph's, so the engine promises the version it speaks: moving to
another version of that protocol is a major change, because every
interface's sync adapter would have to move with it.

A breaking change ships as a new major with a new contract version
(`/api/v2`, `cartograph/v2`) served beside the old one for at least one
minor release, and a new Go module path (`/v2`), as Go requires.

## How it is enforced

- `just compat` compares the contract and the schemas against the last
  tag and fails on a breaking change unless `VERSION` has a higher
  major. CI runs it on every pull request.
- `just compat` also compares `pkg/` with the last tag (`scripts/check-api`,
  apidiff) and fails on an incompatible change unless the major is higher.
- `VERSION` is the single source: the flake, the binary (`cartograph
  --version`), the image tag and the release all read it. A release is a
  tag `v<VERSION>` on `main`; `.github/workflows/release.yml` builds the
  binaries for both architectures and the images, and publishes them.
- Pre-releases are `1.1.0-rc.1`; build metadata (`+<sha>`) is stamped
  into the binary by the release workflow and never into `VERSION`.

## Deprecation

A field, operation or flag that will be removed in the next major is
marked `deprecated` in the contract (OpenAPI and JSON Schema both have
the keyword) at least one minor release before, and the release notes
say what replaces it.

## cartograph-ui

The interfaces version independently. `ENGINE_VERSION` in `cartograph-ui`
names the engine contract it was built against; the engine's
`UI_VERSION` names the interface build it embeds, and `UI_SHA256` the
SHA-256 of that release's `dist.tar.gz`, so an engine commit always
embeds the same bytes. A UI release is compatible with every engine
release of the same major.

To embed a new interface release, change both files in one commit:

```
echo vX.Y.Z > UI_VERSION
curl -sSfL https://github.com/ProjectCartograph/cartograph-ui/releases/download/vX.Y.Z/dist.tar.gz | sha256sum | cut -d' ' -f1 > UI_SHA256
just ui && just ci
```
