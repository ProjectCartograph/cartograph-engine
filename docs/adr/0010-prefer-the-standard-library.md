# 0010. Prefer the standard library; every dependency earns its place

**Status:** Accepted

## Context

Each third-party module is code that runs with the engine's privileges.
Each one is a supply chain to watch and something to keep current, and
it adds to the binary and the build. An audit found two that did not
earn their place:

- an archived YAML library (`gopkg.in/yaml.v3`, last released 2022);
- a UUID library used once.

The metrics (ADR 0009) raised the same question: the Prometheus client
library brings eight modules to write a few lines of text.

## Decision

A dependency is added only when the standard library cannot do the job
safely in a reasonable amount of code. Each one is at its latest release
and is listed here with its reason. Indirect modules stay at the
versions their importers require: raising them past that broke code
generation in the audit. The interface repository follows the same rule
for npm packages, and its `npm audit` must be clean.

| Module | Why it stays |
|---|---|
| `github.com/coder/websocket` | WebSocket framing parses untrusted network input. A small, maintained, dependency-free library is safer than our own parser. |
| `github.com/fxamacker/cbor/v2` | The automerge-repo protocol is CBOR, which the standard library lacks, and the decoder parses untrusted input. This library is built and fuzzed for that. |
| `github.com/tetratelabs/wazero` | Runs the Automerge module in pure Go, with no dependencies (ADR 0007). |
| `github.com/jackc/pgx/v5` | The Postgres driver and pool. `database/sql` has no `LISTEN` and no driver of its own. |
| `modernc.org/sqlite` | SQLite without cgo, for the vault index. |
| `github.com/santhosh-tekuri/jsonschema/v6` | JSON Schema 2020-12 validation, the contract's own language. |
| `github.com/fsnotify/fsnotify` | Portable file watching for the vault. |
| `go.yaml.in/yaml/v3` | YAML, the vault's syntax. It is the maintained successor of the archived library, with the same API; v4 is not yet released. |
| `github.com/oapi-codegen/runtime` | Required by the server generated from the contract. |

Replaced by the standard library: `google/uuid` (`crypto/rand`). Kept
out by it: the Prometheus client, whose text format the engine writes
with `net/http` and `runtime/metrics`, checked with `promtool`.

## Consequences

- The dependency rule (ADR 0003) names the drivers and forbids the
  archived YAML path everywhere.
- A pull request that adds a module says why the standard library will
  not do, and adds a row here.
- Two old modules remain indirectly, and neither reaches code the
  engine runs: `google/uuid` comes through the generated server's
  runtime, and `gopkg.in/yaml.v3` only through the code generator, a
  build-time tool.
