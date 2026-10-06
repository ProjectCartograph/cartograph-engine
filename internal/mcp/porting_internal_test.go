package mcp

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
)

// Every field the porting map names as Kind.field is one the kind's
// schema holds: an agent told to write a field that does not exist
// has to stop and read the schema.
func TestThePortingMapNamesOnlyRealFields(t *testing.T) {
	named := regexp.MustCompile(`\b([A-Z][A-Za-z]+)\.([a-z][A-Za-z]+)`)
	n := 0
	for _, r := range porting {
		for _, m := range named.FindAllStringSubmatch(r.Part+" "+r.Goes+" "+r.How, -1) {
			b, err := os.ReadFile("../../contract/schemas/" + strings.ToLower(m[1]) + ".schema.json")
			if err != nil {
				continue // not a kind
			}
			var s struct {
				Properties struct {
					Spec struct {
						Properties map[string]any `json:"properties"`
					} `json:"spec"`
				} `json:"properties"`
			}
			if err := json.Unmarshal(b, &s); err != nil {
				t.Fatal(err)
			}
			n++
			if _, ok := s.Properties.Spec.Properties[m[2]]; !ok {
				t.Errorf("%q names %s.%s, which the schema does not hold", r.Part, m[1], m[2])
			}
		}
	}
	if n < 10 {
		t.Fatalf("only %d fields named; the pattern is not reading the map", n)
	}
}
