package contract_test

import (
	"encoding/json"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/contract"
)

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
