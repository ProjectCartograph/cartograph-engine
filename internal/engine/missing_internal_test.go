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

// A pattern failure is said without the validator's expression
// (engine#96: a new project's empty team read as a regular expression).
func TestAPatternFailureIsSaidInWords(t *testing.T) {
	for message, want := range map[string]string{
		"'' does not match pattern '^[a-z0-9][a-z0-9-]{1,63}$'":        "Needed before this can be merged.",
		"'Depot A' does not match pattern '^[a-z0-9][a-z0-9-]{1,63}$'": "Lower-case letters, digits and hyphens, starting with a letter or digit.",
		"'x' does not match pattern '^[A-Z]{3}$'":                      "This is not in the form asked; check how it is written.",
		"value must be one of 'a', 'b'":                                "Choose one of the values offered.",
	} {
		if got := plainProblem(message); got != want {
			t.Errorf("plainProblem(%q) = %q, want %q", message, got, want)
		}
	}
}
