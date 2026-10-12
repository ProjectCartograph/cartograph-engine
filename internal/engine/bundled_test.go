package engine_test

import (
	"encoding/json"
	"strings"
	"testing"
)

// A kind's schema is served whole: the shared definitions it uses carried
// under $defs and every reference pointing into them, so an interface
// reads a Day as a date without knowing common.schema.json exists.
func TestASchemaIsServedWithItsDefinitions(t *testing.T) {
	t.Parallel()
	e := newTestEngine(t)
	for _, k := range e.Kinds() {
		raw, err := e.SchemaJSON(k.Kind)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "common.schema.json#") {
			t.Errorf("%s still points into common.schema.json", k.Kind)
		}
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("%s: %v", k.Kind, err)
		}
		defs, _ := doc["$defs"].(map[string]any)
		var walk func(any)
		walk = func(n any) {
			switch v := n.(type) {
			case map[string]any:
				if ref, _ := v["$ref"].(string); strings.HasPrefix(ref, "#/$defs/") && defs[strings.TrimPrefix(ref, "#/$defs/")] == nil {
					t.Errorf("%s: %s names no definition it carries", k.Kind, ref)
				}
				for _, c := range v {
					walk(c)
				}
			case []any:
				for _, c := range v {
					walk(c)
				}
			}
		}
		walk(doc)
	}
	raw, _ := e.SchemaJSON("ReportingCycle")
	var doc struct {
		Defs map[string]struct {
			Format string `json:"format"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil || doc.Defs["Day"].Format != "date" {
		t.Fatalf("a cycle's first day is not served as a date: %v", err)
	}
}
