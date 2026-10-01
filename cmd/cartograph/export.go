package main

import (
	"context"
	"flag"
	"fmt"
)

func runExport(args []string) error {
	positional, flagArgs := splitPositional(args, 2)

	fs := flag.NewFlagSet("export", flag.ExitOnError)
	db := fs.String("db", "", "deprecated: path to sqlite database (use positional argument instead)")
	as := fs.String("codec", "", "write the export in this syntax (yaml or json); the migration between codecs")
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}

	var outDir, vaultOrDB string
	if len(positional) == 2 {
		outDir = positional[0]
		vaultOrDB = positional[1]
	} else if len(positional) == 1 {
		outDir = positional[0]
		vaultOrDB = *db
		if vaultOrDB == "" {
			vaultOrDB = "cartograph.db"
		}
	} else {
		return fmt.Errorf("usage: cartograph export <out-dir> [<vault-or-db>] [--codec yaml|json]")
	}

	ctx := context.Background()
	e, closeDB, err := openEngine(ctx, vaultOrDB, false, envCodec())
	if err != nil {
		return err
	}
	defer closeDB()

	target := e.Codec()
	if *as != "" {
		if target, err = codecFor(*as); err != nil {
			return err
		}
	}
	if err := e.ExportDirAs(ctx, outDir, target); err != nil {
		return err
	}
	fmt.Println("exported to", outDir)
	return nil
}
