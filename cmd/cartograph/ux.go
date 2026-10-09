package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/activity"
	activityjsonl "github.com/ProjectCartograph/cartograph-engine/v2/internal/activity/jsonl"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/config"
)

// runUX analyses the people's trace (docs/adr/0034) as
// docs/EVALUATING_PEOPLE.md reads it: tasks rebuilt from acts, defects per
// million opportunities from the flows, yields, the eight wastes, lead
// times, work in progress, and the fields most refused.
func runUX(args []string) error {
	fs := flag.NewFlagSet("ux", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print the report as JSON")
	idle := fs.Duration("idle", time.Hour, "how long a task may go untouched before the trace ends and still be open, not given up")
	person := fs.String("person", "", "only this person's acts, so one person is measured apart from another")
	from := fs.String("from", "", "only acts from this time on (RFC 3339, or a date)")
	to := fs.String("to", "", "only acts before this time (RFC 3339, or a date)")
	period := fs.String("period", "", "take one reading per period of each figure: day, week or build")
	chart := fs.Bool("chart", false, "with -period, chart each figure as an individuals and moving range (XmR) chart")
	split := fs.String("split", "", "with -period, work out the limits from this period or build on: a changed interface is a new process")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return fmt.Errorf("usage: cartograph ux [-json] [-idle 1h] [-person subject] [-from t] [-to t] [-period day|week|build [-chart] [-split period]] <trace file>... | <postgres URL> (where CARTOGRAPH_UI_TRACE keeps it)")
	}
	q := activity.Query{Person: *person}
	var err error
	if q.From, err = whenOf(*from); err != nil {
		return err
	}
	if q.To, err = whenOf(*to); err != nil {
		return err
	}
	acts, err := readActs(context.Background(), fs.Args(), q)
	if err != nil {
		return err
	}
	flows, err := activity.Flows()
	if err != nil {
		return err
	}
	if *period != "" {
		if *period != activity.Day && *period != activity.Week && *period != activity.Build {
			return fmt.Errorf("-period is day, week or build, not %q", *period)
		}
		series := activity.Readings(acts, flows, activity.Options{Idle: *idle}, *period, *split)
		if *asJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(series)
		}
		printReadings(os.Stdout, series, *chart)
		return nil
	}
	r := activity.Analyse(acts, flows, activity.Options{Idle: *idle})
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(r)
	}
	printUX(os.Stdout, r)
	return nil
}

// readActs reads the acts q asks for through the trace's reader port, so
// the analysis is the same wherever the trace is kept.
func readActs(ctx context.Context, sources []string, q activity.Query) ([]activity.Event, error) {
	rd, closeReader, err := traceReader(ctx, sources)
	if err != nil {
		return nil, err
	}
	defer closeReader()
	return rd.Read(ctx, q)
}

// traceReader is the reader the sources name: one postgres:// URL, or
// the files a jsonl trace was appended to.
func traceReader(ctx context.Context, sources []string) (activity.Reader, func(), error) {
	for _, s := range sources {
		if config.IsPostgresURL(s) {
			if len(sources) > 1 {
				return nil, nil, errors.New("read one Postgres trace, or files, not both")
			}
			store, closeStore, err := activityPostgres(ctx, s)
			if err != nil {
				return nil, nil, err
			}
			return store, closeStore, nil
		}
	}
	return activityjsonl.NewReader(sources...), func() {}, nil
}

