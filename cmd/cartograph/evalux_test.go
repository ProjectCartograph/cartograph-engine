package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/activity"
	activityjsonl "github.com/ProjectCartograph/cartograph-engine/v2/internal/activity/jsonl"
)

// A task run is scored from its people's trace alone, and the streak is
// counted on the build its runs were served from.
func TestATaskRunIsScoredFromItsTrace(t *testing.T) {
	dir := t.TempDir()
	task := filepath.Join(dir, "define-team.json")
	if err := os.WriteFile(task, []byte(`{"name":"define a team","brief":"Add the packing team.","runs":1,
		"bar":{"type":"define","kind":"Team","maxDefects":0,"maxSeconds":600}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	rd := filepath.Join(dir, "tasks", "001")
	if err := os.MkdirAll(rd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(rd, "run.json"), evalRun{Name: "001", Commit: "abc1234"}); err != nil {
		t.Fatal(err)
	}
	rec, err := activityjsonl.Open(filepath.Join(rd, "people.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 10, 9, 13, 0, 0, 0, time.UTC)
	rec.Record(activity.Event{At: t0, Name: activity.FlowOpen, Source: activity.Interface, Session: "w", Kind: "Team", Record: "t1", Target: "new"})
	rec.Record(activity.Event{At: t0.Add(90 * time.Second), Name: activity.VersionSave, Source: activity.Server, Kind: "Team", Record: "t1", Outcome: activity.OK})
	_ = rec.Close()

	if err := evalUXScoreCmd([]string{dir, "001", "-task", task}); err != nil {
		t.Fatal(err)
	}
	var s uxScore
	if err := readJSON(filepath.Join(rd, "score.json"), &s); err != nil {
		t.Fatal(err)
	}
	if !s.Pass || len(s.Checks) != 3 {
		t.Fatalf("score %+v", s)
	}
	if err := evalUXStatusCmd([]string{dir, "-task", task}); err != nil {
		t.Fatal(err)
	}
}
