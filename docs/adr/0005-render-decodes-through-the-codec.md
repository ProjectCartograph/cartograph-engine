# 0005. Documents decode through the engine's codec

**Status:** Accepted

## Context

The `codec.Codec` port exists so that nothing in the engine, or beside
it, parses a manifest syntax itself. A deployment selects the syntax
with `CARTOGRAPH_CODEC`, and the store keeps the text that codec
produced.

`internal/render`, which builds HTML charters from engine data, read
each version's text with `yaml.Unmarshal`. That worked under the JSON
codec only by accident (JSON is valid YAML). It would break under any
codec whose text is not YAML. It also put a syntax library into a
package whose job is presentation.

## Decision

`render` decodes every manifest through `engine.Codec()`, the codec the
composition root gave the engine. It no longer imports a YAML library,
and `internal/arch` forbids it from doing so again. The number
formatter now handles every numeric type a codec may hand back (`int`,
`int64`, `float64`), not only those the YAML library produces.

## Options considered

**Leave it, since JSON parses as YAML.** That is true for the two
codecs that ship, but it is the kind of accident the port exists to
prevent, and a third codec would hit it at run time.

**Give render its own codec parameter.** That is explicit, but every
call site would have to thread the same codec the engine already
holds, and a mismatch between the two would be possible.

**Use the engine's codec (chosen).** One codec per engine, read from
the engine that render already takes.

## Consequences

- A new codec works for documents with no change to `render`.
- `render` depends on the engine and nothing else in the module, which
  is what `ARCHITECTURE.md` section 3.2 always said it did.
