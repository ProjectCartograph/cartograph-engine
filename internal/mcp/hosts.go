package mcp

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// Rebinding protection, where it protects something (cartograph-engine
// #106). The SDK refuses any request that arrives on a loopback address
// with a Host that is not loopback: right for a local server nobody signs
// in to, which a web page in a browser on the same machine could reach by
// rebinding its own domain to 127.0.0.1; wrong behind a reverse proxy on
// the same machine, which forwards to 127.0.0.1 with the public Host
// kept, and whose public name may not be known until after deployment.
//
// So the SDK's check is off, and this one stands in its place only where
// it is asked for: where nobody signs in, or where a deployment lists its
// hosts. With sign-in, a rebinding page has no credential to send, and a
// proxy that authenticates is already the only way in.

// guardHosts wraps next with the check; hosts are the public host names a
// proxy may forward to Cartograph over loopback, a leading "*." matching
// any name under a domain.
func guardHosts(next http.Handler, hosts []string) http.Handler {
	exact := map[string]bool{}
	var under []string
	for _, h := range hosts {
		switch h = hostOnly(h); {
		case h == "":
		case strings.HasPrefix(h, "*."):
			under = append(under, h[1:])
		default:
			exact[h] = true
		}
	}
	ours := func(host string) bool {
		host = hostOnly(host)
		if exact[host] {
			return true
		}
		for _, suffix := range under {
			if strings.HasSuffix(host, suffix) && len(host) > len(suffix) {
				return true
			}
		}
		return false
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if local, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr); ok && local != nil &&
			isLoopback(local.String()) && !isLoopback(r.Host) && !ours(r.Host) {
			slog.Warn("mcp request refused: its Host is not one of Cartograph's names", "host", r.Host, "setting", "CARTOGRAPH_MCP_HOSTS")
			http.Error(w, fmt.Sprintf("Forbidden: invalid Host header %q: a proxy reaching Cartograph over loopback must send one of its public host names; list them in CARTOGRAPH_MCP_HOSTS", r.Host), http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// hostOnly is a Host header or address without its port, in lower case.
func hostOnly(h string) string {
	h = strings.TrimSpace(h)
	if host, _, err := net.SplitHostPort(h); err == nil {
		h = host
	}
	return strings.ToLower(strings.Trim(h, "[]"))
}

// isLoopback says whether an address or Host names this machine.
func isLoopback(addr string) bool {
	host := hostOnly(addr)
	if host == "localhost" {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.IsLoopback()
}
