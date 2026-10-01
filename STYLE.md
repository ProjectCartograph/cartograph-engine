# Style

Two guides, both Google's, adopted whole; this page is only what they
leave to the project and what the gate enforces.

## Code

**[Google Go Style Guide](https://google.github.io/styleguide/go/)**,
with its [decisions](https://google.github.io/styleguide/go/decisions)
and [best practices](https://google.github.io/styleguide/go/best-practices).
Where that guide and the Go team's
[Effective Go](https://go.dev/doc/effective_go) differ, the Google guide
wins.

Enforced by `just ci`:

- `gofmt` (`just fmt-check`), `go vet`, `staticcheck` (`just lint`).
- The dependency rule, as a test (`just arch`, `internal/arch`):
  dependencies point inward. Entities (the kinds, the contract) import
  nothing of the engine; the engine imports ports, never an adapter or
  a driver; adapters import the engine's ports; `cmd` is the only
  package that knows everything. A package that needs something from
  further out gets it through a port, injected in `cmd`.
- No organisation's words anywhere, no em dash in what a person reads
  (`just words`).
- Nothing tracked that is not the product (`just clean-tree`).

What the guide leaves to us:

- Comments say why. A comment that restates the code is deleted. A
  package comment says what the package is for and what it must not
  know; a port's comment says what every adapter must guarantee.
- Errors carry the operation and the object: `fmt.Errorf("open vault:
  %w", err)`. A refusal a person will read is a `Problem` with a path
  and a sentence, never an error string.
- An adapter has a conformance test or it is not done. A test fake
  returns what the real thing returns.
- No `init()`, no package-level mutable state, no globals for
  configuration: `internal/config` reads the environment once and
  `cmd` injects.
- Names are the discipline's nouns (`docs/TAXONOMY.md`). Identifiers
  are identifiers: enums camelCase, ids hyphenated, never prose.

## Commits

**[Google's CL description guidance](https://google.github.io/eng-practices/review/developer/cl-descriptions.html)**,
checked by `just commit-check` (`scripts/check-commit-msg`) in CI on
every pull request.

- **First line**: a short summary of what the change does, as an
  imperative sentence without a trailing period, under 72 characters.
  "Add a NATS fan-out adapter", "Refuse a goal under the wrong level". Not
  "Fixed bug", not "WIP", not a type prefix (`feat:`, `fix:`): this
  repository does not use Conventional Commits.
- **A blank line.**
- **The body**: what the change does and why, for the reader who was
  not there. What the problem was, why this approach, what was
  considered and rejected, what a reviewer should look at. Reference
  the issue (`Fixes #12`) on its own line at the end. Say what you ran
  and what it printed when it matters.
- **`Signed-off-by`** (`git commit -s`): the Developer Certificate of
  Origin.

A change that cannot be described in one summary line is two changes.

Install the hook once and the check runs on every commit:

```
git config core.hooksPath .githooks
```

## Pull requests

One change per pull request, titled as its first commit's summary.
The description is the commit body. CI runs `just ci` on both
architectures and `just commit-check` on the commits; a pull request
that claims a check it did not run is sent back.
