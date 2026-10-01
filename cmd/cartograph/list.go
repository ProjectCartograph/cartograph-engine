package main

import (
	"context"
	"flag"
	"fmt"

	"github.com/ProjectCartograph/cartograph-engine/internal/engine"
)

func runList(args []string) error {
	positional, flagArgs := splitPositional(args, 2)

	fs := flag.NewFlagSet("list", flag.ExitOnError)
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	if len(positional) < 1 {
		return fmt.Errorf("usage: cartograph list <vault-dir> [<kind>]")
	}

	vaultDir := positional[0]
	kind := ""
	if len(positional) > 1 {
		kind = positional[1]
	}

	ctx := context.Background()
	e, closeDB, err := openEngine(ctx, vaultDir, false, envCodec())
	if err != nil {
		return err
	}
	defer closeDB()

	if kind == "" {
		// List all kinds with counts
		kinds := e.Kinds()
		for _, k := range kinds {
			fmt.Printf("%s\t%d\n", k.Kind, k.Count)
		}
	} else {
		// List manifests of a specific kind
		filter := engine.Filter{Q: ""}
		summaries, err := e.List(ctx, kind, filter, false)
		if err != nil {
			return err
		}
		if len(summaries) == 0 {
			return nil
		}
		for _, s := range summaries {
			fmt.Printf("%s\t%s\n", s.ID, s.Name)
		}
	}
	return nil
}
