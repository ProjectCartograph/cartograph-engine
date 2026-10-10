// Package conformance holds every place a people's trace is kept to the
// same promises: what is recorded is read back whole, in the order it
// happened, and a query keeps to its window and its person.
package conformance

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/activity"
)

// Open gives a fresh, empty trace: what records into it and what reads
// it back, which may be one value.
type Open func(t *testing.T) (activity.Recorder, activity.Reader)

// Run checks the trace Open gives.
func Run(t *testing.T, open Open) {
	t0 := time.Date(2026, 10, 9, 13, 0, 0, 0, time.UTC)
	acts := []activity.Event{
		{At: t0.Add(2 * time.Second), Name: activity.VersionSave, Source: activity.Server, Person: "p", Build: "2.11.0",
			Kind: "Goal", Record: "g1", ChangeSet: "cs1", Outcome: activity.Refused, Problems: 2,
			Paths: []string{"/spec/objective", "/spec/keyResults/-/target"}, Checks: []string{"risks-constrained"}},
		{At: t0, Name: activity.FlowOpen, Source: activity.Interface, Session: "w1", Person: "p", Interface: "web",
			Surface: "goals/$id", Kind: "Goal", Record: "g1", Target: "new"},
		{At: t0.Add(time.Second), Name: activity.Press, Source: activity.Interface, Session: "w1", Person: "p",
			Surface: "goals/$id", Step: "aim", Field: "/spec/objective", Target: "action", Millis: 140, Sign: true},
		{At: t0.Add(3 * time.Second), Name: activity.Press, Source: activity.Interface, Session: "w2", Person: "q", Target: "none"},
	}

	t.Run("reads back whole, in order", func(t *testing.T) {
		rec, rd := open(t)
		for _, a := range acts {
			rec.Record(a)
		}
		got, err := rd.Read(context.Background(), activity.Query{})
		if err != nil {
			t.Fatal(err)
		}
		want := []activity.Event{acts[1], acts[2], acts[0], acts[3]}
		if len(got) != len(want) {
			t.Fatalf("read %d acts, want %d", len(got), len(want))
		}
		for i := range want {
			if !reflect.DeepEqual(norm(got[i]), norm(want[i])) {
				t.Errorf("act %d:\ngot  %+v\nwant %+v", i, got[i], want[i])
			}
		}
	})

	t.Run("keeps to a window and a person", func(t *testing.T) {
		rec, rd := open(t)
		for _, a := range acts {
			rec.Record(a)
		}
		got, err := rd.Read(context.Background(), activity.Query{From: t0.Add(time.Second), To: t0.Add(3 * time.Second), Person: "p"})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 || got[0].Name != activity.Press || got[1].Name != activity.VersionSave {
			t.Fatalf("window and person: %+v", got)
		}
	})

	t.Run("an empty trace reads nothing", func(t *testing.T) {
		_, rd := open(t)
		got, err := rd.Read(context.Background(), activity.Query{})
		if err != nil || len(got) != 0 {
			t.Fatalf("read %+v, %v", got, err)
		}
	})
}

// norm makes times comparable across a round trip through a store.
func norm(e activity.Event) activity.Event {
	e.At = e.At.UTC().Truncate(time.Microsecond)
	return e
}
