package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/ProjectCartograph/cartograph-engine/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/internal/render"
)

func runHandoff(args []string) error {
	positional, flagArgs := splitPositional(args, 2)

	fs := flag.NewFlagSet("handoff", flag.ExitOnError)
	db := fs.String("db", "", "path to sqlite database or vault directory (default: current directory)")
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	if len(positional) < 1 {
		return fmt.Errorf(`usage: cartograph handoff <project id> [<vault-dir>]`)
	}

	projectID := positional[0]

	// Determine vault/database path
	vaultPath := *db
	if vaultPath == "" && len(positional) > 1 {
		vaultPath = positional[1]
	}
	if vaultPath == "" {
		vaultPath = "."
	}

	ctx := context.Background()
	e, closeEngine, err := openEngine(ctx, vaultPath, false, envCodec())
	if err != nil {
		return err
	}
	defer closeEngine()

	snapshotNum, err := e.HandoffGate(ctx, projectID)
	if err != nil {
		var ve *engine.ValidationError
		if errors.As(err, &ve) {
			fmt.Fprintf(os.Stderr, "cartograph handoff: refused\n")
			for _, p := range ve.Problems {
				fmt.Fprintf(os.Stderr, "  - %s: %s\n", p.Path, p.Message)
			}
			os.Exit(1)
		}
		return fmt.Errorf("handoff: %w", err)
	}

	htmlBytes, jsonBytes, err := render.Charter(ctx, e, projectID, snapshotNum)
	if err != nil {
		return fmt.Errorf("render charter: %w", err)
	}

	// A PDF is optional: no browser, no PDF, and the handoff still goes.
	var pdfBytes []byte
	if pdf, err := newPrinter(ctx, e, "").Print(ctx, htmlBytes); err == nil {
		pdfBytes = pdf
	} else {
		fmt.Fprintf(os.Stderr, "warning: no pdf: %v\n", err)
	}

	// Call engine Handoff with rendered bytes
	result, err := e.Handoff(ctx, projectID, "cli", engine.HandoffRequest{
		HtmlBytes: htmlBytes,
		JsonBytes: jsonBytes,
		PdfBytes:  pdfBytes,
	})
	if err != nil {
		var ve *engine.ValidationError
		if errors.As(err, &ve) {
			for _, p := range ve.Problems {
				fmt.Fprintf(os.Stderr, "cartograph: %s\n", p.Message)
			}
			os.Exit(1)
		}
		return fmt.Errorf("cartograph: %w", err)
	}

	// Print bundle paths
	fmt.Printf("Wrote: %s\n", result.HtmlPath)
	fmt.Printf("Wrote: %s\n", result.JsonPath)
	if result.PdfPath != "" {
		fmt.Printf("Wrote: %s\n", result.PdfPath)
	}
	return nil
}
