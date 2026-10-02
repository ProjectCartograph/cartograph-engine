package syncserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
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

// KeepAgent announces an agent's step on a feed, as AnnounceAgent does,
// and keeps repeating it until the agent has been quiet for a while
// (WithLinger), so a person who opens Cartograph, or reloads it, while
// their agent waits on them still sees it and where it got to
// (docs/adr/0018). Each repeat is the same step, so nobody counts it
// twice. Only this replica repeats it, through the fan-out like the
// first; it is presence, held in memory, and a restart forgets it.
func (s *Server) KeepAgent(ctx context.Context, docID, actor, name string, agent map[string]any) {
	s.announce(ctx, docID, actor, name, "", agent)
	if s.lingerFor <= 0 {
		return
	}
	s.lingerMu.Lock()
	defer s.lingerMu.Unlock()
	if s.lingering == nil {
		s.lingering = map[string]*kept{}
	}
	s.lingering[docID+"\x00"+actor] = &kept{docID: docID, actor: actor, name: name, agent: agent, until: time.Now().Add(s.lingerFor)}
	if !s.lingerRunning {
		s.lingerRunning = true
		go s.repeatKept()
	}
}

// kept is the last step of one agent on one feed, and until when it is
// repeated.
type kept struct {
	docID, actor, name string
	agent              map[string]any
	until              time.Time
}

// repeatKept repeats every kept step at the presence heartbeat, and
// stops when none is left.
func (s *Server) repeatKept() {
	t := time.NewTicker(s.lingerEvery)
	defer t.Stop()
	for range t.C {
		now := time.Now()
		s.lingerMu.Lock()
		var due []*kept
		for k, st := range s.lingering {
			if now.After(st.until) {
				delete(s.lingering, k)
				continue
			}
			due = append(due, st)
		}
		if len(due) == 0 {
			s.lingerRunning = false
			s.lingerMu.Unlock()
			return
		}
		s.lingerMu.Unlock()
		for _, st := range due {
			ctx, cancel := context.WithTimeout(context.Background(), s.lingerEvery)
			s.announce(ctx, st.docID, st.actor, st.name, "", st.agent)
			cancel()
		}
	}
}

// lingerState is the steps KeepAgent repeats.
type lingerState struct {
	lingerMu      sync.Mutex
	lingering     map[string]*kept
	lingerRunning bool
	// lingerFor is how long after its last step an agent is still shown
	// on its person's feed; lingerEvery how often it is repeated.
	lingerFor, lingerEvery time.Duration
}

// WithLinger keeps an agent's last step on its person's feed for this
// long after the step, repeated every so often. 0 announces it once.
func WithLinger(lasts, every time.Duration) Option {
	return func(s *Server) { s.lingerFor, s.lingerEvery = lasts, every }
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
