package contract_test

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"reflect"
	"strings"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/contract"
)

// A property whose values live in common.schema.json carries them a second
// time as x-cartograph-enum, so the interface can classify the field without
// resolving the $ref (surfaces/sheet/schema.ts deliberately never does).
// Two copies of the same list is two chances to be wrong, so this asserts
// they are the same list.
func TestCartographEnumAnnotationsMatchTheirDefinitions(t *testing.T) {
	common := readSchema(t, "schemas/common.schema.json")
	defs, _ := common["$defs"].(map[string]any)
	if len(defs) == 0 {
		t.Fatal("common.schema.json has no $defs")
	}

	entries, err := fs.ReadDir(contract.Schemas, "schemas")
	if err != nil {
		t.Fatal(err)
	}

	checked := 0
	for _, e := range entries {
		doc := readSchema(t, "schemas/"+e.Name())
		walk(doc, func(path string, prop map[string]any) {
			annotated, ok := prop["x-cartograph-enum"].([]any)
			if !ok {
				return
			}
			ref, _ := prop["$ref"].(string)
			name := strings.TrimPrefix(ref, "common.schema.json#/$defs/")
			if ref == "" || name == ref {
				t.Errorf("%s%s: x-cartograph-enum without a $ref into common.schema.json's $defs", e.Name(), path)
				return
			}
			def, ok := defs[name].(map[string]any)
			if !ok {
				t.Errorf("%s%s: $ref names %q, which common.schema.json does not define", e.Name(), path, name)
				return
			}
			want, _ := def["enum"].([]any)
			if !reflect.DeepEqual(annotated, want) {
				t.Errorf("%s%s: x-cartograph-enum %v does not match $defs/%s enum %v", e.Name(), path, annotated, name, want)
			}
			checked++
		})
	}

	if checked == 0 {
		t.Fatal("no x-cartograph-enum annotation found; this test would never fail")
	}
}

func readSchema(t *testing.T, name string) map[string]any {
	t.Helper()
	data, err := contract.Schemas.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return doc
}

// walk visits every object in a schema document, so an annotation is found
// wherever in the tree it sits.
func walk(node any, visit func(path string, prop map[string]any)) {
	var rec func(any, string)
	rec = func(n any, path string) {
		switch v := n.(type) {
		case map[string]any:
			visit(path, v)
			for k, child := range v {
				rec(child, path+"/"+k)
			}
		case []any:
			for i, child := range v {
				rec(child, fmt.Sprintf("%s/%d", path, i))
			}
		}
	}
	rec(node, "")
}
