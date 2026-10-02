package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/api"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/reporting"
)

// runReport is `cartograph report`: a report over a vault or a store, as
// CSV for a spreadsheet or JSON, the same the API serves.
func runReport(args []string) error {
	positional, flagArgs := splitPositional(args, 2)
	fs := flag.NewFlagSet("report", flag.ExitOnError)
	format := fs.String("format", "csv", "csv or json")
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	if len(positional) != 2 {
		return fmt.Errorf("usage: cartograph report <%s> <vault, file or postgres:// URL> [-format csv|json]", strings.Join(reporting.Standard, "|"))
	}
	ctx := context.Background()
	reports := os.Getenv("CARTOGRAPH_REPORTS")
	if reports == "off" {
		return fmt.Errorf("reporting is off (CARTOGRAPH_REPORTS=off)")
	}
	c, err := compose(ctx, storeOptions{Target: positional[1], Codec: envCodec(), Fanout: "memory", Reports: reports})
	if err != nil {
		return err
	}
	defer c.Close()
	t, err := c.Reports.Run(ctx, positional[0])
	if err != nil {
		return err
	}
	switch *format {
	case "csv":
		b, err := api.ReportCSV(t)
		if err != nil {
			return err
		}
		_, err = os.Stdout.Write(b)
		return err
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{"name": t.Name, "columns": t.Columns, "rows": t.Rows})
	}
	return fmt.Errorf("format %q: want csv or json", *format)
}
