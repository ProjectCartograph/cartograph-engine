// Package json is the JSON codec. It exists for two reasons: to prove the
// codec port with a second syntax, and for a deployment whose manifests
// live in a database or come from tools that speak JSON more readily than
// YAML. Files are written indented, two spaces, keys in the order the
// encoder sorts them, so a save is stable across runs.
package json

import (
	"bytes"
	gojson "encoding/json"

	yamlv3 "gopkg.in/yaml.v3"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/codec"
)

// Codec is the JSON codec. The zero value is ready to use.
type Codec struct{}

var _ codec.Codec = Codec{}

// New returns the JSON codec.
func New() Codec { return Codec{} }

func (Codec) Name() string      { return "json" }
func (Codec) Extension() string { return ".json" }

func (Codec) Decode(text []byte) (map[string]any, error) {
	var doc map[string]any
	if err := gojson.Unmarshal(text, &doc); err != nil {
		return nil, err
	}
	return doc, nil
}

// DecodeInto decodes through the generic document and YAML's field
// mapping, so the engine's typed views (which carry `yaml` tags) read a
// JSON manifest without a second set of tags. JSON is a subset of YAML
// 1.2, so this is a faithful read, at the cost of one extra pass.
func (Codec) DecodeInto(text []byte, v any) error {
	if !gojson.Valid(text) {
		var probe any
		return gojson.Unmarshal(text, &probe)
	}
	return yamlv3.Unmarshal(text, v)
}

func (Codec) Encode(v any) ([]byte, error) {
	// A typed value with yaml tags only would encode under its Go field
	// names; go through YAML's view of it first so both codecs agree on
	// the field names, then write JSON.
	y, err := yamlv3.Marshal(v)
	if err != nil {
		return nil, err
	}
	var generic any
	if err := yamlv3.Unmarshal(y, &generic); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := gojson.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(stringKeys(generic)); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// stringKeys turns yaml.v3's map[string]any (and any nested map[any]any
// an older document shape may produce) into what encoding/json accepts.
func stringKeys(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = stringKeys(val)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if ks, ok := k.(string); ok {
				out[ks] = stringKeys(val)
			}
		}
		return out
	case []any:
		for i := range t {
			t[i] = stringKeys(t[i])
		}
		return t
	default:
		return v
	}
}

// DecodeAll: a JSON text is one manifest.
func (c Codec) DecodeAll(text []byte) ([]codec.Document, error) {
	doc, err := c.Decode(text)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, nil
	}
	return []codec.Document{{Doc: doc, Text: text}}, nil
}
