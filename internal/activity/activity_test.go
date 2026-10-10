package activity

import (
	"strings"
	"testing"
)

func TestAnInterfaceActHoldsShapesOnly(t *testing.T) {
	t.Parallel()
	ok := []Event{
		{Name: StepEnter, Session: "w-1a2b", Surface: "goals/$id", Kind: "Goal", Record: "g-7f3a", Step: "aim"},
		{Name: FieldSet, Kind: "Goal", Field: "/spec/keyResults/{kr-1}/target"},
		{Name: Press, Surface: "projects", Target: "none", Millis: 140},
		{Name: Request, Outcome: Failed, Millis: 1800, Sign: true},
		{Name: PickerClose, Field: "/spec/parent", Target: "chosen"},
	}
	for _, e := range ok {
		if _, err := NewInterfaceAct(e); err != nil {
			t.Errorf("%+v: %v", e, err)
		}
	}
	// Each of these could carry what a person wrote, or is not an act an
	// interface sees.
	bad := []Event{
		{Name: "typed"},
		{Name: VersionSave},
		{Name: StepEnter, Step: "Feed the town"},
		{Name: FieldSet, Field: "/spec/objective=Feed the town"},
		{Name: FieldSet, Field: "/spec/keyResults/{deliveries graded A}"},
		{Name: StepEnter, Surface: "goals/Feed the town"},
		{Name: StepEnter, Record: strings.Repeat("x", 81)},
		{Name: Press, Target: "Save as version"},
		{Name: Request, Outcome: "slow"},
		{Name: Press, Millis: -1},
	}
	for _, e := range bad {
		if _, err := NewInterfaceAct(e); err == nil {
			t.Errorf("%+v: taken, want refused", e)
		}
	}
}

type kept []Event

func (k *kept) Record(e Event) { *k = append(*k, e) }

func TestStampedNamesTheBuild(t *testing.T) {
	t.Parallel()
	var k kept
	Stamped(&k, "2.11.0").Record(Event{Name: Press})
	if len(k) != 1 || k[0].Build != "2.11.0" {
		t.Fatalf("got %+v", k)
	}
	if !IsOff(nil) || !IsOff(Off{}) || IsOff(&k) {
		t.Fatal("IsOff")
	}
}
