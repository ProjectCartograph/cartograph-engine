// Package arch holds the dependency rule as a test, so the shape of the
// system is checked in the ten-second gate and not by review alone.
//
// The rule is clean architecture's: dependencies point inward. The
// entities (the kinds, the contract) know nothing about the engine; the
// engine (the use cases) knows nothing about any adapter, only the
// ports; adapters know the engine and the ports; the composition root
// (cmd) knows everything. A package that needs something from a layer
// outside its own gets it through a port, injected at the root.
//
// Every package in the module has a rule, so a new package cannot slip
// in without somebody saying which ring it belongs to.
package arch

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// drivers are the frameworks and drivers of the outer ring: a wire, a
// database, a process, a syntax, a file watcher. Only adapters, and the
// ports that are themselves HTTP middleware, may use them.
var drivers = []string{
	"net/http", "database/sql", "os/exec",
	"gopkg.in/yaml.v3", "go.yaml.in/yaml", "modernc.org/sqlite", "github.com/fsnotify/fsnotify",
	wazero,
	pgx,
}

// wazero runs the Automerge module. It is the CRDT adapter's driver and
// no one else's: another package needing WebAssembly is a new port.
const wazero = "github.com/tetratelabs/wazero"

// pgx is the Postgres driver. Only the Postgres adapters may use it; the
// root hands their pool from one to the other.
const pgx = "github.com/jackc/pgx"

// postgresAdapters are the packages pgx is allowed in.
var postgresAdapters = []string{"internal/store/postgres", "internal/fanout/postgres", "internal/reporting/postgres"}

// outer is what only the composition root and the driving adapters
// beside it may know.
var outer = []string{"internal/api", "internal/spa", "internal/render", "internal/config", "internal/mcp", "internal/oauth", "cmd"}

// adapters are every driven adapter and the syntax helpers they share.
// A port or the engine knowing one of these would point a dependency
// outward.
var adapters = []string{
	"internal/store/", "internal/codec/", "internal/printer/", "internal/auth/",
	"internal/crdt/", "internal/fanout/", "internal/reporting/", "internal/layout/",
	"internal/yamlfmt", "pkg/client/",
}

// leaf forbids everything in the module and every driver: a package that
// stands alone.
var leaf = join([]string{"internal", "pkg", "cmd"}, drivers)

// adapter is the rule for a driven adapter named own: it knows the ports
// and the entities, never the engine, a driving adapter, the root or
// another adapter (the root composes adapters, they do not compose each
// other). The syntax helpers are shared, and allowed.
func adapter(own string) []string {
	siblings := []string{
		"internal/store/vault", "internal/store/sqlite", "internal/store/memory", "internal/store/postgres",
		"internal/codec/yaml", "internal/codec/json",
		"internal/printer/chromium", "internal/auth/access", "internal/auth/proxy", "internal/auth/roles",
		"internal/fanout/memory", "internal/fanout/postgres",
		"internal/store/conformance", "internal/codec/conformance", "internal/fanout/conformance",
		"internal/crdt/automerge", "internal/crdt/conformance",
		"internal/reporting/postgres", "internal/reporting/conformance",
		"internal/layout/force", "internal/layout/conformance",
	}
	var forbid []string
	for _, s := range siblings {
		if s != own {
			forbid = append(forbid, s)
		}
	}
	if own != "internal/crdt/automerge" {
		forbid = append(forbid, wazero)
	}
	if !contains(postgresAdapters, own) {
		forbid = append(forbid, pgx)
	}
	return join(forbid, outer, []string{"internal/engine", "internal/kinds", "pkg/client", "pkg/uiconformance"})
}

