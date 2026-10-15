package mcp_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	codecyaml "github.com/ProjectCartograph/cartograph-engine/v2/internal/codec/yaml"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/identity"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/mcp"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store/memory"
)

// A reverse proxy on the same machine forwards to Cartograph over
// loopback with the public Host kept; MCP answers it when that Host is
// one of Cartograph's own names, and refuses any other, against DNS
// rebinding (cartograph-engine#106: every call was refused behind a
// proxy on Azure).
func TestMCPBehindAProxyOnLoopback(t *testing.T) {
	t.Parallel()
	e, err := engine.New(memory.NewManifestStore(), memory.NewOperationalStore(), engine.WithCodec(codecyaml.New()))
	if err != nil {
		t.Fatal(err)
	}
	checked := mcp.Handler(mcp.Options{Engine: e, Version: "test", HostCheck: true, Hosts: []string{"atlas.example.org", "*.apps.example.net"}})
	// With sign-in the check is off: a deployment whose name is known only
	// after it is deployed works behind its proxy with nothing set.
	signedIn := mcp.Handler(mcp.Options{Engine: e, Version: "test"})
	initialize := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`
	call := func(h http.Handler, local, host string) int {
		r := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(initialize))
		r.Host = host
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/event-stream")
		ctx := identity.WithPrincipal(context.Background(), identity.Principal{Subject: "ada", Email: "ada@example.org"})
		ctx = context.WithValue(ctx, http.LocalAddrContextKey, &net.TCPAddr{IP: net.ParseIP(local), Port: 8080})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r.WithContext(ctx))
		return w.Code
	}
	for _, c := range []struct {
		local, host string
		refused     bool
	}{
		{"127.0.0.1", "atlas.example.org", false},     // the proxy, with the public name
		{"127.0.0.1", "ATLAS.example.org:443", false}, // the same name, cased and with a port
		{"127.0.0.1", "localhost:8080", false},        // a client on this machine
		{"::1", "[::1]:8080", false},
		{"127.0.0.1", "evil.example.com", true},         // a page rebinding its own name to 127.0.0.1
		{"10.0.0.4", "evil.example.com", false},         // not loopback: the check does not apply
		{"127.0.0.1", "slot-2.apps.example.net", false}, // a name under a listed domain
		{"127.0.0.1", "apps.example.net", true},         // the domain itself is not under it
		{"127.0.0.1", "evilapps.example.net", true},
	} {
		got := call(checked, c.local, c.host)
		if refused := got == http.StatusForbidden; refused != c.refused {
			t.Errorf("local %s, Host %s: status %d, refused %v, want refused %v", c.local, c.host, got, refused, c.refused)
		}
	}
	if got := call(signedIn, "127.0.0.1", "cartograph.example.net"); got == http.StatusForbidden {
		t.Errorf("with sign-in, a proxy over loopback was refused")
	}
}
