package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/ProjectCartograph/cartograph-engine/internal/engine"
)

func runCheck(args []string) error {
	positional, flagArgs := splitPositional(args, 2)

	fs := flag.NewFlagSet("check", flag.ExitOnError)
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	if len(positional) < 1 {
		return fmt.Errorf(`usage: cartograph check <vault-dir> [<project id>]`)
	}

	vaultDir := positional[0]
	projectID := ""
	if len(positional) > 1 {
		projectID = positional[1]
	}

	ctx := context.Background()
	e, closeDB, err := openEngine(ctx, vaultDir, false, envCodec())
	if err != nil {
		return err
	}
	defer closeDB()

	blockingCount := 0

	if projectID != "" {
		// Check a single project
		checks, err := e.ProjectChecks(ctx, projectID, false)
		if err != nil {
			return err
		}
		for _, item := range checks.Items {
			if item.State == "block" {
				fmt.Printf("[block] %s: %s\n", item.Section, item.Message)
				blockingCount++
			} else if item.State == "warn" {
				fmt.Printf("[warn]  %s: %s\n", item.Section, item.Message)
			}
		}
	} else {
		// Check all projects
		projects, err := e.List(ctx, "Project", engine.Filter{}, false)
		if err != nil {
			return err
		}
		for _, proj := range projects {
			checks, err := e.ProjectChecks(ctx, proj.ID, false)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error checking %s: %v\n", proj.ID, err)
				continue
			}
			for _, item := range checks.Items {
				if item.State == "block" {
					fmt.Printf("%s [block] %s: %s\n", proj.ID, item.Section, item.Message)
					blockingCount++
				} else if item.State == "warn" {
					fmt.Printf("%s [warn]  %s: %s\n", proj.ID, item.Section, item.Message)
				}
			}
		}
	}

	if blockingCount > 0 {
		return fmt.Errorf("check failed: %d blocking items", blockingCount)
	}
	return nil
}
