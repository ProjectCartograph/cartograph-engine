package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/api"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/auth"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/auth/proxy"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/auth/roles"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/config"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/fanout"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/printer"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/printer/chromium"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/spa"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/syncserver"
)

// runServe is the composition root: it reads the configuration, picks an
// adapter for every port, and wires them to the one engine. Nothing below
// this function knows which adapter it got.
//
// The process is a twelve-factor one: configuration from the environment
// (flags override it), logs to stdout, one port, a clean stop on SIGTERM,
// and liveness and readiness at /healthz and /readyz for whatever runs it.
func runServe(args []string) error {
	positional, flagArgs := splitPositional(args, 1)

	cfg, err := config.FromEnv(os.Getenv)
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	cfg.Flags(fs)
	db := fs.String("db", "", "deprecated: path to sqlite database (use the positional argument or CARTOGRAPH_VAULT)")
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	switch {
	case len(positional) > 0:
		cfg.Vault = positional[0]
	case *db != "":
		cfg.Vault = *db
	}

	logger := newLogger(cfg)
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// A Postgres store, when configured, replaces the vault.
	target := cfg.Vault
	if cfg.Store != "" {
		target = cfg.Store
	}
	opts := storeOptions{Target: target, Watch: cfg.Watch, Codec: cfg.Codec, Fanout: cfg.Fanout, FanoutURL: cfg.FanoutURL, DocCache: cfg.DocCache}
	if cfg.Authz == "access" {
		d, err := loadDirectory(cfg.AccessFile)
		if err != nil {
			return err
		}
		opts.Access = &d
	}
	comp, err := compose(ctx, opts)
	if err != nil {
		return err
	}
	defer comp.Close()
	e := comp.Engine

	if cfg.ImportDir != "" {
		report, err := e.ImportDir(ctx, cfg.ImportDir, "serve", "startup import")
		if err != nil {
			return err
		}
		if len(report.Problems) > 0 {
			for _, fp := range report.Problems {
				for _, p := range fp.Problems {
					logger.Error("import problem", "file", fp.File, "path", p.Path, "message", p.Message)
				}
			}
			return fmt.Errorf("startup import failed, see problems above")
		}
		logger.Info("imported", "manifests", len(report.Imported), "from", cfg.ImportDir)
	}

	// Sensible defaults, once. A vault that holds no Unit gets the standard
	// set so a KPI's unit picker is not empty on a first run; a vault that
	// has dropped one it does not want keeps it dropped.
	if seeded, err := e.SeedStandardUnits(ctx); err != nil {
		return err
	} else if len(seeded) > 0 {
		logger.Info("seeded standard units", "count", len(seeded))
	}

	// The printer: the environment names a browser, else the vault's own
	// settings, else the adapter resolves one. None at all is fine; a
	// PDF is then refused with a message, and nothing else is affected.
	var pdf printer.Printer = printer.None{}
	browser := cfg.Chromium
	if browser == "" {
		if settings, err := e.GetSettings(ctx); err == nil {
			browser = settings.Chromium
		}
	}
	if path, err := chromium.Resolve(browser); err == nil {
		pdf = chromium.New(path)
		logger.Info("pdf printer", "browser", path)
	} else {
		logger.Warn("no pdf printer", "reason", err)
	}

	// Identity: who a request runs as.
	var authn auth.Authenticator = auth.NoAuthentication{}
	if cfg.Auth == "proxy" {
		authn = proxy.New(cfg.AuthProxyHeader)
	}

	// Policy: what a principal may do. The one enforcement point wraps
	// the whole API, after authentication.
	var authz auth.Authorizer = auth.AllowAll{}
	switch cfg.Authz {
	case "roles":
		authz = roles.Parse(cfg.ReadRoles, cfg.WriteRoles)
	case "access":
		// The engine asks the same policy at every write (docs/adr/0011).
		authz = comp.Authz
	}

	var ready atomic.Bool
	var mtr *metrics
	if cfg.MetricsAddr != "" {
		mtr = newMetrics()
	}
	mux, syncSrv := routes(e, comp.Fanout, authn, authz, pdf, &ready, syncserver.WithPing(cfg.SyncPing))
	mtr.watchShared(e.Shared())
	mtr.watchSync(syncSrv)
	mtr.watchFanout(comp.Counted)

	// Requests run on a context of their own, cancelled only after the
	// shutdown below has given them their grace period. Deriving it from
	// the signal context would cancel every request, sync sockets
	// included, the moment SIGTERM arrives.
	baseCtx, cancelBase := context.WithCancel(context.Background())
	defer cancelBase()
	srv := &http.Server{
		Addr:              cfg.ListenAddr(),
		Handler:           requestLog(logger, mtr, mux),
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return baseCtx },
	}
	var metricsSrv *http.Server
	if mtr != nil {
		mln, err := net.Listen("tcp", cfg.MetricsAddr)
		if err != nil {
			return fmt.Errorf("metrics listener: %w", err)
		}
		metricsSrv = &http.Server{Handler: mtr.handler(), ReadHeaderTimeout: 10 * time.Second}
		go func() { _ = metricsSrv.Serve(mln) }()
		logger.Info("metrics listening", "addr", mln.Addr().String())
	}

	// A transport is a listener. TCP by default; a UNIX socket when the
	// address says so (CARTOGRAPH_ADDR=unix:///run/cartograph.sock), which is how
	// a terminal interface on the same machine, or one forwarded over
	// SSH, reaches the engine without a network port.
	ln, err := listen(cfg.ListenAddr())
	if err != nil {
		return err
	}
	errc := make(chan error, 1)
	go func() {
		attrs := []any{"addr", ln.Addr().String(), "network", ln.Addr().Network()}
		if cfg.Store != "" {
			attrs = append(attrs, "store", config.Redact(cfg.Store))
		} else {
			attrs = append(attrs, "vault", cfg.Vault)
		}
		attrs = append(attrs, "fanout", comp.FanoutAdapter, "codec", cfg.Codec, "auth", cfg.Auth, "authz", cfg.Authz)
		logger.Info("cartograph listening", attrs...)
		ready.Store(true)
		errc <- srv.Serve(ln)
	}()

	select {
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}

	// Stop taking new work, finish what is in flight, then close the
	// stores. /readyz goes 503 first, and keeps serving for the drain
	// delay, so load balancers stop sending work before it is refused.
	ready.Store(false)
	logger.Info("shutting down", "drain", cfg.DrainDelay.String(), "grace", cfg.ShutdownTimeout.String())
	time.Sleep(cfg.DrainDelay)
	// Sync sockets are hijacked connections Shutdown does not track: tell
	// each peer to reconnect, which takes it to a replica that is staying.
	if syncSrv != nil {
		syncSrv.Shutdown()
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	err = srv.Shutdown(shutdownCtx)
	if metricsSrv != nil {
		_ = metricsSrv.Shutdown(shutdownCtx)
	}
	if err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}

