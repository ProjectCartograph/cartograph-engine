// Package yaml is the YAML codec: yaml.v3 to read, yamlfmt to write, so a
// file saved by Cartograph keeps the two-space indentation the vaults are
// written in by hand.
package yaml

import (
	"bytes"
	"errors"
	"io"

	yamlv3 "go.yaml.in/yaml/v3"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/codec"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/yamlfmt"
)

// Codec is the YAML codec. The zero value is ready to use.
type Codec struct{}

var _ codec.Codec = Codec{}

// New returns the YAML codec.
func New() Codec { return Codec{} }

func (Codec) Name() string      { return "yaml" }
func (Codec) Extension() string { return ".yaml" }

func (Codec) Decode(text []byte) (map[string]any, error) {
	var doc map[string]any
	if err := yamlv3.Unmarshal(text, &doc); err != nil {
		return nil, err
	}
	return doc, nil
}

func (Codec) DecodeInto(text []byte, v any) error { return yamlv3.Unmarshal(text, v) }

func (Codec) Encode(v any) ([]byte, error) { return yamlfmt.Marshal(v) }

// DecodeAll splits a file on "---". A file holding exactly one manifest
// keeps its original bytes, so what is committed is the text that was
// written; only a file that holds several is re-marshalled per
// document, since there is no other way to give each its own text. An
// empty document (a stray "---", trailing comments) is skipped: it
// names no kind and is not an attempt at one.
func (Codec) DecodeAll(text []byte) ([]codec.Document, error) {
	dec := yamlv3.NewDecoder(bytes.NewReader(text))
	var nodes []yamlv3.Node
	for {
		var node yamlv3.Node
		err := dec.Decode(&node)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	var out []codec.Document
	for i := range nodes {
		var doc map[string]any
		if err := nodes[i].Decode(&doc); err != nil || doc == nil {
			continue
		}
		t := text
		if len(nodes) > 1 {
			b, err := yamlfmt.Marshal(&nodes[i])
			if err != nil {
				return nil, err
			}
			t = b
		}
		out = append(out, codec.Document{Doc: doc, Text: t})
	}
	return out, nil
}