// rules name, per package (relative to the module), the packages it must
// not depend on, transitively. A key ending in "/" is the rule for every
// package under it that has no rule of its own. A forbidden name covers
// that package and everything under it; ending it in "/" covers only
// what is under it.
var rules = map[string][]string{
	// The composition root knows everything; the test knows nothing.
	"cmd/cartograph": nil,
	"internal/arch":  nil,

	// Entities: nothing in the module, no driver.
	"internal/kinds/kit": leaf,
	"internal/contract":  leaf,
	"internal/sentence":  leaf,
	// The kinds know their schemas and the kit, never the engine, a port
	// or an adapter.
	"internal/kinds":  join([]string{"internal/engine", "internal/store", "internal/codec", "internal/printer", "internal/auth"}, outer, adapters, drivers),
	"internal/kinds/": join([]string{"internal/engine", "internal/store", "internal/codec", "internal/printer", "internal/auth"}, outer, adapters, drivers),

	// Ports: the entities only. The identity ports are HTTP middleware,
	// so net/http is theirs; nothing else from the outer ring is, and
	// auth knows only the identity types beneath it.
	"internal/store":   join([]string{"internal/engine", "internal/kinds", "internal/store/", "internal/codec", "pkg/client", "pkg/uiconformance"}, outer, adapters, drivers),
	"internal/codec":   join([]string{"internal", "pkg", "cmd"}, drivers),
	"internal/printer": join([]string{"internal", "pkg", "cmd"}, drivers),
	"internal/layout":  join([]string{"internal", "pkg", "cmd"}, drivers),
	"internal/crdt":    join([]string{"internal", "pkg", "cmd"}, drivers),
	"internal/fanout":  join([]string{"internal", "pkg", "cmd"}, drivers),
	"internal/auth": join([]string{"internal/engine", "internal/kinds", "internal/store", "internal/codec", "internal/printer",
		"internal/crdt", "internal/fanout", "internal/contract", "internal/sentence", "internal/syncserver", "internal/yamlfmt",
		"pkg", "cmd"}, outer, adapters, without(drivers, "net/http")),
	// Who a request runs as and what they ask, without HTTP: the part of
	// the identity port the engine may know. It stands alone.
	"internal/identity": leaf,
	"pkg/client":        join([]string{"internal", "pkg/client/", "pkg/uiconformance", "cmd"}, drivers),
	"pkg/uiconformance": join([]string{"internal", "pkg/client/", "cmd"}, drivers),

	// The use cases: ports and entities only, never an adapter, never a
	// driver, never a syntax.
	// Nor does it know reporting, which is not the core (docs/adr/0014).
	"internal/engine": join([]string{"pkg/uiconformance", "internal/reporting"}, outer, adapters, drivers),

	// Driven adapters.
	"internal/store/vault":        adapter("internal/store/vault"),
	"internal/store/sqlite":       adapter("internal/store/sqlite"),
	"internal/store/memory":       adapter("internal/store/memory"),
	"internal/store/manifestmeta": adapter(""),
	"internal/store/conformance":  adapter("internal/store/conformance"),
	"internal/store/postgres":     adapter("internal/store/postgres"),
	// Fan-out adapters carry bytes between replicas; they know no store
	// and no syntax.
	"internal/fanout/memory":      join([]string{"internal/store", "internal/codec"}, adapter("internal/fanout/memory")),
	"internal/fanout/postgres":    join([]string{"internal/store", "internal/codec"}, adapter("internal/fanout/postgres")),
	"internal/fanout/conformance": join([]string{"internal/store", "internal/codec"}, adapter("internal/fanout/conformance")),
	"internal/codec/yaml":         join([]string{"internal/store"}, adapter("internal/codec/yaml")),
	"internal/codec/json":         join([]string{"internal/store"}, adapter("internal/codec/json")),
	"internal/codec/conformance":  join([]string{"internal/store"}, adapter("internal/codec/conformance")),
	"internal/printer/chromium":   join([]string{"internal/store", "internal/codec"}, adapter("internal/printer/chromium")),
	// A layout places points; it knows its port and nothing else, and its
	// suite holds any layout to the same promises.
	"internal/layout/force":       join([]string{"internal/store", "internal/codec"}, drivers, adapter("internal/layout/force")),
	"internal/layout/conformance": join([]string{"internal/store", "internal/codec"}, drivers, adapter("internal/layout/conformance")),
	"internal/auth/access":        join([]string{"internal/store", "internal/codec"}, adapter("internal/auth/access")),
	"internal/auth/proxy":         join([]string{"internal/store", "internal/codec"}, adapter("internal/auth/proxy")),
	"internal/auth/roles":         join([]string{"internal/store", "internal/codec"}, adapter("internal/auth/roles")),
	"internal/yamlfmt":            join([]string{"internal", "pkg", "cmd"}, without(drivers, "go.yaml.in/yaml")),
	// The CRDT adapter knows its port and wazero; its suite knows the
	// port only, so it holds any adapter to the same promises.
	"internal/crdt/automerge":   join([]string{"internal/store", "internal/codec", "internal/printer", "internal/auth", "internal/fanout"}, adapter("internal/crdt/automerge")),
	"internal/crdt/conformance": join([]string{"internal/store", "internal/codec", "internal/printer", "internal/auth", "internal/fanout"}, drivers, adapter("internal/crdt/conformance")),

	// Driving adapters: the engine and the ports, never a driven adapter
	// (the root chooses those), never each other, never the root.
	// The sync socket is a driving adapter like the API: the engine and
	// the ports, never a driven adapter, the API, or the root.
	"internal/syncserver": join([]string{"internal/api", "internal/spa", "internal/render", "internal/config", "cmd"}, adapters, without(drivers, "net/http")),
	// The MCP front door (docs/adr/0016): the engine and the ports, never
	// another front door, a driven adapter or the root; net/http for the
	// handler, and the MCP SDK, which no one else uses. The SDK's package
	// also holds its client, which can start a server as a process, so
	// os/exec comes with it; the adapter starts none.
	"internal/mcp": join([]string{"internal/api", "internal/spa", "internal/render", "internal/config", "internal/syncserver", "cmd"}, adapters, without(drivers, "net/http", "os/exec")),
	// The authorization server for agents is an adapter of the identity
	// port, outside the engine: it knows the engine only through the port
	// it declares, and no store, codec or other adapter.
	"internal/oauth":   join([]string{"internal/api", "internal/spa", "internal/render", "internal/config", "internal/mcp", "internal/syncserver", "internal/engine", "cmd"}, adapters, without(drivers, "net/http")),
	"internal/api":     join([]string{"internal/spa", "internal/config", "cmd"}, adapters, without(drivers, "net/http")),
	"internal/api/gen": join([]string{"internal", "pkg", "cmd"}, without(drivers, "net/http")),
	"internal/spa":     join([]string{"internal", "pkg", "cmd"}, without(drivers, "net/http")),
	// render reads through the engine and decodes through its codec; it
	// parses no syntax and touches no store.
	// Reporting is a port of its own, outside the core (docs/adr/0014):
	// the engine and the other ports never know it. The computed reporter
	// is a front door like render, reading through the engine; the
	// postgres reporter is a driven adapter over the store's database.
	"internal/reporting":             leaf,
	"internal/reporting/computed":    join([]string{"internal/api", "internal/spa", "internal/config", "cmd", "internal/reporting/postgres", "internal/reporting/conformance"}, without(adapters, "internal/reporting/"), drivers),
	"internal/reporting/postgres":    adapter("internal/reporting/postgres"),
	"internal/reporting/conformance": adapter("internal/reporting/conformance"),
	"internal/render":                join([]string{"internal/api", "internal/spa", "internal/config", "cmd"}, adapters, drivers),
	"internal/config":                leaf,

	// The client port's transports, and the reference driver. inproc
	// reads the actor through the identity port, which is HTTP
	// middleware, so net/http comes with it.
	"pkg/client/inproc":              join([]string{"internal/api", "internal/spa", "internal/render", "internal/config", "cmd", "pkg/client/remote", "pkg/uiconformance"}, without(adapters, "pkg/client/"), without(drivers, "net/http")),
	"pkg/client/remote":              join([]string{"internal", "pkg/client/inproc", "pkg/uiconformance", "cmd"}, without(drivers, "net/http")),
	"pkg/uiconformance/clientdriver": join([]string{"internal", "pkg/client/", "cmd"}, drivers),
}

