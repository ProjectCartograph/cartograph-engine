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
	for name, j := range judgements {
		f, err := os.Open(filepath.Join("testdata", "decide", name+".jsonl"))
		if err != nil {
			t.Errorf("%s: no examples: %v", name, err)
			continue
		}
		right, of := 0, 0
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			var ex struct{ Text, Answer string }
			if err := json.Unmarshal(sc.Bytes(), &ex); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			got, ok := e.judge(ctx, name, ex.Text)
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
		_ = f.Close()
		t.Logf("%s: %d of %d (kept at %d of %d)", name, right, of, j.Measured.Right, j.Measured.Of)
		if right < j.Measured.Right {
			t.Errorf("%s scores %d of %d, below the %d it was kept at", name, right, of, j.Measured.Right)
		}
	}
}