// newLogger writes to stdout in the configured format and level. Stdout,
// not a file: the environment decides where logs go.
func newLogger(cfg config.Config) *slog.Logger {
	var level slog.Level
	switch strings.ToLower(cfg.LogLevel) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: level}
	if cfg.LogFormat == "json" {
		return slog.New(slog.NewJSONHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, opts))
}

// requestLog logs one line per request at debug, and at warn for a 5xx,
// and counts every request for the metrics. Health probes are counted
// but not logged: they would be most of the log.
func requestLog(logger *slog.Logger, mtr *metrics, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		took := time.Since(start)
		mtr.observe(r.URL.Path, r.Method, rec.status, took)
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			return
		}
		attrs := []any{"method", r.Method, "path", r.URL.Path, "status", rec.status, "ms", took.Milliseconds()}
		if rec.status >= 500 {
			logger.Warn("request", attrs...)
		} else {
			logger.Debug("request", attrs...)
		}
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Hijack hands the connection to a WebSocket (the sync socket), which
// needs the raw connection; without it every upgrade fails with 501.
func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("%T cannot be hijacked", r.ResponseWriter)
	}
	r.status = http.StatusSwitchingProtocols
	return h.Hijack()
}

// Unwrap lets http.ResponseController reach the writer underneath.
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// listen opens the listener an address names: "unix:///path" for a
// UNIX domain socket (removed first if stale), anything else as TCP.
func listen(addr string) (net.Listener, error) {
	if path, ok := strings.CutPrefix(addr, "unix://"); ok {
		_ = os.Remove(path)
		return net.Listen("unix", path)
	}
	return net.Listen("tcp", addr)
}

// withSync routes a WebSocket upgrade on /sync to the sync server.
func withSync(next, sync http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sync" && strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			sync.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// routes is every route the server answers, behind the same middleware
// for the API and the sync socket. Split out so a test drives exactly the
// stack serve runs.
func routes(e *engine.Engine, fan fanout.Bus, authn auth.Authenticator, authz auth.Authorizer, pdf printer.Printer, ready *atomic.Bool, syncOpts ...syncserver.Option) (*http.ServeMux, *syncserver.Server) {
	mux := http.NewServeMux()
	var inner http.Handler = api.New(e, api.Deps{Printer: pdf, Authorizer: authz})
	var syncSrv *syncserver.Server
	if sh := e.Shared(); sh != nil {
		// The sync socket sits inside the same authentication and
		// authorization as every other request; a WebSocket upgrade on
		// /sync goes to it, everything else to the API.
		syncSrv = syncserver.New(sh, fan, authz, slog.Default(), syncOpts...)
		inner = withSync(inner, syncSrv)
	}
	apiHandler := auth.Middleware(authn, auth.Authorize(authz, inner))
	mux.Handle("/api/v1/", http.StripPrefix("/api/v1", apiHandler))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"ok"}`)
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !ready.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(w, `{"status":"stopping"}`)
			return
		}
		fmt.Fprint(w, `{"status":"ready"}`)
	})
	mux.Handle("/", spa.Handler())
	return mux, syncSrv
}
