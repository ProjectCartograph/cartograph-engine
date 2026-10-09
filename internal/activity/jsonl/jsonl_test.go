package jsonl_test

import (
	"path/filepath"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/activity"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/activity/conformance"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/activity/jsonl"
)

func TestConformance(t *testing.T) {
	t.Parallel()
	conformance.Run(t, func(t *testing.T) (activity.Recorder, activity.Reader) {
		path := filepath.Join(t.TempDir(), "people.jsonl")
		rec, err := jsonl.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = rec.Close() })
		return rec, jsonl.NewReader(path)
	})
}
