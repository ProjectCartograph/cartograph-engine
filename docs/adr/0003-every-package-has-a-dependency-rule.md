# 0003. Every package has a dependency rule

**Status:** Accepted

## Context

`internal/arch` holds the dependency rule as a test: for each package
it names, it lists what that package must never depend on,
transitively. The test passed, yet two packages broke the rule it
exists to keep:

- `internal/render` parsed manifest text with the YAML library itself,
  going around the codec port (see 0005).
- `internal/store/vault` imported the SQLite adapter to open its own
  index, so one driven adapter composed another (see 0004).

Neither was caught, for three reasons in the test itself:

1. It only checked packages it named. Seventeen of the module's
   forty-one packages had no rule, `internal/render` among them.
2. A forbidden name matched only that exact package. Forbidding
   `internal/api` did not forbid `internal/api/gen`, and listing each
   adapter by name meant a new adapter was allowed by default.
3. It skipped every standard library path, so `net/http`,
   `database/sql` and `os/exec` could never be forbidden, although the
   ring table calls them drivers.

## Decision

- Every package in the module must have a rule. A second test,
  `TestEveryPackageHasARule`, fails on a package with none. The
  composition root and the test package have empty rules, stated
  explicitly. A key ending in `/` gives a default rule to the packages
  under it, which is how each kind's package is covered.
- A forbidden module name covers that package and everything under it.
  A name ending in `/` covers only what is under it, which lets the
  engine depend on the `internal/store` port while every
  `internal/store/...` adapter stays forbidden.
- Standard library drivers can be forbidden by exact name.
- Rules are built from named groups (`drivers`, `outer`, `adapters`,
  and `adapter(own)` for a driven adapter's siblings), so the rings in
  `ARCHITECTURE.md` section 3.1 read directly off the code.

## Options considered

**Keep the deny-list and add the missing entries by hand.** This is
the smallest change, but it fails the same way the next time a package
is added without a rule.

**An allow-list per package.** This is the strictest option: a package
may depend only on what its rule names. But every new import, including
standard library ones, would need a rule change, and most of those
changes carry no architectural meaning. That noise would teach people
to edit the rule without thinking.

**Deny-lists, with every package required to have one (chosen).** The
rule stays about rings, not about individual imports. Coverage is
total, so a new package forces someone to say which ring it belongs to.

## Consequences

- Adding a package means adding its rule in the same change. The
  failure message names the file and the section of `ARCHITECTURE.md`
  to read.
- The test caught both breaches above once they were put back
  temporarily, and passes without them.
- Some allowances are deliberate and written down next to the rule.
  The identity ports are HTTP middleware, so `internal/auth` may use
  `net/http`, and `pkg/client/inproc` reaches `net/http` through them.
  Splitting the identity port from its middleware would remove that
  allowance. It has not been worth a breaking change yet.
- Test files are not checked. A test may compose adapters, as the
  composition root does.
