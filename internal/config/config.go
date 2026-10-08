// Package config is the one place the process reads its environment. Every
// setting has an CARTOGRAPH_ variable, a flag that overrides it, and a default
// that works on a laptop, in that order of precedence: flag, environment,
// default. Nothing else in the server calls os.Getenv for configuration.
//
// This is the twelve-factor "config" factor: a deployment differs from
// another by environment alone, and the same binary runs in both.
package config

import (
	"flag"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Config is everything `cartograph serve` can be told from outside.
type Config struct {
	// Addr is the listen address. CARTOGRAPH_ADDR, default ":8080". A bare
	// port ("8080", what PORT-style platforms hand out) is accepted, and
	// "unix:///path/to.sock" listens on a UNIX domain socket instead.
	Addr string

	// Vault is the directory of manifests to serve, or a SQLite file.
	// CARTOGRAPH_VAULT, default ".". A positional argument on the command line
	// wins over both.
	Vault string

	// Store, when set, is a postgres:// (or postgresql://) URL, and the
	// manifests, project state, bundles and shared drafts live in that
	// database instead of a vault: what a stateless deployment of many
	// replicas needs. CARTOGRAPH_STORE, default empty: serve Vault.
	Store string

	// Fanout carries "this document changed" and presence between
	// replicas: "memory" (one replica) or "postgres" (LISTEN/NOTIFY on
	// the Store database). CARTOGRAPH_FANOUT, default empty, which means
	// postgres when Store is set and memory otherwise; FanoutAdapter
	// resolves it.
	Fanout string

	// FanoutURL, when set, is the Postgres URL the fan-out's listening
	// connection uses instead of Store. LISTEN needs a session of its own,
	// so when Store goes through a pooler in transaction mode (PgBouncer's
	// usual setting), this must reach Postgres directly.
	// CARTOGRAPH_FANOUT_URL, default empty: the Store URL.
	FanoutURL string

	// MetricsAddr, when set, serves Prometheus metrics at /metrics on a
	// listener of its own, kept off the address users reach.
	// CARTOGRAPH_METRICS_ADDR, default empty: no metrics listener.
	MetricsAddr string

	// SyncPing is how often the sync socket pings an idle peer, so a load
	// balancer or proxy does not close a quiet connection (nginx and AWS
	// ALB close one after 60 s by default). CARTOGRAPH_SYNC_PING, default
	// 20s; 0 turns pings off.
	SyncPing time.Duration

	// SyncIdle closes a sync connection that has changed no document for
	// this long, so an unattended window holds no replica up and the
	// deployment can scale to zero (docs/adr/0015). Pings and presence
	// do not count. CARTOGRAPH_SYNC_IDLE, default 10m; 0 never closes.
	SyncIdle time.Duration

	// DocCache bounds how many shared documents a replica keeps in
	// memory; the least recently used are dropped first, and a dropped
	// one is loaded again from the store when next needed.
	// CARTOGRAPH_DOC_CACHE, default 1000.
	DocCache int

	// CompactAfter is how old a version must be, beside not being its
	// manifest's latest, before a Postgres store keeps it as a patch
	// instead of whole (docs/adr/0013). It is the time a rolling upgrade
	// from 2.2 has to finish: a 2.2 replica cannot read a compacted
	// version. CARTOGRAPH_COMPACT_AFTER, default 24h; 0 turns it off.
	CompactAfter time.Duration

	// Reports is the reporting adapter (docs/adr/0014): "computed" from
	// the engine's reads on any store, "postgres" as views beside a
	// Postgres store, or "off" for a deployment that reports elsewhere.
	// CARTOGRAPH_REPORTS, default computed.
	Reports string

	// GraphLayout places the workspace graph: "layered" (the default), a
	// band per stage of the order of work from the top down, or "force",
	// a force-directed layout. CARTOGRAPH_GRAPH_LAYOUT.
	GraphLayout string

	// Semantic is the syntax the KPIs export in as a semantic layer
	// (TAXONOMY.md D57): "dbt" (the default) or "off".
	// CARTOGRAPH_SEMANTIC.
	Semantic string

	// Decide is the decision model the engine asks about what people
	// write (docs/adr/0023): "off" (the default) or "laya", a Laya
	// sidecar at DecideURL, each call given at most DecideTimeout.
	// CARTOGRAPH_DECIDE, CARTOGRAPH_DECIDE_URL, CARTOGRAPH_DECIDE_TIMEOUT.
	Decide        string
	DecideURL     string
	DecideTimeout time.Duration

	// MCP serves agents at /api/v1/mcp (docs/adr/0016): "off" (the
	// default) or "on". With an access list, only the roles its
	// mapping's agents key names may use one. CARTOGRAPH_MCP.
	MCP string

	// MCPTrace keeps every MCP tool call by its shape, for governance
	// and analysis (docs/adr/0028): "off" (the default) or a file path,
	// to which calls are appended as JSON lines. CARTOGRAPH_MCP_TRACE.
	MCPTrace string
	// MCPAuth is who authenticates an agent's requests: "proxy" (the
	// default), whatever authenticates every other request, checking the
	// tokens of the authorization server MCPIssuer names; or
	// "cartograph", Cartograph's own authorization server, at MCPIssuer,
	// which is then Cartograph's public address. CARTOGRAPH_MCP_AUTH.
	MCPAuth string
	// MCPIssuer is the authorization server MCP clients sign in with,
	// advertised as RFC 9728 protected resource metadata; empty
	// advertises none. CARTOGRAPH_MCP_ISSUER.
	MCPIssuer string
	// AgentKey signs what Cartograph's own authorization server issues:
	// at least 32 bytes, the same on every replica. CARTOGRAPH_AGENT_KEY.
	AgentKey string

	// DrainDelay is how long, after SIGTERM, the server keeps serving
	// while /readyz answers 503, so load balancers stop sending it work
	// before it stops accepting any. CARTOGRAPH_DRAIN_DELAY, default 0.
	DrainDelay time.Duration

	// Watch reloads manifests when their files change on disk.
	// CARTOGRAPH_WATCH, default true.
	Watch bool

	// Chromium is the browser to print PDFs with. CARTOGRAPH_CHROMIUM, default
	// empty: the printer then tries the vault's settings, CHROMIUM, then
	// the PATH.
	Chromium string

	// LogFormat is "text" or "json". CARTOGRAPH_LOG_FORMAT, default "text".
	// Logs always go to stdout; the environment decides where they end up.
	LogFormat string

	// LogLevel is debug, info, warn or error. CARTOGRAPH_LOG_LEVEL, default
	// "info".
	LogLevel string

	// ShutdownTimeout is how long in-flight requests get after SIGTERM.
	// CARTOGRAPH_SHUTDOWN_TIMEOUT, default 10s.
	ShutdownTimeout time.Duration

	// Auth selects the authenticator: "none" (every request is the
	// vault's operator) or "proxy" (the identity is the value of
	// AuthProxyHeader, set by an authenticating reverse proxy that is the
	// only way to reach the server). CARTOGRAPH_AUTH, default "none".
	Auth string

	// AuthProxyHeader is the header the proxy authenticator reads.
	// CARTOGRAPH_AUTH_PROXY_HEADER, default "X-Forwarded-User".
	AuthProxyHeader string

	// Authz selects the authorizer: "allow" (every principal may do
	// everything), "roles" (ReadRoles and WriteRoles decide; an anonymous
	// principal is refused, so pair it with an authenticator), or
	// "access" (an access list of people, four roles and teams, with
	// AccessFile mapping directory groups onto them; docs/adr/0011).
	// CARTOGRAPH_AUTHZ, default "allow".
	Authz string

	// AccessFile is the mapping from directory groups to roles and teams
	// for the access authorizer. CARTOGRAPH_ACCESS_FILE, default empty:
	// no group grants anything, and only people an administrator lists
	// may sign in.
	AccessFile string

	// ReadRoles and WriteRoles are comma-separated role names for the
	// roles authorizer. CARTOGRAPH_READ_ROLES (empty: any authenticated
	// principal may read), CARTOGRAPH_WRITE_ROLES (empty: nobody may write).
	ReadRoles  string
	WriteRoles string

	// Codec is the manifest syntax, "yaml" or "json". CARTOGRAPH_CODEC,
	// default "yaml". A vault is written in one codec; changing it is a
	// migration, not a restart.
	Codec string

	// ImportDir, when set, is imported on startup before serving.
	// CARTOGRAPH_IMPORT, default empty.
	ImportDir string
}

// Getenv is the shape of os.Getenv, so a test can supply its own.
type Getenv func(key string) string

// Defaults is the configuration a laptop needs: serve the current
// directory on :8080, text logs, no authentication.
func Defaults() Config {
	return Config{
		Addr:            ":8080",
		Vault:           ".",
		Watch:           true,
		LogFormat:       "text",
		LogLevel:        "info",
		ShutdownTimeout: 10 * time.Second,
		SyncPing:        20 * time.Second,
		SyncIdle:        10 * time.Minute,
		DocCache:        1000,
		CompactAfter:    24 * time.Hour,
		Reports:         "computed",
		GraphLayout:     "layered",
		Semantic:        "dbt",
		Decide:          "off",
		DecideURL:       "http://127.0.0.1:8411",
		DecideTimeout:   5 * time.Second,
		MCP:             "off",
		MCPAuth:         "proxy",
		Auth:            "none",
		AuthProxyHeader: "X-Forwarded-User",
		Authz:           "allow",
		Codec:           "yaml",
	}
}

// FromEnv returns Defaults overridden by every CARTOGRAPH_ variable getenv
// knows. A variable with an unparseable value is an error that names it.
func FromEnv(getenv Getenv) (Config, error) {
	c := Defaults()
	if v := getenv("CARTOGRAPH_ADDR"); v != "" {
		c.Addr = v
	}
	if v := getenv("CARTOGRAPH_VAULT"); v != "" {
		c.Vault = v
	}
	if v := getenv("CARTOGRAPH_STORE"); v != "" {
		c.Store = v
	}
	if v := getenv("CARTOGRAPH_FANOUT"); v != "" {
		c.Fanout = v
	}
	if v := getenv("CARTOGRAPH_FANOUT_URL"); v != "" {
		c.FanoutURL = v
	}
	if v := getenv("CARTOGRAPH_METRICS_ADDR"); v != "" {
		c.MetricsAddr = v
	}
	for _, d := range []struct {
		key string
		to  *time.Duration
	}{{"CARTOGRAPH_SYNC_PING", &c.SyncPing}, {"CARTOGRAPH_SYNC_IDLE", &c.SyncIdle}, {"CARTOGRAPH_DRAIN_DELAY", &c.DrainDelay}, {"CARTOGRAPH_DECIDE_TIMEOUT", &c.DecideTimeout}} {
		if v := getenv(d.key); v != "" {
			p, err := time.ParseDuration(v)
			if err != nil {
				return c, fmt.Errorf("%s: %w", d.key, err)
			}
			*d.to = p
		}
	}
	if v := getenv("CARTOGRAPH_DOC_CACHE"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return c, fmt.Errorf("CARTOGRAPH_DOC_CACHE: %w", err)
		}
		c.DocCache = n
	}
	if v := getenv("CARTOGRAPH_WATCH"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return c, fmt.Errorf("CARTOGRAPH_WATCH: %w", err)
		}
		c.Watch = b
	}
	if v := getenv("CARTOGRAPH_CHROMIUM"); v != "" {
		c.Chromium = v
	}
	if v := getenv("CARTOGRAPH_LOG_FORMAT"); v != "" {
		c.LogFormat = v
	}
	if v := getenv("CARTOGRAPH_LOG_LEVEL"); v != "" {
		c.LogLevel = v
	}
	if v := getenv("CARTOGRAPH_SHUTDOWN_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return c, fmt.Errorf("CARTOGRAPH_SHUTDOWN_TIMEOUT: %w", err)
		}
		c.ShutdownTimeout = d
	}
	if v := getenv("CARTOGRAPH_COMPACT_AFTER"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return c, fmt.Errorf("CARTOGRAPH_COMPACT_AFTER: %w", err)
		}
		c.CompactAfter = d
	}
	if v := getenv("CARTOGRAPH_MCP"); v != "" {
		c.MCP = v
	}
	if v := getenv("CARTOGRAPH_MCP_TRACE"); v != "" {
		c.MCPTrace = v
	}
	if v := getenv("CARTOGRAPH_MCP_ISSUER"); v != "" {
		c.MCPIssuer = v
	}
	if v := getenv("CARTOGRAPH_MCP_AUTH"); v != "" {
		c.MCPAuth = v
	}
	c.AgentKey = getenv("CARTOGRAPH_AGENT_KEY")
	if v := getenv("CARTOGRAPH_REPORTS"); v != "" {
		c.Reports = v
	}
	if v := getenv("CARTOGRAPH_GRAPH_LAYOUT"); v != "" {
		c.GraphLayout = v
	}
	if v := getenv("CARTOGRAPH_SEMANTIC"); v != "" {
		c.Semantic = v
	}
	if v := getenv("CARTOGRAPH_DECIDE"); v != "" {
		c.Decide = v
	}
	if v := getenv("CARTOGRAPH_DECIDE_URL"); v != "" {
		c.DecideURL = v
	}

	if v := getenv("CARTOGRAPH_AUTH"); v != "" {
		c.Auth = v
	}
	if v := getenv("CARTOGRAPH_AUTH_PROXY_HEADER"); v != "" {
		c.AuthProxyHeader = v
	}
	if v := getenv("CARTOGRAPH_AUTHZ"); v != "" {
		c.Authz = v
	}
	if v := getenv("CARTOGRAPH_ACCESS_FILE"); v != "" {
		c.AccessFile = v
	}
	if v := getenv("CARTOGRAPH_READ_ROLES"); v != "" {
		c.ReadRoles = v
	}
	if v := getenv("CARTOGRAPH_WRITE_ROLES"); v != "" {
		c.WriteRoles = v
	}
	if v := getenv("CARTOGRAPH_CODEC"); v != "" {
		c.Codec = v
	}
	if v := getenv("CARTOGRAPH_IMPORT"); v != "" {
		c.ImportDir = v
	}
	return c, c.Validate()
}

