package engine

import (
	"slices"
	"testing"
)

// Several properties missing at once are each a field to fill, named as
// one missing alone is (cartograph-ui#88: a funding line showed "missing
// properties 'amount', 'currency'").
func TestMissingPropertiesAreEachAField(t *testing.T) {
	for message, want := range map[string][]string{
		"missing property 'name'":                 {"name"},
		"missing properties 'amount', 'currency'": {"amount", "currency"},
		"value must be one of 'a', 'b'":           nil,
	} {
		if got := missingProperties(message); !slices.Equal(got, want) {
			t.Errorf("missingProperties(%q) = %q, want %q", message, got, want)
		}
	}
}
