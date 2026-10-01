package main

import (
	"context"
	"fmt"
	"os"

	"github.com/ProjectCartograph/cartograph-engine/internal/codec"
	codecjson "github.com/ProjectCartograph/cartograph-engine/internal/codec/json"
	codecyaml "github.com/ProjectCartograph/cartograph-engine/internal/codec/yaml"
	"github.com/ProjectCartograph/cartograph-engine/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/internal/store/sqlite"
	"github.com/ProjectCartograph/cartograph-engine/internal/store/vault"
)

// codecFor picks the manifest syntax by name; "" and "yaml" are YAML.
func codecFor(name string) (codec.Codec, error) {
	switch name {
	case "", "yaml":
		return codecyaml.New(), nil
	case "json":
		return codecjson.New(), nil
	}
	return nil, fmt.Errorf("codec %q: want yaml or json", name)
}

// envCodec is the codec a one-shot command uses: CARTOGRAPH_CODEC, else YAML.
func envCodec() string { return os.Getenv("CARTOGRAPH_CODEC") }

// openEngine is the one place a command composes an engine from adapters.
// A directory opens as a vault (files are the truth, a SQLite index beside
// them); a file opens as a SQLite database. Either way the caller gets an
// engine and a close function and never sees which adapter it got: a
// vault brings its apply gate and bundle store with it, and the engine
// picks those up itself.
func openEngine(ctx context.Context, dbOrVaultPath string, watch bool, codecName string) (*engine.Engine, func() error, error) {
	c, err := codecFor(codecName)
	if err != nil {
		return nil, nil, err
	}
	info, statErr := os.Stat(dbOrVaultPath)
	isDir := statErr == nil && info.IsDir()

	if isDir {
		v, err := vault.New(ctx, dbOrVaultPath, vault.Options{Watch: watch, Extension: c.Extension(), OpenIndex: sqlite.OpenVaultIndexIn})
		if err != nil {
			return nil, nil, fmt.Errorf("open vault: %w", err)
		}
		e, err := engine.New(v, v.Index().Operational(), engine.WithCodec(c))
		if err != nil {
			v.Close()
			return nil, nil, fmt.Errorf("build engine: %w", err)
		}
		// The reference index is rebuilt whenever the state is loaded,
		// not only on writes: files edited while nothing was running
		// carry references the index has never seen.
		if err := e.Reindex(ctx); err != nil {
			v.Close()
			return nil, nil, fmt.Errorf("reindex: %w", err)
		}
		return e, v.Close, nil
	}

	db, err := sqlite.Open(ctx, dbOrVaultPath)
	if err != nil {
		return nil, nil, fmt.Errorf("open database: %w", err)
	}
	e, err := engine.New(sqlite.NewManifestStore(db), sqlite.NewOperationalStore(db), engine.WithCodec(c))
	if err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("build engine: %w", err)
	}
	return e, db.Close, nil
}
