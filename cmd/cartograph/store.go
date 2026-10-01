package main

import (
	"context"
	"fmt"
	"os"

	"github.com/ProjectCartograph/cartograph-engine/internal/codec"
	codecjson "github.com/ProjectCartograph/cartograph-engine/internal/codec/json"
	codecyaml "github.com/ProjectCartograph/cartograph-engine/internal/codec/yaml"
	"github.com/ProjectCartograph/cartograph-engine/internal/config"
	"github.com/ProjectCartograph/cartograph-engine/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/internal/fanout"
	fanoutmemory "github.com/ProjectCartograph/cartograph-engine/internal/fanout/memory"
	fanoutpostgres "github.com/ProjectCartograph/cartograph-engine/internal/fanout/postgres"
	"github.com/ProjectCartograph/cartograph-engine/internal/store"
	"github.com/ProjectCartograph/cartograph-engine/internal/store/postgres"
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

// composition is what compose chose for every port a server needs:
// the engine over its stores, and the shared drafts' document store and
// fan-out, which the server wires to the sync endpoint.
type composition struct {
	Engine        *engine.Engine
	Docs          store.DocStore
	Fanout        fanout.Bus
	FanoutAdapter string // "memory" or "postgres", for the log
	Close         func() error
}

// storeOptions say what to open. Target is a vault directory, a SQLite
// file, or a postgres:// URL. Fanout is "memory", "postgres", or empty
// for the default: postgres with a Postgres store, memory otherwise.
type storeOptions struct {
	Target string
	Watch  bool
	Codec  string
	Fanout string
}

// openEngine composes an engine for a one-shot command, which needs no
// fan-out: nobody else is listening to it.
func openEngine(ctx context.Context, target string, watch bool, codecName string) (*engine.Engine, func() error, error) {
	c, err := compose(ctx, storeOptions{Target: target, Watch: watch, Codec: codecName, Fanout: "memory"})
	if err != nil {
		return nil, nil, err
	}
	return c.Engine, c.Close, nil
}

// compose is the one place adapters are chosen. A postgres:// URL opens
// the Postgres adapters, all over one pool: every row is live, so there
// is no apply gate and nothing to reindex. A directory opens as a vault
// (files are the truth, a SQLite index beside them, which also holds the
// shared drafts); a file opens as a SQLite database. Either way the
// caller gets the same ports and never sees which adapters it got: a
// vault brings its apply gate and bundle store with it, and the engine
// picks those up itself.
func compose(ctx context.Context, o storeOptions) (*composition, error) {
	c, err := codecFor(o.Codec)
	if err != nil {
		return nil, err
	}
	fanoutName := o.Fanout
	if fanoutName == "" {
		fanoutName = "memory"
		if config.IsPostgresURL(o.Target) {
			fanoutName = "postgres"
		}
	}
	if fanoutName == "postgres" && !config.IsPostgresURL(o.Target) {
		return nil, fmt.Errorf("fanout postgres needs a Postgres store, not %s", o.Target)
	}
	if fanoutName != "memory" && fanoutName != "postgres" {
		return nil, fmt.Errorf("fanout %q: want memory or postgres", fanoutName)
	}

	if config.IsPostgresURL(o.Target) {
		pool, err := postgres.Open(ctx, o.Target)
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", config.Redact(o.Target), err)
		}
		e, err := engine.New(postgres.NewManifestStore(pool), postgres.NewOperationalStore(pool),
			engine.WithCodec(c), engine.WithBundles(postgres.NewBundleStore(pool)))
		if err != nil {
			pool.Close()
			return nil, fmt.Errorf("build engine: %w", err)
		}
		var bus fanout.Bus = fanoutmemory.New()
		if fanoutName == "postgres" {
			if bus, err = fanoutpostgres.New(ctx, pool); err != nil {
				pool.Close()
				return nil, err
			}
		}
		return &composition{Engine: e, Docs: postgres.NewDocStore(pool), Fanout: bus, FanoutAdapter: fanoutName, Close: func() error {
			err := bus.Close()
			pool.Close()
			return err
		}}, nil
	}

	info, statErr := os.Stat(o.Target)
	if statErr == nil && info.IsDir() {
		v, err := vault.New(ctx, o.Target, vault.Options{Watch: o.Watch, Extension: c.Extension(), OpenIndex: sqlite.OpenVaultIndexIn})
		if err != nil {
			return nil, fmt.Errorf("open vault: %w", err)
		}
		e, err := engine.New(v, v.Index().Operational(), engine.WithCodec(c))
		if err != nil {
			v.Close()
			return nil, fmt.Errorf("build engine: %w", err)
		}
		// The reference index is rebuilt whenever the state is loaded,
		// not only on writes: files edited while nothing was running
		// carry references the index has never seen.
		if err := e.Reindex(ctx); err != nil {
			v.Close()
			return nil, fmt.Errorf("reindex: %w", err)
		}
		bus := fanoutmemory.New()
		return &composition{Engine: e, Docs: v.Index().Docs(), Fanout: bus, FanoutAdapter: fanoutName, Close: func() error {
			bus.Close()
			return v.Close()
		}}, nil
	}

	db, err := sqlite.Open(ctx, o.Target)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	e, err := engine.New(sqlite.NewManifestStore(db), sqlite.NewOperationalStore(db), engine.WithCodec(c))
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("build engine: %w", err)
	}
	bus := fanoutmemory.New()
	return &composition{Engine: e, Docs: sqlite.NewDocStore(db), Fanout: bus, FanoutAdapter: fanoutName, Close: func() error {
		bus.Close()
		return db.Close()
	}}, nil
}
