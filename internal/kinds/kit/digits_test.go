package kit

import "testing"

// A number in a name is not a target; a number used as one is.
func TestAnObjectiveMayNameANumberedThing(t *testing.T) {
	for text, bad := range map[string]bool{
		"Stalls from Grade 1 to Grade 3 sell only graded produce": false,
		"Operators apply the Grade 2 requirements":                false,
		"Raise compliance to 80 percent":                          true,
		"Halve sugary drinks sold in 2 years":                     true,
		"Faults are found at intake rather than by a buyer":       false,
	} {
		if got := ObjectiveDigitProblem(text, "/spec/objective") != nil; got != bad {
			t.Errorf("%q: flagged %v, want %v", text, got, bad)
		}
	}
}
