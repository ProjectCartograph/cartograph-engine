package main

import (
	"context"
	"flag"
	"fmt"
	"os"
)

func runImport(args []string) error {
	positional, flagArgs := splitPositional(args, 1)

	fs := flag.NewFlagSet("import", flag.ExitOnError)
	db := fs.String("db", "cartograph.db", "path to the sqlite database")
	reason := fs.String("reason", "", "reason for this import")
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	if len(positional) != 1 || *reason == "" {
		return fmt.Errorf(`usage: cartograph import <dir> -db cartograph.db --reason "..."`)
	}
	dir := positional[0]

	ctx := context.Background()
	e, closeDB, err := openEngine(ctx, *db, false, envCodec())
	if err != nil {
		return err
	}
	defer closeDB()

	report, err := e.ImportDir(ctx, dir, "local", *reason)
	if err != nil {
		return err
	}
	if len(report.Problems) > 0 {
		for _, fp := range report.Problems {
			for _, p := range fp.Problems {
				fmt.Printf("%s: %s: %s\n", fp.File, p.Path, p.Message)
			}
		}
		os.Exit(1)
	}
	for _, fp := range report.Notes {
		for _, p := range fp.Problems {
			fmt.Printf("%s: note: %s\n", fp.File, p.Message)
		}
	}
	for _, item := range report.Imported {
		fmt.Printf("%s/%s: version %d\n", item.Kind, item.ID, item.Version)
	}
	return nil
}
