package config

import (
	"flag"
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) Getenv {
	return func(k string) string { return m[k] }
}

func TestDefaultsWorkWithNoEnvironment(t *testing.T) {
	c, err := FromEnv(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.Addr != ":8080" || c.Vault != "." || !c.Watch || c.Auth != "none" || c.LogFormat != "text" {
		t.Fatalf("unexpected defaults: %+v", c)
	}
}

func TestEnvironmentOverridesDefaults(t *testing.T) {
	c, err := FromEnv(env(map[string]string{
		"CARTOGRAPH_ADDR":             "9090",
		"CARTOGRAPH_VAULT":            "/data/vault",
		"CARTOGRAPH_WATCH":            "false",
		"CARTOGRAPH_LOG_FORMAT":       "json",
		"CARTOGRAPH_SHUTDOWN_TIMEOUT": "3s",
		"CARTOGRAPH_AUTH":             "proxy",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.ListenAddr() != ":9090" {
		t.Fatalf("a bare port becomes :port, got %q", c.ListenAddr())
	}
	if c.Vault != "/data/vault" || c.Watch || c.LogFormat != "json" || c.ShutdownTimeout != 3*time.Second || c.Auth != "proxy" {
		t.Fatalf("environment not applied: %+v", c)
	}
}

func TestFlagsOverrideEnvironment(t *testing.T) {
	c, err := FromEnv(env(map[string]string{"CARTOGRAPH_ADDR": ":1", "CARTOGRAPH_LOG_LEVEL": "warn"}))
	if err != nil {
		t.Fatal(err)
	}
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	c.Flags(fs)
	if err := fs.Parse([]string{"-addr", ":2"}); err != nil {
		t.Fatal(err)
	}
	if c.Addr != ":2" {
		t.Fatalf("flag should win, got %q", c.Addr)
	}
	if c.LogLevel != "warn" {
		t.Fatalf("an unset flag keeps the environment's value, got %q", c.LogLevel)
	}
}

func TestBadValuesAreNamed(t *testing.T) {
	for name, m := range map[string]map[string]string{
		"watch":   {"CARTOGRAPH_WATCH": "maybe"},
		"timeout": {"CARTOGRAPH_SHUTDOWN_TIMEOUT": "soon"},
		"format":  {"CARTOGRAPH_LOG_FORMAT": "xml"},
		"auth":    {"CARTOGRAPH_AUTH": "magic"},
	} {
		if _, err := FromEnv(env(m)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestAuthzAndCodecValidation(t *testing.T) {
	if _, err := FromEnv(env(map[string]string{"CARTOGRAPH_AUTHZ": "roles"})); err == nil {
		t.Fatal("roles without an authenticator must be refused")
	}
	c, err := FromEnv(env(map[string]string{"CARTOGRAPH_AUTHZ": "roles", "CARTOGRAPH_AUTH": "proxy", "CARTOGRAPH_WRITE_ROLES": "editors"}))
	if err != nil || c.WriteRoles != "editors" {
		t.Fatalf("roles with proxy: %v %+v", err, c)
	}
	if _, err := FromEnv(env(map[string]string{"CARTOGRAPH_CODEC": "toml"})); err == nil {
		t.Fatal("an unknown codec must be refused")
	}
	c, err = FromEnv(env(map[string]string{"CARTOGRAPH_CODEC": "json"}))
	if err != nil || c.Codec != "json" {
		t.Fatalf("json codec: %v", err)
	}
}

func TestStoreSelectsPostgresAndItsFanout(t *testing.T) {
	c, err := FromEnv(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.Store != "" || c.FanoutAdapter() != "memory" {
		t.Fatalf("a vault fans out in memory: store %q, fanout %q", c.Store, c.FanoutAdapter())
	}
	c, err = FromEnv(env(map[string]string{"CARTOGRAPH_STORE": "postgres://u:secret@db/cartograph"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.FanoutAdapter() != "postgres" {
		t.Fatalf("a Postgres store fans out over Postgres by default, got %q", c.FanoutAdapter())
	}
	c, err = FromEnv(env(map[string]string{"CARTOGRAPH_STORE": "postgresql://db/cartograph", "CARTOGRAPH_FANOUT": "memory"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.FanoutAdapter() != "memory" {
		t.Fatalf("CARTOGRAPH_FANOUT wins, got %q", c.FanoutAdapter())
	}
}

func TestStoreAndFanoutRefusals(t *testing.T) {
	for name, m := range map[string]map[string]string{
		"store not postgres":        {"CARTOGRAPH_STORE": "mysql://db"},
		"fanout unknown":            {"CARTOGRAPH_FANOUT": "carrier-pigeon"},
		"postgres fanout, no store": {"CARTOGRAPH_FANOUT": "postgres"},
	} {
		if _, err := FromEnv(env(m)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	_, err := FromEnv(env(map[string]string{"CARTOGRAPH_STORE": "mysql://u:secret@db"}))
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("a refused URL is named without its password: %v", err)
	}
}

// The settings a replicated deployment tunes: where the fan-out listens,
// metrics, the sync ping, the cache bound and the drain delay.
func TestScaleSettings(t *testing.T) {
	c, err := FromEnv(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.SyncPing != 20*time.Second || c.DocCache != 1000 || c.DrainDelay != 0 || c.MetricsAddr != "" || c.FanoutURL != "" {
		t.Fatalf("defaults: ping %s cache %d drain %s metrics %q fanout url %q", c.SyncPing, c.DocCache, c.DrainDelay, c.MetricsAddr, c.FanoutURL)
	}
	c, err = FromEnv(env(map[string]string{
		"CARTOGRAPH_STORE":        "postgres://app@pgbouncer:6432/db",
		"CARTOGRAPH_FANOUT_URL":   "postgres://app@postgres:5432/db",
		"CARTOGRAPH_METRICS_ADDR": ":9090",
		"CARTOGRAPH_SYNC_PING":    "25s",
		"CARTOGRAPH_DOC_CACHE":    "5000",
		"CARTOGRAPH_DRAIN_DELAY":  "10s",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.FanoutURL != "postgres://app@postgres:5432/db" || c.MetricsAddr != ":9090" || c.SyncPing != 25*time.Second || c.DocCache != 5000 || c.DrainDelay != 10*time.Second {
		t.Fatalf("not applied: %+v", c)
	}
	for name, vars := range map[string]map[string]string{
		"fanout url without a postgres store": {"CARTOGRAPH_FANOUT_URL": "postgres://x/db"},
		"fanout url that is not postgres":     {"CARTOGRAPH_STORE": "postgres://x/db", "CARTOGRAPH_FANOUT_URL": "mysql://x/db"},
		"a ping too short to be a ping":       {"CARTOGRAPH_SYNC_PING": "10ms"},
		"an unreadable ping":                  {"CARTOGRAPH_SYNC_PING": "soon"},
		"an empty cache":                      {"CARTOGRAPH_DOC_CACHE": "0"},
		"a cache that is not a number":        {"CARTOGRAPH_DOC_CACHE": "many"},
		"a negative drain":                    {"CARTOGRAPH_DRAIN_DELAY": "-1s"},
	} {
		if _, err := FromEnv(env(vars)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if c, err := FromEnv(env(map[string]string{"CARTOGRAPH_SYNC_PING": "0"})); err != nil || c.SyncPing != 0 {
		t.Errorf("0 turns pings off: %v %v", c.SyncPing, err)
	}
}

func TestAccessSettings(t *testing.T) {
	c, err := FromEnv(env(map[string]string{"CARTOGRAPH_AUTH": "proxy", "CARTOGRAPH_AUTHZ": "access", "CARTOGRAPH_ACCESS_FILE": "/etc/cartograph/access.yaml"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Authz != "access" || c.AccessFile != "/etc/cartograph/access.yaml" {
		t.Fatalf("not applied: %+v", c)
	}
	for name, vars := range map[string]map[string]string{
		"access without an authenticator":       {"CARTOGRAPH_AUTHZ": "access"},
		"an access file for another authorizer": {"CARTOGRAPH_AUTH": "proxy", "CARTOGRAPH_ACCESS_FILE": "access.yaml"},
		"an unknown authorizer":                 {"CARTOGRAPH_AUTH": "proxy", "CARTOGRAPH_AUTHZ": "acl"},
	} {
		if _, err := FromEnv(env(vars)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestAgentSettings(t *testing.T) {
	ok := map[string]string{"CARTOGRAPH_AUTH": "proxy", "CARTOGRAPH_AUTHZ": "access", "CARTOGRAPH_MCP": "on",
		"CARTOGRAPH_MCP_AUTH": "cartograph", "CARTOGRAPH_MCP_ISSUER": "https://cartograph.example.org",
		"CARTOGRAPH_AGENT_KEY": strings.Repeat("k", 32)}
	c, err := FromEnv(env(ok))
	if err != nil {
		t.Fatal(err)
	}
	if c.MCPAuth != "cartograph" || len(c.AgentKey) != 32 {
		t.Fatalf("not applied: %+v", c)
	}
	if c, _ := FromEnv(env(nil)); c.MCPAuth != "proxy" {
		t.Fatalf("the default: %q", c.MCPAuth)
	}
	with := func(k, v string) map[string]string {
		m := map[string]string{}
		for kk, vv := range ok {
			m[kk] = vv
		}
		m[k] = v
		return m
	}
	for name, vars := range map[string]map[string]string{
		"an unknown mode":            with("CARTOGRAPH_MCP_AUTH", "jwt"),
		"no public address":          with("CARTOGRAPH_MCP_ISSUER", ""),
		"an address with a path":     with("CARTOGRAPH_MCP_ISSUER", "https://cartograph.example.org/dex"),
		"a short key":                with("CARTOGRAPH_AGENT_KEY", "short"),
		"grants with no access list": with("CARTOGRAPH_AUTHZ", "roles"),
	} {
		if _, err := FromEnv(env(vars)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
