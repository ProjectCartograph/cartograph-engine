// Package arch holds the dependency rule as a test, so the shape of the
// system is checked in the ten-second gate and not by review alone.
//
// The rule is clean architecture's: dependencies point inward. The
// entities (pkg/merge, the kinds) know nothing about the engine; the
// engine (the use cases) knows nothing about any adapter, only the
// ports; adapters know the engine and the ports; the composition root
// (cmd) knows everything. A package that needs something from a layer
// outside its own gets it through a port, injected at the root.
package arch

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// rules name, per package (relative to the module), the module packages
// it must not depend on, transitively. A prefix ending in "/" forbids a
// whole tree.
var rules = map[string][]string{
	// Entities: no dependency on anything in the module.
	"pkg/merge":          {"internal/", "pkg/client", "pkg/uiconformance", "cmd/"},
	"internal/kinds/kit": {"internal/engine", "internal/store", "internal/api", "internal/codec", "cmd/"},
	// The kinds know their schemas and the kit, never the engine or an adapter.
	"internal/kinds": {"internal/engine", "internal/api", "internal/store", "internal/codec", "internal/printer", "internal/auth", "cmd/"},
	// Ports: the store port knows only the entities.
	"internal/store":   {"internal/engine", "internal/api", "internal/store/", "internal/codec", "cmd/"},
	"internal/codec":   {"internal/engine", "internal/api", "internal/codec/", "internal/store", "cmd/"},
	"internal/printer": {"internal/", "cmd/"},
	"internal/auth":    {"internal/engine", "internal/api", "internal/store", "internal/auth/", "cmd/"},
	// The use cases: ports only, never an adapter, never a driver.
	"internal/engine": {
		"internal/api", "internal/spa", "internal/render", "cmd/",
		"internal/store/sqlite", "internal/store/vault", "internal/store/memory",
		"internal/codec/yaml", "internal/codec/json",
		"internal/printer/chromium",
		"internal/auth",
		"internal/yamlfmt",
		"gopkg.in/yaml.v3", "modernc.org/sqlite", "github.com/fsnotify/fsnotify",
	},
	// The client port: entities only. Its adapters sit below it.
	"pkg/client":        {"internal/", "pkg/client/", "cmd/"},
	"pkg/uiconformance": {"internal/", "pkg/client/", "cmd/"},
	// Driven adapters: the engine's ports, never the drivers.
	"internal/store/vault":      {"internal/engine", "internal/api", "cmd/"},
	"internal/store/sqlite":     {"internal/engine", "internal/api", "cmd/"},
	"internal/store/memory":     {"internal/engine", "internal/api", "cmd/"},
	"internal/codec/yaml":       {"internal/engine", "internal/api", "internal/store", "cmd/"},
	"internal/codec/json":       {"internal/engine", "internal/api", "internal/store", "cmd/"},
	"internal/printer/chromium": {"internal/engine", "internal/api", "internal/store", "cmd/"},
	// Driving adapters: never each other's internals, never the root.
	"internal/api":      {"internal/store/sqlite", "internal/store/vault", "internal/store/memory", "cmd/"},
	"pkg/client/inproc": {"internal/api", "internal/store/sqlite", "internal/store/vault", "cmd/"},
	"pkg/client/remote": {"internal/engine", "internal/api", "internal/store", "cmd/"},
}

func TestDependenciesPointInward(t *testing.T) {
	module := strings.TrimSpace(run(t, "go", "list", "-m"))
	for pkg, forbidden := range rules {
		full := module + "/" + pkg
		out := run(t, "go", "list", "-deps", "-json=ImportPath", full)
		var deps []string
		dec := json.NewDecoder(strings.NewReader(out))
		for dec.More() {
			var p struct{ ImportPath string }
			if err := dec.Decode(&p); err != nil {
				t.Fatalf("%s: %v", pkg, err)
			}
			deps = append(deps, p.ImportPath)
		}
		for _, d := range deps {
			if d == full {
				continue
			}
			// Only this module's packages and the named third-party ones
			// are rules; the standard library's own internal/ is not ours.
			if !strings.HasPrefix(d, module+"/") && !strings.Contains(d, ".") {
				continue
			}
			rel := strings.TrimPrefix(d, module+"/")
			for _, f := range forbidden {
				if strings.HasSuffix(f, "/") && strings.HasPrefix(rel, f) && rel != strings.TrimSuffix(f, "/") {
					t.Errorf("%s depends on %s, which the dependency rule forbids (%s)", pkg, rel, f)
				} else if !strings.HasSuffix(f, "/") && (rel == f || d == f) {
					t.Errorf("%s depends on %s, which the dependency rule forbids", pkg, rel)
				}
			}
		}
	}
}

func run(t *testing.T, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = "../.."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
	return string(out)
}
