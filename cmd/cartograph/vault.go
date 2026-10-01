package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/ProjectCartograph/cartograph-engine/internal/engine"
)

// The state commands drive the same engine methods the API does, so what
// the command line can do and what the Snapshots screen can do never
// drift apart. Each opens the vault without the watcher: a one-shot
// command has nothing to watch for.

// runApply includes a manifest file in the live state.
func runApply(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: cartograph apply <Kind/id> <vault-dir>")
	}
	ref, vaultDir := args[0], args[1]

	ctx := context.Background()
	e, closeEngine, err := openEngine(ctx, vaultDir, false, envCodec())
	if err != nil {
		return err
	}
	defer closeEngine()

	if _, err := e.Apply(ctx, []string{ref}); err != nil {
		return stateError("apply", err)
	}
	fmt.Printf("applied %s\n", ref)
	return nil
}

// runExclude removes a manifest from the live state and keeps its file.
func runExclude(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf(`usage: cartograph exclude <Kind/id> <vault-dir> --reason "..."`)
	}
	fs := flag.NewFlagSet("exclude", flag.ExitOnError)
	reason := fs.String("reason", "", "reason for exclusion")
	if err := fs.Parse(args[2:]); err != nil {
		return err
	}
	ref, vaultDir := args[0], args[1]
	kind, id, ok := splitRef(ref)
	if !ok {
		return fmt.Errorf("invalid ref format: %s (expected Kind/id)", ref)
	}

	ctx := context.Background()
	e, closeEngine, err := openEngine(ctx, vaultDir, false, envCodec())
	if err != nil {
		return err
	}
	defer closeEngine()

	if !e.HasState() {
		return stateError("exclude", engine.ErrNoState)
	}
	if err := e.Delete(ctx, kind, id, "local", *reason); err != nil {
		return stateError("exclude", err)
	}
	fmt.Printf("excluded %s\n", ref)
	return nil
}

// runRecover re-includes an excluded manifest.
func runRecover(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf(`usage: cartograph recover <Kind/id> <vault-dir> --reason "..."`)
	}
	fs := flag.NewFlagSet("recover", flag.ExitOnError)
	reason := fs.String("reason", "", "reason for recovery")
	if err := fs.Parse(args[2:]); err != nil {
		return err
	}
	ref, vaultDir := args[0], args[1]

	ctx := context.Background()
	e, closeEngine, err := openEngine(ctx, vaultDir, false, envCodec())
	if err != nil {
		return err
	}
	defer closeEngine()

	if _, err := e.Recover(ctx, ref, "local", *reason); err != nil {
		return stateError("recover", err)
	}
	fmt.Printf("recovered %s\n", ref)
	return nil
}

// runUnapplied lists manifest files the live state does not include.
func runUnapplied(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: cartograph unapplied <vault-dir>")
	}
	ctx := context.Background()
	e, closeEngine, err := openEngine(ctx, args[0], false, envCodec())
	if err != nil {
		return err
	}
	defer closeEngine()

	refs, err := e.ListUnapplied(ctx)
	if err != nil {
		return stateError("unapplied", err)
	}
	if len(refs) == 0 {
		fmt.Println("unapplied manifests: (none)")
		return nil
	}
	for _, r := range refs {
		fmt.Printf("%s/%s", r.Kind, r.ID)
		if r.Name != "" {
			fmt.Printf(" (%s)", r.Name)
		}
		fmt.Println()
	}
	return nil
}

// runExcluded lists excluded manifests.
func runExcluded(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: cartograph excluded <vault-dir>")
	}
	ctx := context.Background()
	e, closeEngine, err := openEngine(ctx, args[0], false, envCodec())
	if err != nil {
		return err
	}
	defer closeEngine()

	exclusions, err := e.ListExcluded(ctx)
	if err != nil {
		return stateError("excluded", err)
	}
	if len(exclusions) == 0 {
		fmt.Println("excluded manifests: (none)")
		return nil
	}
	for _, x := range exclusions {
		fmt.Printf("%s/%s (%s): %s\n", x.Kind, x.ID, x.Name, x.Reason)
	}
	return nil
}

// stateError prints the engine's problems the way the API would return
// them, and names the command.
func stateError(command string, err error) error {
	var ve *engine.ValidationError
	if errors.As(err, &ve) {
		for _, p := range ve.Problems {
			fmt.Fprintf(os.Stderr, "cartograph %s: %s\n", command, p.Message)
		}
		return fmt.Errorf("%s refused", command)
	}
	if errors.Is(err, engine.ErrNoState) {
		return fmt.Errorf("%s only works on a vault directory", command)
	}
	return fmt.Errorf("%s: %w", command, err)
}

func splitRef(ref string) (kind, id string, ok bool) {
	for i := 0; i < len(ref); i++ {
		if ref[i] == '/' {
			kind, id = ref[:i], ref[i+1:]
			return kind, id, kind != "" && id != ""
		}
	}
	return "", "", false
}
