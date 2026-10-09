package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/activity"
	activityjsonl "github.com/ProjectCartograph/cartograph-engine/v2/internal/activity/jsonl"
	activitypostgres "github.com/ProjectCartograph/cartograph-engine/v2/internal/activity/postgres"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/config"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/mcp"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/trace"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/trace/jsonl"
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

// peopleTrace is the recorder of people's acts the configuration names
// (docs/adr/0034), each act stamped with this build: none when off or
// unset; a table in the Postgres a postgres:// URL names, where every
// replica of a stateless deployment appends; else JSON lines appended to
// the file named.
func peopleTrace(ctx context.Context, setting string) (activity.Recorder, func(), error) {
	switch {
	case setting == "" || setting == "off":
		return activity.Off{}, func() {}, nil
	case config.IsPostgresURL(setting):
		s, closeStore, err := activityPostgres(ctx, setting)
		if err != nil {
			return nil, nil, err
		}
		return activity.Stamped(s, version), closeStore, nil
	}
	r, err := activityjsonl.Open(setting)
	if err != nil {
		return nil, nil, err
	}
	return activity.Stamped(r, version), func() { _ = r.Close() }, nil
}

// activityPostgres opens the people's trace in the Postgres url names,
// on a pool of its own.
func activityPostgres(ctx context.Context, url string) (*activitypostgres.Store, func(), error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, nil, fmt.Errorf("open %s: %w", config.Redact(url), err)
	}
	s, err := activitypostgres.Open(ctx, pool)
	if err != nil {
		pool.Close()
		return nil, nil, err
	}
	return s, pool.Close, nil
}