func printUX(w io.Writer, r activity.Report) {
	fmt.Fprintf(w, "Acts %d, %s to %s\n", r.Acts, r.From.Format(time.RFC3339), r.To.Format(time.RFC3339))
	fmt.Fprintf(w, "Defects %d in %d opportunities: %.0f DPMO, sigma %.2f\n", r.Defects, r.Opportunities, r.DPMO, r.Sigma)
	fmt.Fprintf(w, "First pass yield %.1f%%, completion %.1f%%\n", r.FirstPassYield*100, r.Completion*100)
	fmt.Fprintf(w, "Value stream: %d value-adding, %d necessary, %d waste (value added %.1f%%)\n\n",
		r.Value[activity.ValueAdding], r.Value[activity.Necessary], r.Value[activity.Waste], r.ValueAddedRatio*100)
	fmt.Fprintf(w, "%-7s %-18s %7s %8s %7s %5s %6s %5s %5s %9s %6s %8s %8s %6s\n",
		"task", "kind", "started", "finished", "givenUp", "open", "done", "FPY", "RTY", "DPMO", "sigma", "p50 s", "p95 s", "extra")
	for _, t := range r.Tasks {
		extra := "-"
		if t.ShortestPath > 0 {
			extra = fmt.Sprintf("%.2fx", t.Extra)
		}
		fmt.Fprintf(w, "%-7s %-18s %7d %8d %7d %5d %5.0f%% %4.0f%% %4.0f%% %9.0f %6.2f %8.1f %8.1f %6s\n",
			t.Type, t.Kind, t.Started, t.Finished, t.GivenUp, t.Open, t.Completion*100, t.FirstPassYield*100,
			t.RolledThroughputYield*100, t.DPMO, t.Sigma, t.MedianSeconds, t.P95Seconds, extra)
	}
	fmt.Fprintf(w, "\nWaste:\n")
	for _, c := range r.Waste {
		fmt.Fprintf(w, "  %-18s %-32s %5d of %-6d %10.3f %s\n", c.Waste, c.Name, c.Count, c.Base, c.Rate, c.Per)
	}
	fmt.Fprintf(w, "\nLead times:\n")
	for _, l := range r.LeadTimes {
		fmt.Fprintf(w, "  %-46s %4d  median %.0f s, p95 %.0f s\n", l.Name, l.Count, l.Median, l.P95)
	}
	fmt.Fprintf(w, "\nWork in progress:\n")
	for _, s := range r.Inventory {
		fmt.Fprintf(w, "  %-18s %4d  median age %.0f s\n", s.Name, s.Count, s.MedianAgeSecs)
	}
	fmt.Fprintf(w, "\nSteps' first pass yield:\n")
	for _, s := range r.Steps {
		fmt.Fprintf(w, "  %-18s %-20s %3d of %-3d %5.0f%%\n", s.Kind, s.Step, s.Passed, s.Tasks, s.Yield*100)
	}
	fmt.Fprintf(w, "\nFields most refused:\n")
	for i, c := range r.Pareto {
		if i == 10 {
			break
		}
		fmt.Fprintf(w, "  %-18s %-20s %-40s %d\n", c.Kind, c.Step, c.Field, c.Count)
	}
	fmt.Fprintf(w, "\nNot measured yet: ")
	for i, n := range r.NotMeasured {
		if i > 0 {
			fmt.Fprint(w, "; ")
		}
		fmt.Fprint(w, n)
	}
	fmt.Fprintln(w)
}

// whenOf reads a -from or -to time: RFC 3339, or a date; empty is open.
func whenOf(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return t, fmt.Errorf("%q is neither RFC 3339 nor a date", s)
	}
	return t, nil
}

// printReadings prints each figure's readings, and with chart, its
// centre, natural process limits and the signals that the process moved.
func printReadings(w io.Writer, series []activity.Series, chart bool) {
	for i, s := range series {
		if i > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "%s\n", s.Name)
		for j, p := range s.Chart.Points {
			mark := ""
			if s.Split != "" && p.Period == s.Split {
				mark = "  <- limits from here"
			}
			if chart && len(p.Signals) > 0 {
				mark += "  signal: " + strings.Join(p.Signals, ", ")
			}
			fmt.Fprintf(w, "  %-12s %12.3f  (%d)%s\n", p.Period, p.Value, s.Counts[j], mark)
		}
		if !chart {
			continue
		}
		c := s.Chart
		if len(c.Points) >= 2 && c.Note != "Two readings at least are needed for limits." {
			fmt.Fprintf(w, "  centre %.3f, limits %.3f to %.3f, stable %v\n", c.Centre, c.Lower, c.Upper, c.Stable)
		}
		if c.Note != "" {
			fmt.Fprintf(w, "  %s\n", c.Note)
		}
	}
}
