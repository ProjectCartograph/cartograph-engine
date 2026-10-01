// Package codec is the port between a manifest's text and its document.
// The engine validates and reasons about documents (the generic map a
// schema validates, or a typed view of one); what text those documents
// are written in is an adapter's business. YAML is the one the vaults are
// written in; the json adapter beside it proves the seam and gives a
// database-backed deployment a text its own tools read.
//
// A store carries the text the codec produced, byte for byte, so
// versions and diffs are over what the person wrote. Changing the codec
// of an existing vault is therefore a migration (export, re-encode,
// import), not a flag.
package codec

// Document is one manifest out of a text that may hold several: the
// decoded document and the exact bytes it was written as, so what is
// committed is the text the person wrote.
type Document struct {
	Doc  map[string]any
	Text []byte
}

// Codec turns manifest text into documents and back.
type Codec interface {
	// Name is the codec's short name, "yaml" or "json".
	Name() string
	// Extension is the file extension a vault uses for this codec's
	// files, with the dot: ".yaml".
	Extension() string
	// Decode parses text into the generic document a schema validates.
	Decode(text []byte) (map[string]any, error)
	// DecodeAll parses a text that may hold several manifests (YAML's
	// "---"), each with its own bytes; a syntax with no such notion
	// returns one. An empty document is skipped, not reported.
	DecodeAll(text []byte) ([]Document, error)
	// DecodeInto parses text into a typed value. Field names follow the
	// value's `yaml` tags, which every typed view in the engine carries;
	// an adapter for another syntax maps its names onto those.
	DecodeInto(text []byte, v any) error
	// Encode writes a document, or a typed value, as canonical text: the
	// form a vault file is saved in, stable across runs.
	Encode(v any) ([]byte, error)
}
