package trace_test

import (
	"testing"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/trace"
)

// Every call is an opportunity and every refusal a defect; a refused step
// tried again is rework; the yields multiply over the write steps.
func TestAnalyseReadsCallsAsAProcess(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	c := func(tool, record, outcome string) trace.Call {
		at = at.Add(time.Second)
		return trace.Call{At: at, Tool: tool, Record: record, Outcome: outcome, Session: "s1", Seconds: 0.01}
	}
	calls := []trace.Call{
		c("start_work", "", trace.Refused),
		c("start_work", "", trace.OK),
		c("next", "", trace.OK),
		c("next", "", trace.OK),
		c("settle", "Project/p1", trace.OK),
		c("settle", "Project/p2", trace.Refused),
		c("settle", "Project/p2", trace.OK),
		c("propose", "", trace.OK),
	}
	r := trace.Analyse(calls)
	if r.Calls != 8 || r.Defects != 2 || r.DPMO != 250000 {
		t.Fatalf("defects: %+v", r)
	}
	if r.Rework != 2 || r.MeanAttempts != 2 {
		t.Errorf("rework %d, attempts %v", r.Rework, r.MeanAttempts)
	}
	// start_work 0 of 1 first time, settle 1 of 2, propose 1 of 1.
	if r.RolledThroughputYield != 0 {
		t.Errorf("rolled throughput yield %v", r.RolledThroughputYield)
	}
	if r.Value[trace.Waste] != 3 || r.Value[trace.ValueAdding] != 4 {
		t.Errorf("value stream %v", r.Value)
	}
	if r.Sessions[0].Proposed != true || r.Transitions[0].Count < 1 {
		t.Errorf("sessions %+v", r.Sessions)
	}
	if r.Sigma < 1.5 || r.Sigma > 2.5 {
		t.Errorf("sigma %v for a quarter defective", r.Sigma)
	}
}
