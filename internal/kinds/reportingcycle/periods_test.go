package reportingcycle

import (
	"reflect"
	"testing"
)

func ends(ps []Period) []string {
	var out []string
	for _, p := range ps {
		out = append(out, p.End+" "+p.Start+" "+p.Label)
	}
	return out
}

func TestARegularCycleLinesUpWithItsYear(t *testing.T) {
	spec := map[string]any{"periodMonths": 3, "startMonth": 4, "dueOffsetDays": 10}
	got, err := Between(spec, "2026-05", "2026-10")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"2026-06 2026-04 ", "2026-09 2026-07 ", "2026-12 2026-10 "}
	if !reflect.DeepEqual(ends(got), want) {
		t.Fatalf("got %q, want %q", ends(got), want)
	}
	if got[0].Due != "2026-07-10" {
		t.Fatalf("due %s, want 2026-07-10", got[0].Due)
	}
}

// Terms of 4, 4 and 3 months, which no regular cycle can hold.
func TestTermsRepeatEachYearByTheMonthEachEnds(t *testing.T) {
	spec := map[string]any{"periods": []any{
		map[string]any{"name": "Term I", "endMonth": 12},
		map[string]any{"name": "Term II", "endMonth": 4},
		map[string]any{"name": "Term III", "endMonth": 7},
	}}
	got, err := Between(spec, "2026-09", "2027-12")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"2026-12 2026-08 Term I 2026/27",
		"2027-04 2027-01 Term II 2026/27",
		"2027-07 2027-05 Term III 2026/27",
		"2027-12 2027-08 Term I 2027/28",
	}
	if !reflect.DeepEqual(ends(got), want) {
		t.Fatalf("got %q, want %q", ends(got), want)
	}
	if !Ends(spec, "2027-04") || Ends(spec, "2027-03") {
		t.Fatal("Ends does not follow the term ends")
	}
}

func TestSurveyWavesAreDatedOnce(t *testing.T) {
	spec := map[string]any{"periods": []any{
		map[string]any{"name": "Follow-up", "end": "2027-12"},
		map[string]any{"name": "Baseline", "end": "2026-12"},
	}}
	got, err := Between(spec, "2026-01", "2030-12")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"2026-12 2026-12 Baseline", "2027-12 2027-01 Follow-up"}
	if !reflect.DeepEqual(ends(got), want) {
		t.Fatalf("got %q, want %q", ends(got), want)
	}
	if !Ends(spec, "2027-12") || Ends(spec, "2028-12") {
		t.Fatal("Ends does not follow the waves")
	}
}

func TestACalendarYearIsLabelledByOneYear(t *testing.T) {
	spec := map[string]any{"periods": []any{
		map[string]any{"name": "First half", "endMonth": 6},
		map[string]any{"name": "Second half", "endMonth": 12},
	}}
	got, _ := Between(spec, "2026-01", "2026-12")
	if len(got) != 2 || got[0].Label != "First half 2026" || got[1].Start != "2026-07" {
		t.Fatalf("got %+v", got)
	}
}

func TestARangeIsBounded(t *testing.T) {
	if _, err := Between(map[string]any{"periodMonths": 1, "startMonth": 1}, "1900-01", "2900-01"); err == nil {
		t.Fatal("a thousand-year range was walked")
	}
	if _, err := Between(map[string]any{}, "2026-13", "2027-01"); err == nil {
		t.Fatal("month 13 was accepted")
	}
}
