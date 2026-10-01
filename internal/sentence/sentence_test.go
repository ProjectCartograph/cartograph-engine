package sentence_test

import (
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/sentence"
)

// The interface composes the same sentence from the same parts. Where
// these two disagree, a charter says something the screen does not.
func TestComposition(t *testing.T) {
	groups := []string{"Depot staff", "Retail partners"}
	who := sentence.WhoPhrase(groups)

	if want := "Depot staff and retail partners"; who != want {
		t.Fatalf("who = %q, want %q", who, want)
	}

	got := sentence.Problem(who, "wait a season to learn a delivery failed", "checks happen after dispatch")
	want := "Depot staff and retail partners wait a season to learn a delivery failed, because checks happen after dispatch."
	if got != want {
		t.Fatalf("problem =\n%q\nwant\n%q", got, want)
	}

	got = sentence.Change("every delivery is checked at intake", who, "learn of a failure the same day")
	want = "Every delivery is checked at intake, so depot staff and retail partners learn of a failure the same day."
	if got != want {
		t.Fatalf("change =\n%q\nwant\n%q", got, want)
	}

	got = sentence.Aim("every depot checks produce against one standard", "buyers get one quality")
	want = "Every depot checks produce against one standard, so that buyers get one quality."
	if got != want {
		t.Fatalf("aim =\n%q\nwant\n%q", got, want)
	}
}

// Until the second clause is written the sentence stops at the first,
// which is a smaller claim rather than an unfinished one.
func TestOneClauseIsEnough(t *testing.T) {
	if got, want := sentence.Change("records join on one identifier", "Depot staff", ""), "Records join on one identifier."; got != want {
		t.Fatalf("change = %q, want %q", got, want)
	}
	if got, want := sentence.Aim("Raise produce quality", ""), "Raise produce quality."; got != want {
		t.Fatalf("aim = %q, want %q", got, want)
	}
	if got, want := sentence.Problem("Depot staff", "cannot see a depot falling behind.", ""), "Depot staff cannot see a depot falling behind."; got != want {
		t.Fatalf("problem = %q, want %q", got, want)
	}
}

// Text written before the parts existed carries its own subject. A second
// one would read "Depot staff Depot staff wait a season".
func TestASubjectIsNeverDoubled(t *testing.T) {
	got := sentence.Problem("Depot staff", "Depot staff wait a season", "the data does not join")
	want := "Depot staff wait a season, because the data does not join."
	if got != want {
		t.Fatalf("problem = %q, want %q", got, want)
	}
	got = sentence.Change("checks run at intake", "Depot staff", "depot staff learn the same day")
	want = "Checks run at intake, so depot staff learn the same day."
	if got != want {
		t.Fatalf("change = %q, want %q", got, want)
	}
}

func TestFinishing(t *testing.T) {
	cases := map[string]string{
		"proficiency is checked early": "Proficiency is checked early.",
		"TVET crates are graded":       "TVET crates are graded.",
		"Who owns this?":               "Who owns this?",
		"Already finished.":            "Already finished.",
		"":                             "",
	}
	for in, want := range cases {
		if got := sentence.Finish(in); got != want {
			t.Errorf("Finish(%q) = %q, want %q", in, got, want)
		}
	}
}
