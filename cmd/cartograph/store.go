package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/codec"
	codecjson "github.com/ProjectCartograph/cartograph-engine/v2/internal/codec/json"
	codecyaml "github.com/ProjectCartograph/cartograph-engine/v2/internal/codec/yaml"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/config"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/crdt/automerge"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/fanout"
	fanoutmemory "github.com/ProjectCartograph/cartograph-engine/v2/internal/fanout/memory"
	fanoutpostgres "github.com/ProjectCartograph/cartograph-engine/v2/internal/fanout/postgres"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store/postgres"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store/sqlite"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store/vault"
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
		var bus fanout.Bus = fanoutmemory.New()
		if fanoutName == "postgres" {
			if bus, err = fanoutpostgres.New(ctx, pool); err != nil {
				pool.Close()
				return nil, err
			}
		}
		docs := postgres.NewDocStore(pool)
		e, err := engine.New(postgres.NewManifestStore(pool), postgres.NewOperationalStore(pool),
			append(shared(docs, bus), engine.WithCodec(c), engine.WithBundles(postgres.NewBundleStore(pool)))...)
		if err != nil {
			bus.Close()
			pool.Close()
			return nil, fmt.Errorf("build engine: %w", err)
		}
		return &composition{Engine: e, Docs: docs, Fanout: bus, FanoutAdapter: fanoutName, Close: func() error {
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
		bus := fanoutmemory.New()
		e, err := engine.New(v, v.Index().Operational(), append(shared(v.Index().Docs(), bus), engine.WithCodec(c))...)
		if err != nil {
			bus.Close()
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
		return &composition{Engine: e, Docs: v.Index().Docs(), Fanout: bus, FanoutAdapter: fanoutName, Close: func() error {
			bus.Close()
			return v.Close()
		}}, nil
	}

	db, err := sqlite.Open(ctx, o.Target)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	bus := fanoutmemory.New()
	docs := sqlite.NewDocStore(db)
	e, err := engine.New(sqlite.NewManifestStore(db), sqlite.NewOperationalStore(db), append(shared(docs, bus), engine.WithCodec(c))...)
	if err != nil {
		bus.Close()
		db.Close()
		return nil, fmt.Errorf("build engine: %w", err)
	}
	return &composition{Engine: e, Docs: docs, Fanout: bus, FanoutAdapter: fanoutName, Close: func() error {
		bus.Close()
		return db.Close()
	}}, nil
}

// crdtEngine is the one Automerge runtime of the process: compiling the
// module costs a quarter of a second, so it is done once, on first use.
var crdtEngine = sync.OnceValues(func() (*automerge.Engine, error) { return automerge.New(0) })

// shared are the options that turn on shared drafts (docs/adr/0007):
// the Automerge adapter, the document store and the fan-out. A process
// whose CRDT runtime cannot start serves everything but shared drafts,
// and says why.
func shared(docs store.DocStore, bus fanout.Bus) []engine.Option {
	am, err := crdtEngine()
	if err != nil {
		slog.Warn("shared drafts are off: the CRDT runtime did not start", "err", err)
		return nil
	}
	return []engine.Option{engine.WithCRDT(am), engine.WithDocStore(docs), engine.WithFanout(bus)}
}
