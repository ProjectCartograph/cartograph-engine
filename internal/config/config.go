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

	// DocCache bounds how many shared documents a replica keeps in
	// memory; the least recently used are dropped first, and a dropped
	// one is loaded again from the store when next needed.
	// CARTOGRAPH_DOC_CACHE, default 1000.
	DocCache int

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
	// everything) or "roles" (ReadRoles and WriteRoles decide; an anonymous
	// principal is refused, so pair it with an authenticator).
	// CARTOGRAPH_AUTHZ, default "allow".
	Authz string

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
		DocCache:        1000,
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
	if v := getenv("CARTOGRAPH_AUTH"); v != "" {
		c.Auth = v
	}
	if v := getenv("CARTOGRAPH_AUTH_PROXY_HEADER"); v != "" {
		c.AuthProxyHeader = v
	}
	if v := getenv("CARTOGRAPH_AUTHZ"); v != "" {
		c.Authz = v
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
	fs.IntVar(&c.DocCache, "doc-cache", c.DocCache, "shared documents kept in memory per replica (CARTOGRAPH_DOC_CACHE)")
	fs.BoolVar(&c.Watch, "watch", c.Watch, "reload manifests when their files change (CARTOGRAPH_WATCH)")
	fs.StringVar(&c.Chromium, "chromium", c.Chromium, "browser to print PDFs with (CARTOGRAPH_CHROMIUM)")
	fs.StringVar(&c.LogFormat, "log-format", c.LogFormat, "text or json (CARTOGRAPH_LOG_FORMAT)")
	fs.StringVar(&c.LogLevel, "log-level", c.LogLevel, "debug, info, warn or error (CARTOGRAPH_LOG_LEVEL)")
	fs.DurationVar(&c.ShutdownTimeout, "shutdown-timeout", c.ShutdownTimeout, "grace period for in-flight requests on shutdown (CARTOGRAPH_SHUTDOWN_TIMEOUT)")
	fs.StringVar(&c.Auth, "auth", c.Auth, "none or proxy (CARTOGRAPH_AUTH)")
	fs.StringVar(&c.AuthProxyHeader, "auth-proxy-header", c.AuthProxyHeader, "header carrying the identity when auth is proxy (CARTOGRAPH_AUTH_PROXY_HEADER)")
	fs.StringVar(&c.Authz, "authz", c.Authz, "allow or roles (CARTOGRAPH_AUTHZ)")
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
	case "allow", "roles":
	default:
		return fmt.Errorf("authz %q: want allow or roles", c.Authz)
	}
	if c.Authz == "roles" && c.Auth == "none" {
		return fmt.Errorf("authz roles needs an authenticator: set CARTOGRAPH_AUTH")
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
	if c.DocCache < 1 {
		return fmt.Errorf("doc cache %d: want at least 1", c.DocCache)
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
