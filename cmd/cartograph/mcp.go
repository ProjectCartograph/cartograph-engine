package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/mcp"
)

// runMCP is `cartograph mcp`: MCP over stdin and stdout, for an agent on
// this machine (docs/adr/0016). It runs as the operator, as the command
// line does, and the agent ceiling holds: the agent reads, drafts and
// proposes, and the operator accepts in the interface.
func runMCP(args []string) error {
	positional, _ := splitPositional(args, 1)
	if len(positional) != 1 {
		return fmt.Errorf("usage: cartograph mcp <vault, file or postgres:// URL>")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	c, err := compose(ctx, storeOptions{Target: positional[0], Codec: envCodec(), Fanout: "memory", Reports: os.Getenv("CARTOGRAPH_REPORTS")})
	if err != nil {
		return err
	}
	defer c.Close()
	return mcp.ServeStdio(ctx, mcp.Options{Engine: c.Engine, Reports: c.Reports, Version: version})
}
