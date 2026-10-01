# 0006. The web interface reaches the engine through a client port

**Status:** Accepted

## Context

`UI_CONTRACT.md` gives one rule for every interface: it depends on a
port, never on a wire. The Go side has that port in `pkg/client.Client`,
with an in-process and a remote transport, both proven by
`pkg/uiconformance`. The rule also appears in `cartograph-ui`'s own
brief ("No component talks to a wire").

The web interface did not follow it. It built one concrete
`openapi-fetch` client and called it from components, stores and
routes: 66 calls naming `/manifests/...` paths across 27 files, one raw
`fetch`, and six literal `/api/v1` URLs for charters. A component test
had to mock the HTTP client module, so every test knew the wire. A
second transport, or an in-process interface, would have meant editing
every one of those files.

## Decision

- `src/client/port.ts` declares a TypeScript `Client` interface, named
  after `pkg/client.Client` where an operation exists in both. Its
  types come from the generated contract types, so a contract change is
  a type error, not a wrong screen.
- Refusals are typed. `ClientError` carries the status and the
  server's problems. `Refused` (422) mirrors the Go port's `Refused`,
  so a refused version still lands the server's own message on its
  fields, and `Conflict` (409) and `NotFound` (404) keep the paths that
  depended on them.
- `src/client/http.ts` is the only adapter and the only file that names
  a path. A `ClientProvider` hands the client to components, and
  `main.tsx` constructs it once. Tests use `fakeClient`, in which any
  method a test did not supply fails by name.
- `just wire`, part of `just ci`, fails on an API path, a `fetch` call
  or an `openapi-fetch` import outside the adapter.

## Options considered

**Leave it until flows render from the contract.** That work touches
the same files, but it is much larger, and the wire would have spread
further in the meantime.

**Wrap `openapi-fetch` in hooks, one per endpoint.** This is less code
to move, but components would still depend on HTTP-shaped hooks, and
tests would still mock a module. A hook is not a port.

**A port with one adapter, provided by context (chosen).** It is the
same shape as the engine side. A test fake, another transport or a
recording client all plug in at one point.

## Consequences

- No visible behaviour changed. The one difference on the wire is that
  the snapshots page's single apply sends `{refs: [...]}` instead of
  `{ref: ...}`. The contract accepts both, with the same result.
- Component tests no longer know about HTTP. Two tests that only
  exercised their own mocks now drive the real code.
- Rendering flows from the contract, naming every control by its field
  path (`data-cartograph-field`) and porting `pkg/merge` are still to
  do. They build on this port. `UI_CONTRACT.md` section 4 lists them.