// Flags binds every setting to fs, with the current values as defaults,
// so a flag overrides the environment and the environment overrides the
// default.
func (c *Config) Flags(fs *flag.FlagSet) {
	fs.StringVar(&c.Addr, "addr", c.Addr, "address to listen on (CARTOGRAPH_ADDR)")
	fs.StringVar(&c.Vault, "vault", c.Vault, "vault directory or SQLite file to serve (CARTOGRAPH_VAULT)")
	fs.StringVar(&c.Store, "store", c.Store, "postgres:// URL of the database to serve instead of a vault (CARTOGRAPH_STORE)")
	fs.StringVar(&c.Fanout, "fanout", c.Fanout, "memory or postgres; default postgres with a Postgres store, else memory (CARTOGRAPH_FANOUT)")
	fs.StringVar(&c.FanoutURL, "fanout-url", c.FanoutURL, "direct postgres:// URL for the fan-out's LISTEN connection; default the store URL (CARTOGRAPH_FANOUT_URL)")
	fs.StringVar(&c.MetricsAddr, "metrics-addr", c.MetricsAddr, "address to serve Prometheus /metrics on; empty for none (CARTOGRAPH_METRICS_ADDR)")
	fs.DurationVar(&c.SyncIdle, "sync-idle", c.SyncIdle, "close a sync socket that changed nothing for this long; 0 never (CARTOGRAPH_SYNC_IDLE)")
	fs.DurationVar(&c.SyncPing, "sync-ping", c.SyncPing, "ping interval for idle sync sockets; 0 for none (CARTOGRAPH_SYNC_PING)")
	fs.IntVar(&c.DocCache, "doc-cache", c.DocCache, "shared documents kept in memory per replica (CARTOGRAPH_DOC_CACHE)")
	fs.DurationVar(&c.DrainDelay, "drain-delay", c.DrainDelay, "time to keep serving with /readyz at 503 after SIGTERM (CARTOGRAPH_DRAIN_DELAY)")
	fs.BoolVar(&c.Watch, "watch", c.Watch, "reload manifests when their files change (CARTOGRAPH_WATCH)")
	fs.StringVar(&c.Chromium, "chromium", c.Chromium, "browser to print PDFs with (CARTOGRAPH_CHROMIUM)")
	fs.StringVar(&c.LogFormat, "log-format", c.LogFormat, "text or json (CARTOGRAPH_LOG_FORMAT)")
	fs.StringVar(&c.LogLevel, "log-level", c.LogLevel, "debug, info, warn or error (CARTOGRAPH_LOG_LEVEL)")
	fs.StringVar(&c.MCP, "mcp", c.MCP, "serve agents over MCP at /api/v1/mcp: off or on (CARTOGRAPH_MCP)")
	fs.StringVar(&c.MCPTrace, "mcp-trace", c.MCPTrace, "keep every MCP tool call by its shape: off, or a file to append JSON lines to (CARTOGRAPH_MCP_TRACE)")
	fs.StringVar(&c.MCPAuth, "mcp-auth", c.MCPAuth, "who authenticates agents: proxy or cartograph (CARTOGRAPH_MCP_AUTH)")
	fs.StringVar(&c.MCPIssuer, "mcp-issuer", c.MCPIssuer, "the authorization server MCP clients sign in with (CARTOGRAPH_MCP_ISSUER)")
	fs.StringVar(&c.Reports, "reports", c.Reports, "reporting: computed, postgres (views, with a Postgres store) or off (CARTOGRAPH_REPORTS)")
	fs.StringVar(&c.GraphLayout, "graph-layout", c.GraphLayout, "how the workspace graph is placed: layered or force (CARTOGRAPH_GRAPH_LAYOUT)")
	fs.StringVar(&c.Semantic, "semantic", c.Semantic, "the syntax the KPIs export in as a semantic layer: dbt or off (CARTOGRAPH_SEMANTIC)")
	fs.StringVar(&c.Decide, "decide", c.Decide, "the decision model asked about what people write: off or laya (CARTOGRAPH_DECIDE)")
	fs.StringVar(&c.DecideURL, "decide-url", c.DecideURL, "the Laya sidecar's address (CARTOGRAPH_DECIDE_URL)")
	fs.DurationVar(&c.DecideTimeout, "decide-timeout", c.DecideTimeout, "the longest one decision may take (CARTOGRAPH_DECIDE_TIMEOUT)")
	fs.DurationVar(&c.CompactAfter, "compact-after", c.CompactAfter, "age at which a Postgres store keeps an old version as a patch; 0 is off (CARTOGRAPH_COMPACT_AFTER)")
	fs.DurationVar(&c.ShutdownTimeout, "shutdown-timeout", c.ShutdownTimeout, "grace period for in-flight requests on shutdown (CARTOGRAPH_SHUTDOWN_TIMEOUT)")
	fs.StringVar(&c.Auth, "auth", c.Auth, "none or proxy (CARTOGRAPH_AUTH)")
	fs.StringVar(&c.AuthProxyHeader, "auth-proxy-header", c.AuthProxyHeader, "header carrying the identity when auth is proxy (CARTOGRAPH_AUTH_PROXY_HEADER)")
	fs.StringVar(&c.Authz, "authz", c.Authz, "allow, roles or access (CARTOGRAPH_AUTHZ)")
	fs.StringVar(&c.AccessFile, "access-file", c.AccessFile, "the directory group mapping for authz access (CARTOGRAPH_ACCESS_FILE)")
	fs.StringVar(&c.ReadRoles, "read-roles", c.ReadRoles, "roles that may read, comma-separated (CARTOGRAPH_READ_ROLES)")
	fs.StringVar(&c.WriteRoles, "write-roles", c.WriteRoles, "roles that may write, comma-separated (CARTOGRAPH_WRITE_ROLES)")
	fs.StringVar(&c.Codec, "codec", c.Codec, "manifest syntax, yaml or json (CARTOGRAPH_CODEC)")
	fs.StringVar(&c.ImportDir, "import", c.ImportDir, "import this directory on startup (CARTOGRAPH_IMPORT)")
}

