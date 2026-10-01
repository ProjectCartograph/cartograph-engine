// Package yamlfmt writes YAML the way the vaults are written by hand.
//
// yaml.v3 indents four spaces by default. Every manifest in this repository
// is written two, so saving one through Cartograph reindented the whole file and
// turned a one-line edit into a whole-file diff — the same complaint that
// moved autosave into a staging directory, arriving one step later at the
// save itself.
//
// So there is one encoder, and everything that writes a manifest or a
// vault.yaml goes through it.
package yamlfmt

import (
	"bytes"

	"gopkg.in/yaml.v3"
)

// Indent is the indentation every file Cartograph writes uses.
const Indent = 2

// Marshal encodes v as YAML at Cartograph's indentation.
func Marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(Indent)
	if err := enc.Encode(v); err != nil {
		enc.Close()
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
