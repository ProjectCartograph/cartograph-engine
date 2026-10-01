package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func runSnapshot(args []string) error {
	positional, flagArgs := splitPositional(args, 3)

	fs := flag.NewFlagSet("snapshot", flag.ExitOnError)
	vaultDir := fs.String("db", "", "vault directory (default current directory)")
	reason := fs.String("reason", "", "reason for this snapshot")
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	if len(positional) < 2 || *reason == "" {
		return fmt.Errorf(`usage: cartograph snapshot <kind> <id> [<vault-dir>] --reason "..."`)
	}

	kind, id := positional[0], positional[1]

	// Determine vault directory (can be specified as third positional arg or via -db flag, defaults to .)
	vault := *vaultDir
	if vault == "" && len(positional) > 2 {
		vault = positional[2]
	}
	if vault == "" {
		vault = "."
	}

	ctx := context.Background()
	e, closeDB, err := openEngine(ctx, vault, false, envCodec())
	if err != nil {
		return err
	}
	defer closeDB()

	// Read the current manifest file
	filePath := filepath.Join(vault, kind, id+".yaml")
	yamlBytes, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read manifest file: %w", err)
	}

	// Create snapshot (calls Commit with reason and operator)
	v, err := e.Snapshot(ctx, kind, id, yamlBytes, *reason, "local")
	if err != nil {
		return err
	}

	// Write last-snapshot.txt
	lastSnapshotPath := filepath.Join(vault, ".cartograph", "last-snapshot.txt")
	content := fmt.Sprintf("%s %s v%d: %s\n", kind, id, v.Number, *reason)
	if err := os.WriteFile(lastSnapshotPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("write last-snapshot.txt: %w", err)
	}

	fmt.Printf("%s/%s v%d snapshot created: %s\n", kind, id, v.Number, *reason)
	return nil
}
