package main

import (
	"context"
	"fmt"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/auth"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/auth/access"
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
	// Counted is Fanout with its publishes counted, for the metrics.
	Counted *countedBus
	// Authz is the access policy when storeOptions asked for access by
	// role and team; nil otherwise.
	Authz auth.Authorizer
	Close func() error
}

// storeOptions say what to open. Target is a vault directory, a SQLite
// file, or a postgres:// URL. Fanout is "memory", "postgres", or empty
// for the default: postgres with a Postgres store, memory otherwise.
type storeOptions struct {
	Target string
	Watch  bool
	Codec  string
	Fanout string
	// FanoutURL, when set, is where the Postgres fan-out listens: a
	// direct connection when Target goes through a transaction pooler.
	FanoutURL string
	// DocCache bounds the shared documents kept in memory; 0 is the
	// engine's default.
	DocCache int
	// Access, when set, is access by role and team (docs/adr/0011): the
	// store's access list, this mapping, and the access policy.
	Access *engine.Directory
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
			var opts []fanoutpostgres.Option
			if o.FanoutURL != "" {
				opts = append(opts, fanoutpostgres.ListenOn(o.FanoutURL))
			}
			if bus, err = fanoutpostgres.New(ctx, pool, opts...); err != nil {
				pool.Close()
				return nil, err
			}
		}
		docs := postgres.NewDocStore(pool)
		counted := &countedBus{Bus: bus}
		accessOpts, authz, bind := accessControl(o, postgres.NewAccessStore(pool))
		e, err := engine.New(postgres.NewManifestStore(pool), postgres.NewOperationalStore(pool),
			append(append(shared(docs, counted, o.DocCache), accessOpts...), engine.WithCodec(c), engine.WithBundles(postgres.NewBundleStore(pool)))...)
		if err != nil {
			bus.Close()
			pool.Close()
			return nil, fmt.Errorf("build engine: %w", err)
		}
		bind(e)
		return &composition{Engine: e, Docs: docs, Fanout: counted, Counted: counted, Authz: authz, FanoutAdapter: fanoutName, Close: func() error {
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
		counted := &countedBus{Bus: bus}
		accessOpts, authz, bind := accessControl(o, v.Index().Access())
		e, err := engine.New(v, v.Index().Operational(), append(append(shared(v.Index().Docs(), counted, o.DocCache), accessOpts...), engine.WithCodec(c))...)
		if err != nil {
			bus.Close()
			v.Close()
			return nil, fmt.Errorf("build engine: %w", err)
		}
		bind(e)
		// The reference index is rebuilt whenever the state is loaded,
		// not only on writes: files edited while nothing was running
		// carry references the index has never seen.
		if err := e.Reindex(ctx); err != nil {
			v.Close()
			return nil, fmt.Errorf("reindex: %w", err)
		}
		return &composition{Engine: e, Docs: v.Index().Docs(), Fanout: counted, Counted: counted, Authz: authz, FanoutAdapter: fanoutName, Close: func() error {
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
	counted := &countedBus{Bus: bus}
	accessOpts, authz, bind := accessControl(o, sqlite.NewAccessStore(db))
	e, err := engine.New(sqlite.NewManifestStore(db), sqlite.NewOperationalStore(db), append(append(shared(docs, counted, o.DocCache), accessOpts...), engine.WithCodec(c))...)
	if err != nil {
		bus.Close()
		db.Close()
		return nil, fmt.Errorf("build engine: %w", err)
	}
	bind(e)
	return &composition{Engine: e, Docs: docs, Fanout: counted, Counted: counted, Authz: authz, FanoutAdapter: fanoutName, Close: func() error {
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
func shared(docs store.DocStore, bus fanout.Bus, cache int) []engine.Option {
	am, err := crdtEngine()
	if err != nil {
		slog.Warn("shared drafts are off: the CRDT runtime did not start", "err", err)
		return nil
	}
	return []engine.Option{engine.WithCRDT(am), engine.WithDocStore(docs), engine.WithFanout(bus), engine.WithDocCache(cache)}
}

// accessControl is access by role and team (docs/adr/0011), when o asks
// for it: the store's access list, the mapping, and the access policy,
// which the HTTP layer and the engine share. The policy reads grants
// from the engine, which does not exist yet, so the caller binds it once
// built.
func accessControl(o storeOptions, list store.AccessStore) ([]engine.Option, auth.Authorizer, func(*engine.Engine)) {
	if o.Access == nil {
		return nil, nil, func(*engine.Engine) {}
	}
	var e *engine.Engine
	policy := access.Policy{Grants: func(ctx context.Context, p auth.Principal) (auth.Grants, error) { return e.Grants(ctx, p) }}
	return []engine.Option{engine.WithAccess(list, *o.Access), engine.WithAuthorizer(policy)}, policy, func(built *engine.Engine) { e = built }
}
