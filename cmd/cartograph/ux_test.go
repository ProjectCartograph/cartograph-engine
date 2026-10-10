package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/activity"
	activityjsonl "github.com/ProjectCartograph/cartograph-engine/v2/internal/activity/jsonl"
)

func TestUXReadsATraceFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "people.jsonl")
	rec, err := activityjsonl.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 10, 9, 13, 0, 0, 0, time.UTC)
	for _, e := range []activity.Event{
		{At: t0, Name: activity.FlowOpen, Source: activity.Interface, Session: "w1", Person: "p", Kind: "Team", Record: "t1", Target: "new"},
		{At: t0.Add(time.Minute), Name: activity.VersionSave, Source: activity.Server, Person: "p", Kind: "Team", Record: "t1", Outcome: activity.OK},
		{At: t0.Add(time.Minute), Name: activity.VersionSave, Source: activity.Server, Person: "q", Kind: "Team", Record: "t2", Outcome: activity.OK},
	} {
		rec.Record(e)
	}
	_ = rec.Close()
	acts, err := readActs(context.Background(), []string{path}, activity.Query{Person: "p"})
	if err != nil || len(acts) != 2 {
		t.Fatalf("read %d acts for p, %v", len(acts), err)
	}
	flows, err := activity.Flows()
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	printUX(&out, activity.Analyse(acts, flows, activity.Options{}))
	if !strings.Contains(out.String(), "define  Team") || !strings.Contains(out.String(), "Not measured yet") {
		t.Fatalf("report:\n%s", out.String())
	}
	out.Reset()
	printReadings(&out, activity.Readings(acts, flows, activity.Options{}, activity.Day, ""), true)
	if !strings.Contains(out.String(), "first pass yield") || !strings.Contains(out.String(), "2026-10-09") {
		t.Fatalf("readings:\n%s", out.String())
	}
}
