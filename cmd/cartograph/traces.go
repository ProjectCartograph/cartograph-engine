package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/trace"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/trace/jsonl"
)

// runTraces analyses MCP call traces (docs/adr/0028) as Lean Six Sigma
// reads a process: defects per million calls and the sigma level, first
// pass and rolled throughput yield, rework, the value stream, and where
// sessions vary.
func runTraces(args []string) error {
	fs := flag.NewFlagSet("traces", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print the report as JSON")
	agent := fs.String("agent", "", "only the calls of this agent (its client name), so one writer is measured apart from another")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return fmt.Errorf("usage: cartograph traces [-json] [-agent name] <trace file>... (the files CARTOGRAPH_MCP_TRACE appends to)")
	}
	var calls []trace.Call
	for _, path := range fs.Args() {
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		cs, err := jsonl.Read(f)
		_ = f.Close()
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		for _, c := range cs {
			if *agent == "" || c.Agent == *agent {
				calls = append(calls, c)
			}
		}
	}
	sort.SliceStable(calls, func(i, j int) bool { return calls[i].At.Before(calls[j].At) })
	r := trace.Analyse(calls)
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(r)
	}
	printReport(os.Stdout, r)
	return nil
}

func printReport(w io.Writer, r trace.Report) {
	fmt.Fprintf(w, "Calls %d, defects %d: %.0f DPMO, sigma %.2f\n", r.Calls, r.Defects, r.DPMO, r.Sigma)
	fmt.Fprintf(w, "First pass yield %.1f%%, rolled throughput yield %.1f%% over the write steps\n", r.FirstPassYield*100, r.RolledThroughputYield*100)
	fmt.Fprintf(w, "Rework %d calls, %.2f attempts a reworked step\n", r.Rework, r.MeanAttempts)
	fmt.Fprintf(w, "Value stream: %d value-adding, %d necessary, %d waste (value added %.1f%%)\n",
		r.Value[trace.ValueAdding], r.Value[trace.Necessary], r.Value[trace.Waste], r.ValueAddedRatio*100)
	fmt.Fprintf(w, "Sessions %d, calls per session vary by %.0f%% (coefficient of variation)\n\n", len(r.Sessions), r.CallsPerSessionCV*100)
	fmt.Fprintf(w, "%-16s %6s %7s %9s %6s %6s %7s %7s  %s\n", "tool", "calls", "defects", "DPMO", "sigma", "FPY", "p50 ms", "p95 ms", "defects by kind")
	for _, t := range r.Tools {
		var kinds []string
		for k, n := range t.ByDefect {
			kinds = append(kinds, fmt.Sprintf("%s %d", k, n))
		}
		sort.Strings(kinds)
		fmt.Fprintf(w, "%-16s %6d %7d %9.0f %6.2f %5.0f%% %7d %7d  %s\n", t.Tool, t.Calls, t.Defects, t.DPMO, t.Sigma, t.FirstPassYield*100, t.P50Millis, t.P95Millis, strings.Join(kinds, ", "))
	}
	fmt.Fprintf(w, "\nFields most refused:\n")
	type pathCount struct {
		tool, path string
		n          int
	}
	var paths []pathCount
	for _, t := range r.Tools {
		for p, n := range t.ByPath {
			paths = append(paths, pathCount{t.Tool, p, n})
		}
	}
	sort.Slice(paths, func(i, j int) bool {
		if paths[i].n != paths[j].n {
			return paths[i].n > paths[j].n
		}
		return paths[i].tool+paths[i].path < paths[j].tool+paths[j].path
	})
	if len(paths) > 10 {
		paths = paths[:10]
	}
	for _, p := range paths {
		fmt.Fprintf(w, "  %-16s %-48s %d\n", p.tool, p.path, p.n)
	}
	fmt.Fprintf(w, "\nMost taken steps:\n")
	for _, t := range r.Transitions {
		fmt.Fprintf(w, "  %-16s -> %-16s %d\n", t.From, t.To, t.Count)
	}
}
