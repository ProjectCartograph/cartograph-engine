package syncserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync/atomic"
	"time"

	"github.com/fxamacker/cbor/v2"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/fanout"
)

// agentColour is the colour agents are drawn in, the same on every
// screen, so people tell an agent from a colleague at a glance.
const agentColour = "#7c3aed"

var agentCount atomic.Uint64

// AnnounceAgent tells everyone on a document, on every replica, that an
// agent is working on it (docs/adr/0016). An agent over MCP holds no
// socket, so the server speaks for it: presence as a browser sends it
// (contract/schemas/presence.schema.json), relayed over the fan-out like
// any peer's. It lasts as presence does, ten seconds unless repeated, so
// an agent shows while it works and is gone when it stops.
func (s *Server) AnnounceAgent(ctx context.Context, docID, actor, name string) {
	sum := sha256.Sum256([]byte(actor))
	session := "agent-" + hex.EncodeToString(sum[:8])
	presence, err := cbor.Marshal(map[string]any{
		"v": 1, "session": session, "actor": actor, "name": name, "color": agentColour,
		"focus": nil, "caret": nil, "pointer": nil, "at": time.Now().UnixMilli(),
	})
	if err != nil {
		return
	}
	raw, err := cbor.Marshal(message{Type: "ephemeral", SenderID: session, DocumentID: docID, Data: presence,
		Count: agentCount.Add(1), SessionID: session})
	if err != nil {
		return
	}
	env, _ := json.Marshal(relayed{Replica: "agent", From: session, Data: raw})
	if len(env) <= fanout.MaxPayload {
		_ = s.fan.Publish(ctx, engine.EphemeralTopic(docID), env)
	}
}
