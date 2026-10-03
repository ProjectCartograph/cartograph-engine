# Architecture decision records

One decision per file, numbered in the order they were made. A record
says what forced the decision, what was chosen, what else was weighed,
and what the choice costs. `ARCHITECTURE.md` describes the system as it
stands; these records explain how it got that way.

A record is never edited to change its decision. A later record
supersedes it, and the earlier one gets a status line pointing forward.

| Record | Decision | Status |
|---|---|---|
| [0001](0001-record-architecture-decisions.md) | Record architecture decisions | Accepted |
| [0002](0002-embed-a-pinned-interface-release.md) | Embed a pinned, checksummed interface release | Accepted |
| [0003](0003-every-package-has-a-dependency-rule.md) | Every package has a dependency rule | Accepted |
| [0004](0004-the-root-chooses-the-vault-index.md) | The composition root chooses the vault's index | Accepted |
| [0005](0005-render-decodes-through-the-codec.md) | Documents decode through the engine's codec | Accepted |
| [0006](0006-interfaces-reach-the-engine-through-a-client-port.md) | The web interface reaches the engine through a client port | Accepted |
| [0007](0007-automerge-is-the-one-crdt.md) | Automerge is the one CRDT | Accepted |
| [0008](0008-postgres-and-a-fan-out-port.md) | Postgres holds the state; fan-out is a port | Accepted |
| [0009](0009-operating-at-scale.md) | Operating at scale: probes, shutdown, metrics, and a chart | Accepted |
| [0010](0010-prefer-the-standard-library.md) | Prefer the standard library; every dependency earns its place | Accepted |
| [0011](0011-access-by-role-and-team.md) | Access by role and team, enrolled from the directory | Accepted |
| [0012](0012-postgres-keeps-manifests-as-documents.md) | Postgres keeps manifests as documents; store ports ask for sets | Accepted |
| [0013](0013-copy-on-write-or-merge-on-read.md) | Each stored thing is copy-on-write or merge-on-read, on every backend | Accepted |
| [0014](0014-reporting-is-a-port-not-the-core.md) | Reporting is a port, not the core | Accepted |
| [0015](0015-only-attended-windows-hold-connections.md) | Only an attended window holds a connection | Accepted |
| [0016](0016-agents-read-and-propose-people-decide.md) | Agents read and propose; people decide | Accepted |
| [0017](0017-one-bar-for-people-and-agents.md) | One bar for people and agents, guided by the contract | Accepted |
| [0018](0018-following-agents.md) | Following agents, each person their own | Accepted |
| [0019](0019-said-once-as-the-taxonomy-says.md) | Said once, as the taxonomy says | Accepted |
| [0020](0020-the-purpose-is-the-top-of-the-strategy.md) | The purpose is the top of the strategy | Accepted |
| [0021](0021-evidence-from-several-sources.md) | Evidence from several sources | Accepted |

## Writing one

Copy the shape of an existing record. Use the next free number and a
file name that states the decision. Write it in the same change as the
code it describes, so a reviewer reads the reason beside the diff.
Name roles, never people. Keep it to what a reader needs in a year:
the forces, the choice, the options rejected and why, the costs.

A decision needs a record when it changes a port, a ring, or how the
binary is built, composed or shipped. A bug fix, a new kind or a new
adapter for an existing port does not; `EXTENDING.md` covers those.
