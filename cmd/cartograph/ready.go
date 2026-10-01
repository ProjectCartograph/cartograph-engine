package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/config"
)

// runReady asks a running server whether it is ready and exits 0 or 1.
// It exists for container health checks: the distroless image has no
// shell, no curl and no wget, so the binary checks itself. The address
// comes from CARTOGRAPH_ADDR like the server's own, or from the one argument.
func runReady(args []string) error {
	addr := ""
	if len(args) > 0 {
		addr = args[0]
	} else {
		cfg, err := config.FromEnv(os.Getenv)
		if err != nil {
			return err
		}
		addr = cfg.ListenAddr()
	}
	url := addr
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return fmt.Errorf("address %q: %w", addr, err)
		}
		if host == "" || host == "0.0.0.0" || host == "::" {
			host = "127.0.0.1"
		}
		url = "http://" + net.JoinHostPort(host, port)
	}
	url = strings.TrimSuffix(url, "/") + "/readyz"

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("not ready: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("not ready: %s answered %d", url, resp.StatusCode)
	}
	fmt.Println("ready")
	return nil
}
