package config

import (
	"flag"
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
