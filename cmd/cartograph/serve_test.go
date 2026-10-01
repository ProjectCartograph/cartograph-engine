package main

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/fxamacker/cbor/v2"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/auth"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/printer"
)

// The sync socket must work through exactly the stack serve runs: the
// request log, authentication, authorization, the router. A wrapper that
// cannot hand over the connection answers every upgrade with 501, which
// a test of the sync server alone does not see.
func TestSyncSocketThroughTheServeStack(t *testing.T) {
	dir := t.TempDir()
	if out, err := exec.Command("cp", "-r", "../../examples/minimal/.", dir).CombinedOutput(); err != nil {
		t.Fatalf("copy the example: %v %s", err, out)
	}
	ctx := context.Background()
	comp, err := compose(ctx, storeOptions{Target: dir, Codec: "yaml"})
	if err != nil {
		t.Fatal(err)
	}
	defer comp.Close()
	var ready atomic.Bool
	ready.Store(true)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mux, _ := routes(comp.Engine, comp.Fanout, auth.NoAuthentication{}, auth.AllowAll{}, printer.None{}, &ready)
	srv := httptest.NewServer(requestLog(logger, mux))
	defer srv.Close()

	dctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ws, resp, err := websocket.Dial(dctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/api/v1/sync", nil)
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("upgrade through the serve stack: %v (status %d)", err, status)
	}
	defer ws.Close(websocket.StatusNormalClosure, "")

	join, _ := cbor.Marshal(map[string]any{"type": "join", "senderId": "test-peer", "supportedProtocolVersions": []string{"1"}})
	if err := ws.Write(dctx, websocket.MessageBinary, join); err != nil {
		t.Fatal(err)
	}
	_, data, err := ws.Read(dctx)
	if err != nil {
		t.Fatal(err)
	}
	var peer map[string]any
	if err := cbor.Unmarshal(data, &peer); err != nil {
		t.Fatal(err)
	}
	if peer["type"] != "peer" || peer["targetId"] != "test-peer" || peer["selectedProtocolVersion"] != "1" {
		t.Errorf("handshake answered %v", peer)
	}
}