func TestEveryPackageHasARule(t *testing.T) {
	module := strings.TrimSpace(run(t, "go", "list", "-m"))
	for _, full := range strings.Fields(run(t, "go", "list", "./...")) {
		pkg := strings.TrimPrefix(full, module+"/")
		if _, ok := ruleFor(pkg); !ok {
			t.Errorf("%s has no rule: add it to rules in internal/arch with the ring it belongs to (docs/ARCHITECTURE.md 3.1)", pkg)
		}
	}
}

func TestDependenciesPointInward(t *testing.T) {
	module := strings.TrimSpace(run(t, "go", "list", "-m"))
	for _, full := range strings.Fields(run(t, "go", "list", "./...")) {
		pkg := strings.TrimPrefix(full, module+"/")
		forbidden, _ := ruleFor(pkg)
		if len(forbidden) == 0 {
			continue
		}
		out := run(t, "go", "list", "-deps", "-json=ImportPath", full)
		dec := json.NewDecoder(strings.NewReader(out))
		for dec.More() {
			var p struct{ ImportPath string }
			if err := dec.Decode(&p); err != nil {
				t.Fatalf("%s: %v", pkg, err)
			}
			d := p.ImportPath
			if d == full {
				continue
			}
			// A module package is named relative to the module; anything
			// else (the standard library, a third party) by its import
			// path, so the standard library's own internal/ never reads as
			// this module's.
			name := d
			if strings.HasPrefix(d, module+"/") {
				name = strings.TrimPrefix(d, module+"/")
			} else if strings.HasPrefix(name, "internal/") || name == "internal" || name == "cmd" || strings.HasPrefix(name, "cmd/") {
				continue
			}
			for _, f := range forbidden {
				if covers(f, name) {
					t.Errorf("%s depends on %s, which the dependency rule forbids (%s)", pkg, name, f)
				}
			}
		}
	}
}