// Validate refuses a value no adapter can act on.
func (c Config) Validate() error {
	switch c.LogFormat {
	case "text", "json":
	default:
		return fmt.Errorf("log format %q: want text or json", c.LogFormat)
	}
	switch strings.ToLower(c.LogLevel) {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("log level %q: want debug, info, warn or error", c.LogLevel)
	}
	switch c.Auth {
	case "none", "proxy":
	default:
		return fmt.Errorf("auth %q: want none or proxy", c.Auth)
	}
	if c.Auth == "proxy" && c.AuthProxyHeader == "" {
		return fmt.Errorf("auth proxy needs a header name")
	}
	switch c.Authz {
	case "allow", "roles", "access":
	default:
		return fmt.Errorf("authz %q: want allow, roles or access", c.Authz)
	}
	if (c.Authz == "roles" || c.Authz == "access") && c.Auth == "none" {
		return fmt.Errorf("authz %s needs an authenticator: set CARTOGRAPH_AUTH", c.Authz)
	}
	if c.AccessFile != "" && c.Authz != "access" {
		return fmt.Errorf("an access file is for authz access: set CARTOGRAPH_AUTHZ=access")
	}
	switch c.Codec {
	case "yaml", "json":
	default:
		return fmt.Errorf("codec %q: want yaml or json", c.Codec)
	}
	if c.Store != "" && !IsPostgresURL(c.Store) {
		return fmt.Errorf("store %q: want a postgres:// URL, or nothing to serve the vault", Redact(c.Store))
	}
	switch c.Fanout {
	case "", "memory":
	case "postgres":
		if c.Store == "" {
			return fmt.Errorf("fanout postgres needs a Postgres store: set CARTOGRAPH_STORE")
		}
	default:
		return fmt.Errorf("fanout %q: want memory or postgres", c.Fanout)
	}
	if c.FanoutURL != "" {
		if !IsPostgresURL(c.FanoutURL) {
			return fmt.Errorf("fanout url %q: want a postgres:// URL", Redact(c.FanoutURL))
		}
		if c.FanoutAdapter() != "postgres" {
			return fmt.Errorf("fanout url is for the postgres fan-out: set CARTOGRAPH_STORE")
		}
	}
	if c.ShutdownTimeout < 0 {
		return fmt.Errorf("shutdown timeout must not be negative")
	}
	if c.SyncIdle < 0 || (c.SyncIdle > 0 && c.SyncIdle < time.Minute) {
		return fmt.Errorf("sync idle %s: want 0 (never) or at least 1m", c.SyncIdle)
	}
	if c.SyncPing < 0 || (c.SyncPing > 0 && c.SyncPing < time.Second) {
		return fmt.Errorf("sync ping %s: want 0 (off) or at least 1s", c.SyncPing)
	}
	if c.Decide != "off" && c.Decide != "laya" {
		return fmt.Errorf("decide %q: want off or laya", c.Decide)
	}
	if c.Decide == "laya" {
		if u, err := url.Parse(c.DecideURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("decide url %q: want the Laya sidecar's http address, such as http://127.0.0.1:8411", c.DecideURL)
		}
		if c.DecideTimeout <= 0 {
			return fmt.Errorf("decide timeout %s: want more than 0", c.DecideTimeout)
		}
	}
	if c.Semantic != "dbt" && c.Semantic != "off" {
		return fmt.Errorf("semantic %q: want dbt or off", c.Semantic)
	}
	if c.GraphLayout != "layered" && c.GraphLayout != "force" {
		return fmt.Errorf("graph layout %q: want layered or force", c.GraphLayout)
	}
	if c.MCP != "off" && c.MCP != "on" {
		return fmt.Errorf("mcp %q: want off or on", c.MCP)
	}
	switch c.MCPAuth {
	case "proxy":
	case "cartograph":
		if c.MCP != "on" {
			break
		}
		if u, err := url.Parse(c.MCPIssuer); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || (u.Path != "" && u.Path != "/") {
			return fmt.Errorf("mcp auth cartograph: set CARTOGRAPH_MCP_ISSUER to Cartograph's public address, such as https://cartograph.example.org")
		}
		if len(c.AgentKey) < 32 {
			return fmt.Errorf("mcp auth cartograph: set CARTOGRAPH_AGENT_KEY to a secret of at least 32 bytes, the same on every replica")
		}
		if c.Authz != "access" {
			return fmt.Errorf("mcp auth cartograph: agent grants are kept on the access list; set CARTOGRAPH_AUTHZ=access")
		}
	default:
		return fmt.Errorf("mcp auth %q: want proxy or cartograph", c.MCPAuth)
	}
	switch c.Reports {
	case "computed", "off":
	case "postgres":
		if !IsPostgresURL(c.Store) {
			return fmt.Errorf("reports postgres: the views are in the Postgres store; set CARTOGRAPH_STORE")
		}
	default:
		return fmt.Errorf("reports %q: want computed, postgres or off", c.Reports)
	}
	if c.CompactAfter != 0 && c.CompactAfter < time.Hour {
		return fmt.Errorf("compact after %s: want 0 (off) or at least 1h, the time a rolling upgrade has", c.CompactAfter)
	}
	if c.DocCache < 1 {
		return fmt.Errorf("doc cache %d: want at least 1", c.DocCache)
	}
	if c.DrainDelay < 0 {
		return fmt.Errorf("drain delay must not be negative")
	}
	return nil
}

// ListenAddr normalises Addr: a bare port becomes ":port", which is what a
// platform that hands out PORT expects to work.
func (c Config) ListenAddr() string {
	if _, err := strconv.Atoi(c.Addr); err == nil {
		return ":" + c.Addr
	}
	return c.Addr
}

// IsUnix reports whether Addr names a UNIX domain socket (unix:///path).
func (c Config) IsUnix() bool { return strings.HasPrefix(c.Addr, "unix://") }

// FanoutAdapter is the fan-out adapter to use: Fanout when set, else
// postgres for a Postgres store and memory for a vault.
func (c Config) FanoutAdapter() string {
	if c.Fanout != "" {
		return c.Fanout
	}
	if c.Store != "" {
		return "postgres"
	}
	return "memory"
}

// IsPostgresURL reports whether s names a Postgres database rather
// than a vault directory or a SQLite file.
func IsPostgresURL(s string) bool {
	return strings.HasPrefix(s, "postgres://") || strings.HasPrefix(s, "postgresql://")
}

// Redact hides the password in a database URL, so it can be logged or
// put in an error.
func Redact(s string) string {
	u, err := url.Parse(s)
	if err != nil || u.User == nil {
		return s
	}
	return u.Redacted()
}
