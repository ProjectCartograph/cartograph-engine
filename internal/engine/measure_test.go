//go:build decide

package engine

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/decide/laya"
)

// TestMeasureJudgements asks the decision model every judgement's examples
// whose answer is known (testdata/decide/<name>.jsonl) and holds each to
// the score it was kept at (docs/adr/0030). It needs the pinned model
// (CARTOGRAPH_DECIDE_URL); just decide-measure starts it. Locally only:
// CI runs the fakes.
func TestMeasureJudgements(t *testing.T) {
	url := os.Getenv("CARTOGRAPH_DECIDE_URL")
	if url == "" {
		t.Skip("no model: run just decide-measure")
	}
	e := &Engine{}
	WithDecider(laya.New(url, 30*time.Second))(e)
	ctx := context.Background()
	// The guidance's contrasts, asked as the checks ask them.
	guided := map[string]GuideJudgement{}
	for _, kind := range []string{"Goal", "Gap"} {
		b, ok, err := GuideBundleFor(kind, DefaultLocale)
		if err != nil || !ok {
			t.Fatalf("guidance %s: %v", kind, err)
		}
		for id, j := range b.Judgements {
			guided[id] = j
		}
	}
	for id, c := range contrasts {
		j, ok := guided[id]
		if !ok {
			t.Errorf("%s: measured, but the guidance has no such judgement", id)
			continue
		}
		right, of := measureExamples(t, id, func(text string) (Judged, bool) { return e.contrast(ctx, id, j, text) })
		t.Logf("%s: %d of %d (kept at %d of %d)", id, right, of, c.Measured.Right, c.Measured.Of)
		if right < c.Measured.Right {
			t.Errorf("%s scores %d of %d, below the %d it was kept at", id, right, of, c.Measured.Right)
		}
	}
	for name, j := range judgements {
		right, of := measureExamples(t, name, func(text string) (Judged, bool) { return e.judge(ctx, name, text) })
		t.Logf("%s: %d of %d (kept at %d of %d)", name, right, of, j.Measured.Right, j.Measured.Of)
		if right < j.Measured.Right {
			t.Errorf("%s scores %d of %d, below the %d it was kept at", name, right, of, j.Measured.Right)
		}
	}
}

// measureExamples asks every known example of a question, and counts
// those answered as known: yes (or target) is the judgement holding.
func measureExamples(t *testing.T, name string, ask func(string) (Judged, bool)) (right, of int) {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "decide", name+".jsonl"))
	if err != nil {
		t.Errorf("%s: no examples: %v", name, err)
		return 0, 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var ex struct{ Text, Answer string }
		if err := json.Unmarshal(sc.Bytes(), &ex); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got, ok := ask(ex.Text)
		if !ok {
			t.Fatalf("%s: the model did not answer", name)
		}
		of++
		if got.Holds == (ex.Answer == "yes" || ex.Answer == "target") {
			right++
		} else {
			t.Logf("%s: %.2f, known %s: %s", name, got.Sure, ex.Answer, ex.Text)
		}
	}
	return right, of
}
