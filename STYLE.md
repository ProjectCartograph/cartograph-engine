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

`CONTRIBUTING.md` ("Commit messages") is the full rule, with an
example; this is the summary.

**[Google's Angular commit message format](https://github.com/angular/angular/blob/main/contributing-docs/commit-message-guidelines.md)**,
checked by `just commit-check` (`scripts/check-commit-msg`) in CI on
every pull request.

- **Header**: `<type>(<scope>): <summary>`, at most 72 characters. The
  type is Angular's (`build`, `ci`, `docs`, `feat`, `fix`, `perf`,
  `refactor`, `test`); the scope is one of this repository's areas, or
  none. The summary is imperative, lower-case, with no period:
  "feat(fanout): add a NATS adapter", "fix(kinds): refuse a goal under
  the wrong level". Not "fixed bug", not "WIP".
- **A blank line.**
- **The body**, for every type but `docs`: what the change does and
  why, for the reader who was not there. What the problem was, why
  this approach, what was considered and rejected, what a reviewer
  should look at. Say what you ran and what it printed when it
  matters.
- **The footer**: `BREAKING CHANGE:` or `DEPRECATED:` with what to do
  instead, when the change breaks or deprecates something. Then
  `Fixes #12` on its own line, where an issue exists.
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
