package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/internal/render"
)

func runRender(args []string) error {
	positional, flagArgs := splitPositional(args, 3)

	fs := flag.NewFlagSet("render", flag.ExitOnError)
	db := fs.String("db", "", "vault directory (default current directory)")
	outDir := fs.String("out", ".", "output directory for rendered charter")
	format := fs.String("format", "html", "output format: html, pdf, json, or all")
	working := fs.Bool("working", false, "use working copy instead of latest snapshot")
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	if len(positional) < 2 || positional[0] != "charter" {
		return fmt.Errorf(`usage: cartograph render charter <project id> [<vault-dir>] [--out <dir>] [--format html|pdf|json] [--working]`)
	}

	projectID := positional[1]

	// Determine vault directory (can be specified as third positional arg or via -db flag, defaults to .)
	vault := *db
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

	// Determine which snapshot to use
	snapshot := 0
	if !*working {
		// Get latest snapshot
		versions, err := e.Versions(ctx, "Project", projectID)
		if err != nil {
			return fmt.Errorf("get versions: %w", err)
		}
		if len(versions) == 0 {
			return fmt.Errorf("no snapshot yet; use --working")
		}
		snapshot = versions[len(versions)-1].Number
	}

	// Render the charter
	htmlBytes, jsonBytes, err := render.Charter(ctx, e, projectID, snapshot)
	if err != nil {
		return fmt.Errorf("render charter: %w", err)
	}

	// Create output directory if it doesn't exist
	if err := os.MkdirAll(*outDir, 0755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	// Generate file name (no timestamp, deterministic)
	var baseFilename string
	if snapshot == 0 {
		baseFilename = fmt.Sprintf("charter-%s-v0", projectID)
	} else {
		baseFilename = fmt.Sprintf("charter-%s-v%d", projectID, snapshot)
	}

	// Determine what formats to write
	wantHTML := strings.Contains(*format, "html") || *format == "all"
	wantJSON := strings.Contains(*format, "json") || *format == "all"
	wantPDF := strings.Contains(*format, "pdf") || *format == "all"

	// Write HTML and JSON first (both may be needed for PDF or standalone)
	var htmlPath string
	if wantHTML || wantPDF {
		htmlPath = filepath.Join(*outDir, baseFilename+".html")
		if err := os.WriteFile(htmlPath, htmlBytes, 0644); err != nil {
			return fmt.Errorf("write html: %w", err)
		}
		if wantHTML {
			fmt.Printf("Wrote: %s\n", htmlPath)
		}
	}

	if wantJSON {
		jsonPath := filepath.Join(*outDir, baseFilename+".json")
		if err := os.WriteFile(jsonPath, jsonBytes, 0644); err != nil {
			return fmt.Errorf("write json: %w", err)
		}
		fmt.Printf("Wrote: %s\n", jsonPath)
	}

	if wantPDF {
		pdf, err := newPrinter(ctx, e, "").Print(ctx, htmlBytes)
		if err != nil {
			return fmt.Errorf("render pdf: %w", err)
		}
		pdfPath := filepath.Join(*outDir, baseFilename+".pdf")
		if err := os.WriteFile(pdfPath, pdf, 0o644); err != nil {
			return fmt.Errorf("write pdf: %w", err)
		}
		fmt.Printf("Wrote: %s\n", pdfPath)
	}

	return nil
}
