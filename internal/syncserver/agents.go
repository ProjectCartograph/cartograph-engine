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
//
// focus is the field it last changed, so people see where; agent is what
// it did (the presence schema's agent), so a person can follow it.
func (s *Server) AnnounceAgent(ctx context.Context, docID, actor, name, focus string, agent map[string]any) {
	s.announce(ctx, docID, actor, name, focus, agent)
}

func (s *Server) announce(ctx context.Context, docID, actor, name, focus string, agent map[string]any) {
	sum := sha256.Sum256([]byte(actor))
	session := "agent-" + hex.EncodeToString(sum[:8])
	p := map[string]any{
		"v": 1, "session": session, "actor": actor, "name": name, "color": agentColour,
		// A float, not an int64: CBOR's eight-byte integers decode as BigInt
		// in a browser, which no presence check accepts. Milliseconds are
		// exact in a float for some 280,000 years.
		"focus": nil, "caret": nil, "pointer": nil, "at": float64(time.Now().UnixMilli()),
	}
	if focus != "" {
		p["focus"] = map[string]any{"path": focus}
	}
	if agent != nil {
		p["agent"] = agent
	}
	presence, err := cbor.Marshal(p)
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
