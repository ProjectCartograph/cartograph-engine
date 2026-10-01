package main

import (
	"context"
	"flag"
	"fmt"
)

func runDiff(args []string) error {
	positional, flagArgs := splitPositional(args, 2)

	fs := flag.NewFlagSet("diff", flag.ExitOnError)
	db := fs.String("db", "cartograph.db", "path to the sqlite database")
	from := fs.Int("from", 0, "version to diff from")
	to := fs.Int("to", 0, "version to diff to")
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	if len(positional) != 2 || *from == 0 || *to == 0 {
		return fmt.Errorf("usage: cartograph diff <kind> <id> --from N --to M -db cartograph.db")
	}
	kind, id := positional[0], positional[1]

	ctx := context.Background()
	e, closeDB, err := openEngine(ctx, *db, false, envCodec())
	if err != nil {
		return err
	}
	defer closeDB()

	changes, err := e.Diff(ctx, kind, id, *from, *to)
	if err != nil {
		return err
	}
	if len(changes) == 0 {
		fmt.Println("no differences")
		return nil
	}
	for _, c := range changes {
		switch c.Op {
		case "add":
			fmt.Printf("+ %s: %v\n", c.Path, c.To)
		case "remove":
			fmt.Printf("- %s: %v\n", c.Path, c.From)
		default:
			fmt.Printf("~ %s: %v -> %v\n", c.Path, c.From, c.To)
		}
	}
	return nil
}
