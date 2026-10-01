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
| The Go packages | `pkg/client`, `pkg/merge`, `pkg/uiconformance` | An exported identifier removed or its signature changed; a scenario's expectation tightened | An identifier or scenario added | Internals |
| The command line | `cartograph` subcommands, flags, `CARTOGRAPH_*` settings | A subcommand, flag or setting removed or its meaning changed; a default changed | One added | Help text |
| The vault layout | `<Kind>/<id>.<ext>`, `vault.yaml`, `.cartograph/` | A vault written by one version not opening in a later one | New files a later version writes and an earlier one ignores | The index (always rebuildable) |
| The images and binaries | `cartograph:<version>`, `cartograph-linux-{amd64,arm64}` | A runtime user, port, volume or entrypoint changed | | |

A breaking change ships as a new major with a new contract version
(`/api/v2`, `cartograph/v2`) served beside the old one for at least one
minor release, and a new Go module path (`/v2`), as Go requires.

## How it is enforced

- `just compat` compares the contract and the schemas against the last
  tag and fails on a breaking change unless `VERSION` has a higher
  major. CI runs it on every pull request.
- `just compat` also runs `gorelease` against the last tag for `pkg/`.
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
`UI_VERSION` names the interface build it embeds. A UI release is
compatible with every engine release of the same major.
