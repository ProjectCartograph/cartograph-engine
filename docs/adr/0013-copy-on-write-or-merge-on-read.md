# 0013. Each stored thing is copy-on-write or merge-on-read, on every backend

**Status:** Accepted

## Context

Cartograph stores several things whose reads and writes differ by
orders of magnitude: definitions read on every page and saved now and
then, histories saved on every change and read when someone asks,
readings appended monthly for years, drafts saved on every keystroke.
ADR 0012 gave Postgres a document model; this record names the one
strategy each thing uses and why, so an adapter knows what it must
make cheap.

**Copy-on-write (CoW):** a write produces the whole new state, and a
read takes it as stored. Right when reads outnumber writes and a
reader needs the whole thing.

**Merge-on-read (MoR):** a write appends only what changed, and a read
combines what was appended. Right when writes outnumber reads, or when
the whole is rarely wanted and keeping it whole each time would cost
more than combining it on the rare read.

Every function works on every backend: the vault, memory and Postgres.
A backend may be slower at one, never without it. An agent through MCP
(next release) sees the same functions whatever the store.

## Decision

| What | Strategy | Why |
|---|---|---|
| A manifest's current definition | CoW | Read by every page, rule, check and report; saved a few times a day. A save validates the whole document anyway, so writing it whole costs nothing extra, and every read is one lookup. |
| The current-state row (Postgres `manifests`), the reference index | CoW | Derived at the save that changes them, so lists, search and "who references X" never derive them at read time (the cost ADR 0012 measured). |
| A manifest's earlier versions | MoR | Read only for history and diffs. Kept as a keyframe every 16 versions plus a patch per version, merged on read from at most 15 patches. Storage grows with what changed, not with size times versions. |
| A series, such as a KPI's readings | CoW for the current series, MoR for its items | The current series is read by charts and rules, so the latest version holds it whole. Each item is also an append-only row recording what was recorded, when and by whom. The value of a period at any time is merged from those rows, which is what history, restatement and reporting need, at O(1) per reading. Both are written in one transaction. |
| Working copies (autosave) | CoW | One per manifest, replaced whole: a draft is small and read whole by the editor. |
| Shared drafts (Automerge) | MoR | Every keystroke appends a change; a load merges the snapshot and the changes since, and compaction folds them back (ADR 0007). |
| Project state | MoR | An append-only history whose latest row is the state, found by index. The history is the audit; there is nothing to copy. |
| The access list | CoW | One row per person, replaced at a grant or a sign-in. Read on every request. |
| Events | MoR | Every save, state change and series item appends one event, in the same transaction. A consumer (fan-out, an agent's subscription, an export) reads from its cursor. |
| Reports | MoR, outside the core | Computed when asked, by whichever reporting adapter the deployment chose (ADR 0014), from the same set reads and series items; or answered by a warehouse, or not at all. |

### On every backend

| Function | Vault | Memory | Postgres |
|---|---|---|---|
| Definitions, CoW | the file, rewritten through the journal | maps | `manifests` and the latest version |
| History, MoR | full text in the index (a vault is one team's, and git keeps its history) | full | keyframes and patches |
| Series items | the vault's index (SQLite) | slices | `series_items` |
| Events | the vault's index, written by triggers, so a file edited by hand is an event too | a slice | `events`, written by triggers, so a replica of the previous release writes them too |
| Reports (ADR 0014) | computed | computed | computed, or views in the same database |
| Append to a series | yes | yes | yes |

A vault keeps full history because its history lives in git and a
vault is one team's; patches there would save little and complicate
the one place a person may read the index by hand.

### One static binary

Every adapter is pure Go: SQLite through modernc, Postgres through
pgx, Automerge on wazero, and `CGO_ENABLED=0` in every build. Nothing
here adds a C dependency, and the MCP adapter will not.

## Consequences

- An adapter's job is stated per row of the table: make the CoW reads
  one lookup and the MoR writes one append.
- Reports are not the core: a port of their own (ADR 0014), so a
  deployment chooses them, a warehouse, or none.
- Series items and events add tables to the vault's index. The index
  is still a cache: rebuilt from files, it starts their history again,
  as it already does for versions.
