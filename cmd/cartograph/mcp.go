package main

import (
	"context"
	"fmt"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/trace"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/trace/jsonl"
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
	rec, closeTrace, err := mcpTrace(os.Getenv("CARTOGRAPH_MCP_TRACE"))
	if err != nil {
		return err
	}
	defer closeTrace()
	return mcp.ServeStdio(ctx, mcp.Options{Engine: c.Engine, Reports: c.Reports, Version: version, Trace: rec})
}

// mcpTrace is the trace recorder the configuration names: none when off
// or unset, else JSON lines appended to the file named (docs/adr/0028).
func mcpTrace(setting string) (trace.Recorder, func(), error) {
	if setting == "" || setting == "off" {
		return trace.Off{}, func() {}, nil
	}
	r, err := jsonl.Open(setting)
	if err != nil {
		return nil, nil, err
	}
	return r, func() { _ = r.Close() }, nil
}
