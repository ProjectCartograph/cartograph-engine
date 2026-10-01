// Package conformance is the suite every codec.Codec adapter must pass:
// what the engine relies on, written once, run against each syntax.
package conformance

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/codec"
)

// Run exercises c the way the engine does: decode to a generic document,
// decode into a typed view with yaml tags, encode canonically, and round
// trip without loss.
func Run(t *testing.T, c codec.Codec) {
	t.Helper()
	if c.Name() == "" || len(c.Extension()) < 2 || c.Extension()[0] != '.' {
		t.Fatalf("name %q and extension %q must be set, extension with its dot", c.Name(), c.Extension())
	}

	doc := map[string]any{
		"apiVersion": "cartograph/v1",
		"kind":       "Team",
		"metadata":   map[string]any{"id": "t1", "name": "Team One", "labels": map[string]any{"tier": "a"}},
		"spec":       map[string]any{"members": []any{"r1", "r2"}, "size": 2, "active": true},
	}

	text, err := c.Encode(doc)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	again, err := c.Encode(doc)
	if err != nil || !bytes.Equal(text, again) {
		t.Fatalf("encode must be stable across calls: %v", err)
	}

	back, err := c.Decode(text)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !reflect.DeepEqual(normalise(back), normalise(doc)) {
		t.Fatalf("round trip lost something:\n got %#v\nwant %#v", normalise(back), normalise(doc))
	}

	var typed struct {
		Kind     string `yaml:"kind"`
		Metadata struct {
			ID   string `yaml:"id"`
			Name string `yaml:"name"`
		} `yaml:"metadata"`
		Spec struct {
			Members []string `yaml:"members"`
			Size    int      `yaml:"size"`
			Active  bool     `yaml:"active"`
		} `yaml:"spec"`
	}
	if err := c.DecodeInto(text, &typed); err != nil {
		t.Fatalf("decode into typed view: %v", err)
	}
	if typed.Kind != "Team" || typed.Metadata.ID != "t1" || typed.Metadata.Name != "Team One" ||
		len(typed.Spec.Members) != 2 || typed.Spec.Size != 2 || !typed.Spec.Active {
		t.Fatalf("typed view wrong: %+v", typed)
	}

	if _, err := c.Decode([]byte("{{{ not a document")); err == nil {
		t.Fatal("malformed text must fail to decode")
	}
}

// normalise erases the differences a syntax is allowed: integers that
// come back as floats, []any against []string.
func normalise(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = normalise(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i := range t {
			out[i] = normalise(t[i])
		}
		return out
	case int:
		return float64(t)
	case int64:
		return float64(t)
	default:
		return v
	}
}
