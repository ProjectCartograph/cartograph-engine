package main

import (
	"context"
	"fmt"
	codecyaml "github.com/ProjectCartograph/cartograph-engine/internal/codec/yaml"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/ProjectCartograph/cartograph-engine/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/internal/store/memory"
)

// runValidate checks a single manifest file, or every manifest file under a
// directory (as one set, so members may reference each other), against a
// fresh in-memory store. Nothing is persisted either way.
func runValidate(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: cartograph validate <dir|file>")
	}
	target := args[0]
	info, err := os.Stat(target)
	if err != nil {
		return err
	}

	e, err := engine.New(memory.NewManifestStore(), memory.NewOperationalStore(), engine.WithCodec(codecyaml.New()))
	if err != nil {
		return err
	}
	ctx := context.Background()

	var fileProblems []engine.FileProblems
	if info.IsDir() {
		report, err := e.ImportDir(ctx, target, "validate", "validate only, not persisted")
		if err != nil {
			return err
		}
		for _, fp := range report.Notes {
			for _, p := range fp.Problems {
				fmt.Printf("%s: note: %s\n", fp.File, p.Message)
			}
		}
		fileProblems = report.Problems
	} else {
		raw, err := os.ReadFile(target)
		if err != nil {
			return err
		}
		var doc map[string]any
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			fileProblems = []engine.FileProblems{{File: target, Problems: []engine.Problem{{Message: "invalid yaml: " + err.Error()}}}}
		} else {
			kind, _ := doc["kind"].(string)
			problems, err := e.Validate(ctx, kind, raw)
			if err != nil {
				return err
			}
			if len(problems) > 0 {
				fileProblems = []engine.FileProblems{{File: target, Problems: problems}}
			}
		}
	}

	if len(fileProblems) == 0 {
		fmt.Println("ok")
		return nil
	}
	for _, fp := range fileProblems {
		for _, p := range fp.Problems {
			fmt.Printf("%s: %s: %s\n", fp.File, p.Path, p.Message)
		}
	}
	os.Exit(1)
	return nil
}