// ruleFor finds pkg's rule: its own, or the nearest "dir/" rule above it.
func ruleFor(pkg string) ([]string, bool) {
	if r, ok := rules[pkg]; ok {
		return r, true
	}
	best := ""
	for k := range rules {
		if strings.HasSuffix(k, "/") && strings.HasPrefix(pkg, k) && len(k) > len(best) {
			best = k
		}
	}
	if best == "" {
		return nil, false
	}
	return rules[best], true
}

// covers reports whether the forbidden name f covers the package name. A
// standard library name covers itself only: net/http's own internals are
// reported as net/http, and database/sql/driver is the handful of
// interfaces a value type implements to be stored, not a database.
func covers(f, name string) bool {
	if strings.HasSuffix(f, "/") {
		return strings.HasPrefix(name, f)
	}
	if standard(f) {
		return name == f
	}
	return name == f || strings.HasPrefix(name, f+"/")
}

// standard reports whether a forbidden name is a standard library
// package: not this module's, and no dot in its first element.
func standard(f string) bool {
	first := strings.SplitN(f, "/", 2)[0]
	return first != "internal" && first != "pkg" && first != "cmd" && !strings.Contains(first, ".")
}

func join(lists ...[]string) []string {
	var out []string
	for _, l := range lists {
		out = append(out, l...)
	}
	return out
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func without(list []string, drop ...string) []string {
	var out []string
	for _, s := range list {
		keep := true
		for _, d := range drop {
			if s == d {
				keep = false
			}
		}
		if keep {
			out = append(out, s)
		}
	}
	return out
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
