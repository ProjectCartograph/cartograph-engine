// Package syncserver is the sync socket (GET /api/v1/sync): the
// automerge-repo network protocol, version 1, over a WebSocket, so the
// stock @automerge/automerge-repo WebSocket adapter connects to the engine
// unchanged (docs/adr/0007). It is a driving adapter: it turns protocol
// messages into calls on the engine's shared-draft service and nothing
// more, and it keeps no state a later connection needs.
//
// One connection is one peer (a browser tab, a terminal). Per document it
// holds an Automerge sync state for as long as the connection lives;
// across replicas it learns of changes through the fan-out port, and,
// because a fan-out hint may be lost, it also asks every peer it serves
// for heads on a timer. A lost hint costs latency, never an edit
// (docs/adr/0008).
package syncserver

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/fxamacker/cbor/v2"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/auth"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/crdt"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/fanout"
)

// protocolVersion is the only automerge-repo protocol version there is.
const protocolVersion = "1"

// defaultRecheck is how often a connection offers each of its documents
// to its peer again, so a change whose fan-out hint was lost still
// arrives.
const defaultRecheck = 15 * time.Second

// maxMessage bounds one inbound message. A sync message carrying a large
// document's whole history can be big; anything beyond this is refused.
const maxMessage = 32 << 20

// message is every automerge-repo message, as one CBOR map. Fields a
// message type does not use are omitted.
type message struct {
	Type                      string        `cbor:"type"`
	SenderID                  string        `cbor:"senderId,omitempty"`
	TargetID                  string        `cbor:"targetId,omitempty"`
	DocumentID                string        `cbor:"documentId,omitempty"`
	Data                      []byte        `cbor:"data,omitempty"`
	SupportedProtocolVersions []string      `cbor:"supportedProtocolVersions,omitempty"`
	SelectedProtocolVersion   string        `cbor:"selectedProtocolVersion,omitempty"`
	PeerMetadata              *peerMetadata `cbor:"peerMetadata,omitempty"`
	Metadata                  *peerMetadata `cbor:"metadata,omitempty"`
	Count                     uint64        `cbor:"count,omitempty"`
	SessionID                 string        `cbor:"sessionId,omitempty"`
	Message                   string        `cbor:"message,omitempty"`
}

type peerMetadata struct {
	StorageID   string `cbor:"storageId,omitempty"`
	IsEphemeral bool   `cbor:"isEphemeral"`
}

// relayed is an ephemeral message as it crosses the fan-out: the bytes as
// they arrived, and the replica they arrived at, so that replica does not
// deliver them twice.
type relayed struct {
	Replica string `json:"r"`
	From    string `json:"f"`
	Data    []byte `json:"d"`
}

// Server serves the sync socket.
type Server struct {
	shared *engine.Shared
	fan    fanout.Bus
	authz  auth.Authorizer
	// follows says whether the principal on ctx may follow a person's
	// agents (their own, or an administrator); nil, nobody may.
	follows func(ctx context.Context, person string) bool
	log     *slog.Logger
	peerID  string

	ping time.Duration
	// recheck bounds what a lost fan-out hint costs (WithRecheck).
	recheck time.Duration
	// idle closes a connection that changed nothing for this long
	// (WithIdle); 0 never does.
	idle  time.Duration
	mu    sync.Mutex
	rooms map[string]*room // by document id
	conns map[*conn]struct{}

	received, sent, refused, idleClosed atomic.Int64

	lingerState
}

// Option configures a Server.
type Option func(*Server)

// WithPing pings each peer at this interval, so a quiet connection is
// not closed by a load balancer's idle timeout (60 s by default behind
// nginx or an AWS ALB) and a dead one is noticed. 0 turns pings off.
func WithPing(d time.Duration) Option { return func(s *Server) { s.ping = d } }

// WithIdle closes a connection that has changed no document for d, so an
// open but unattended window holds no replica up and a deployment can
// scale to zero (docs/adr/0015). Pings, the server's own rechecks and
// presence do not count: the server cannot tell a person from a
// heartbeat, and a change to a document is the one thing only a person
// makes. An interface reconnects when its person comes back. 0 never
// closes.
func WithIdle(d time.Duration) Option { return func(s *Server) { s.idle = d } }

// StatusIdle is the close code for a connection closed as idle, so an
// interface can tell it from a failure and wait for its person instead
// of reconnecting at once.
const StatusIdle = websocket.StatusCode(4000)

// WithRecheck sets how often a connection offers its documents again
// whether or not a hint arrived: the most a lost hint can delay a change.
func WithRecheck(d time.Duration) Option { return func(s *Server) { s.recheck = d } }

// WithFollows says who may open a person's agent feed (docs/adr/0018):
// the engine's MayFollow.
func WithFollows(f func(ctx context.Context, person string) bool) Option {
	return func(s *Server) { s.follows = f }
}

// New returns a server over the engine's shared drafts. authz decides,
// per document, whether a peer may read or write it.
func New(shared *engine.Shared, fan fanout.Bus, authz auth.Authorizer, log *slog.Logger, opts ...Option) *Server {
	if authz == nil {
		authz = auth.AllowAll{}
	}
	if log == nil {
		log = slog.Default()
	}
	s := &Server{shared: shared, fan: fan, authz: authz, log: log, peerID: "cartograph-" + shared.Replica(),
		ping: 20 * time.Second, recheck: defaultRecheck, rooms: map[string]*room{}, conns: map[*conn]struct{}{}}
	// An agent waiting on its person for up to ten minutes still shows,
	// repeated at the three seconds presence repeats at.
	s.lingerFor, s.lingerEvery = 10*time.Minute, 3*time.Second
	for _, o := range opts {
		o(s)
	}
	return s
}

// Stats are counters for monitoring.
type Stats struct {
	IdleClosed  int64 // connections closed for changing nothing (WithIdle)
	Connections int   // peers connected to this replica
	Documents   int   // documents at least one of them has open
	Received    int64 // sync messages received
	Sent        int64 // sync messages sent
	Refused     int64 // messages refused: a read-only peer's change, a protocol error
}

// Stats returns the current counters.
func (s *Server) Stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Stats{Connections: len(s.conns), Documents: len(s.rooms), Received: s.received.Load(), Sent: s.sent.Load(), Refused: s.refused.Load(), IdleClosed: s.idleClosed.Load()}
}

// Shutdown closes every connection with "going away", so each peer
// reconnects at once (through the load balancer, to a replica that is
// staying) instead of finding out from a dead socket. Nothing is lost:
// a peer keeps its changes and offers them again on reconnecting.
func (s *Server) Shutdown() {
	s.mu.Lock()
	conns := make([]*conn, 0, len(s.conns))
	for c := range s.conns {
		conns = append(conns, c)
	}
	s.mu.Unlock()
	// Each close waits for the peer's half of the closing handshake, so
	// they run together: a replica with many peers must not spend its
	// whole grace period closing them one by one. A peer that does not
	// answer in time is cut off; it reconnects all the same.
	var wg sync.WaitGroup
	for _, c := range conns {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = c.ws.Close(websocket.StatusGoingAway, "server restarting")
		}()
	}
	// The close frames go out at once; waiting longer than this for
	// every peer's reply would only delay the rest of the shutdown, and
	// the closes finish in the background either way.
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(closeWait):
	}
}

// closeWait bounds how long Shutdown waits for any one peer to finish the
// closing handshake.
const closeWait = time.Second

// ServeHTTP upgrades the request and runs one peer until it leaves.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// The interface is served by this same origin; a proxy in front
		// may rewrite the host, which the origin check would refuse.
		InsecureSkipVerify: true,
	})
	if err != nil {
		return // Accept has answered the request already
	}
	ws.SetReadLimit(maxMessage)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	c := &conn{s: s, ws: ws, principal: auth.PrincipalFrom(r.Context()), docs: map[string]*peerDoc{}}
	s.mu.Lock()
	s.conns[c] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.conns, c)
		s.mu.Unlock()
	}()
	if s.ping > 0 {
		go c.keepalive(ctx, cancel)
	}
	c.active.Store(time.Now().UnixNano())
	if s.idle > 0 {
		go c.watchIdle(ctx, cancel)
	}
	err = c.run(ctx)
	c.leaveAll()
	switch {
	case c.idle.Load():
		s.idleClosed.Add(1) // closed with its code already
	case err == nil, errors.Is(err, context.Canceled), websocket.CloseStatus(err) != -1:
		_ = ws.Close(websocket.StatusNormalClosure, "")
	default:
		s.log.Debug("sync connection ended", "err", err)
		_ = ws.Close(websocket.StatusInternalError, "")
	}
}

// room is one document's peers on this replica, and the fan-out
// subscriptions that wake them.
type room struct {
	docID  string
	peers  map[*conn]struct{}
	cancel context.CancelFunc
}

func (s *Server) join(c *conn, docID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rm, ok := s.rooms[docID]
	if !ok {
		ctx, cancel := context.WithCancel(context.Background())
		rm = &room{docID: docID, peers: map[*conn]struct{}{}, cancel: cancel}
		s.rooms[docID] = rm
		// Subscribe before the peer is answered, so nothing published
		// after it joined can pass this replica by.
		changed, err := s.fan.Subscribe(ctx, engine.DocTopic(docID))
		if err != nil {
			s.log.Warn("fan-out subscribe failed; peers fall back to the periodic check", "doc", docID, "err", err)
		}
		eph, err := s.fan.Subscribe(ctx, engine.EphemeralTopic(docID))
		if err != nil {
			s.log.Warn("fan-out subscribe failed; presence stays on this replica", "doc", docID, "err", err)
		}
		go s.watch(ctx, docID, changed, eph)
	}
	rm.peers[c] = struct{}{}
}

func (s *Server) leave(c *conn, docID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rm, ok := s.rooms[docID]
	if !ok {
		return
	}
	delete(rm.peers, c)
	if len(rm.peers) == 0 {
		rm.cancel()
		delete(s.rooms, docID)
		s.shared.Forget(docID)
	}
}

func (s *Server) peers(docID string, except *conn) []*conn {
	s.mu.Lock()
	defer s.mu.Unlock()
	rm := s.rooms[docID]
	if rm == nil {
		return nil
	}
	out := make([]*conn, 0, len(rm.peers))
	for c := range rm.peers {
		if c != except {
			out = append(out, c)
		}
	}
	return out
}

// watch listens for a document's hints and presence from every replica
// while anyone on this replica has it open.
func (s *Server) watch(ctx context.Context, docID string, changed, eph fanout.Subscription) {
	var changedC, ephC <-chan []byte
	if changed != nil {
		defer changed.Close()
		changedC = changed.C()
	}
	if eph != nil {
		defer eph.Close()
		ephC = eph.C()
	}
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-changedC:
			if !ok {
				return
			}
			for _, c := range s.peers(docID, nil) {
				c.offer(ctx, docID)
			}
		case data, ok := <-ephC:
			if !ok {
				return
			}
			var m relayed
			if json.Unmarshal(data, &m) != nil || m.Replica == s.shared.Replica() {
				continue // ours: delivered locally when it arrived
			}
			for _, c := range s.peers(docID, nil) {
				c.forward(ctx, docID, m.Data)
			}
		}
	}
}

// conn is one peer.
type conn struct {
	s         *Server
	ws        *websocket.Conn
	principal auth.Principal
	remote    string // the peer's id, from its join

	wmu  sync.Mutex // one writer at a time
	mu   sync.Mutex
	docs map[string]*peerDoc

	// active is when the peer last changed a document, or joined, in
	// Unix nanoseconds; idle is set when the connection was closed for
	// it.
	active atomic.Int64
	idle   atomic.Bool
}

type peerDoc struct {
	mu       sync.Mutex
	state    crdt.SyncState
	canWrite bool
}

// keepalive pings the peer until ctx ends; a peer that does not answer
// within the interval is gone, and its connection is ended.
func (c *conn) keepalive(ctx context.Context, end context.CancelFunc) {
	t := time.NewTicker(c.s.ping)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			pctx, cancel := context.WithTimeout(ctx, c.s.ping)
			err := c.ws.Ping(pctx)
			cancel()
			if err != nil && ctx.Err() == nil {
				end()
				return
			}
		}
	}
}

// watchIdle ends the connection once it has changed no document for the
// server's idle time.
func (c *conn) watchIdle(ctx context.Context, end context.CancelFunc) {
	t := time.NewTicker(max(c.s.idle/8, 10*time.Millisecond))
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			if now.Sub(time.Unix(0, c.active.Load())) >= c.s.idle {
				// Close with the code, not by ending ctx: an ended read
				// context drops the connection without a close frame
				// (coder/websocket), and the peer would see a failure.
				c.idle.Store(true)
				_ = c.ws.Close(StatusIdle, "idle")
				end()
				return
			}
		}
	}
}

func (c *conn) run(ctx context.Context) error {
	first, err := c.read(ctx)
	if err != nil {
		return err
	}
	if first.Type != "join" {
		return c.fail(ctx, "expected join")
	}
	if !contains(first.SupportedProtocolVersions, protocolVersion) {
		return c.fail(ctx, "unsupported protocol version")
	}
	c.remote = first.SenderID
	if err := c.write(ctx, message{
		Type: "peer", SenderID: c.s.peerID, TargetID: c.remote,
		SelectedProtocolVersion: protocolVersion,
		PeerMetadata:            &peerMetadata{IsEphemeral: false},
	}); err != nil {
		return err
	}

	tick := time.NewTicker(c.s.recheck)
	defer tick.Stop()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				c.mu.Lock()
				ids := make([]string, 0, len(c.docs))
				for id := range c.docs {
					ids = append(ids, id)
				}
				c.mu.Unlock()
				for _, id := range ids {
					c.offer(ctx, id)
				}
			}
		}
	}()

	for {
		m, err := c.read(ctx)
		if err != nil {
			return err
		}
		switch m.Type {
		case "request", "sync":
			if err := c.sync(ctx, m); err != nil {
				return err
			}
		case "ephemeral":
			c.ephemeral(ctx, m)
		case "leave":
			return nil
		case "remote-subscription-change", "remote-heads-changed":
			// Heads gossip between storage peers; the engine is the store
			// every peer syncs with, so it has nothing to relay.
		default:
			// An unknown type from a newer peer is ignored, not fatal.
		}
	}
}

// sync handles a request or sync message for one document.
func (c *conn) sync(ctx context.Context, m message) error {
	pd, err := c.open(ctx, m.DocumentID)
	if errors.Is(err, errUnavailable) {
		return c.write(ctx, message{Type: "doc-unavailable", SenderID: c.s.peerID, TargetID: c.remote, DocumentID: m.DocumentID})
	}
	if err != nil {
		return err
	}
	c.s.received.Add(1)
	pd.mu.Lock()
	changed, err := c.s.shared.Receive(ctx, m.DocumentID, pd.state, m.Data, pd.canWrite)
	pd.mu.Unlock()
	if errors.Is(err, engine.ErrReadOnly) {
		c.s.refused.Add(1)
		return c.fail(ctx, "this principal may not change "+m.DocumentID)
	}
	if err != nil {
		return err
	}
	c.offer(ctx, m.DocumentID)
	if changed {
		c.active.Store(time.Now().UnixNano())
		// Peers on this replica hear at once; the fan-out hint reaches
		// the rest, and this replica too, where it is a no-op.
		for _, p := range c.s.peers(m.DocumentID, c) {
			p.offer(ctx, m.DocumentID)
		}
	}
	return nil
}

var errUnavailable = errors.New("document unavailable")

// open returns the peer's state for a document, starting it on first
// use after checking the principal may read it.
func (c *conn) open(ctx context.Context, docID string) (*peerDoc, error) {
	c.mu.Lock()
	pd, ok := c.docs[docID]
	c.mu.Unlock()
	if ok {
		return pd, nil
	}
	kind, id, err := c.s.shared.ManifestFor(ctx, docID)
	if err != nil {
		return nil, errUnavailable
	}
	read := auth.Action{Verb: auth.VerbRead, Kind: kind, ID: id}
	write := auth.Action{Verb: auth.VerbWrite, Kind: kind, ID: id}
	if engine.IsPresence(kind, id) {
		read, write = auth.Action{Verb: auth.VerbRead}, auth.Action{Verb: auth.VerbRead}
	}
	if err := c.s.authz.Authorize(ctx, c.principal, read); err != nil {
		return nil, errUnavailable // not knowing and not being allowed look the same
	}
	// A person's agent feed is theirs (docs/adr/0018): what their agents
	// do, on manifests others may not see, is not broadcast.
	if owner, ok := engine.AgentFeedOwner(kind, id); ok {
		if c.s.follows == nil || !c.s.follows(auth.WithPrincipal(ctx, c.principal), owner) {
			return nil, errUnavailable
		}
		write = auth.Action{Verb: auth.VerbRead}
	}
	st, err := c.s.shared.NewSyncState()
	if err != nil {
		return nil, err
	}
	pd = &peerDoc{state: st, canWrite: c.s.authz.Authorize(ctx, c.principal, write) == nil}
	c.mu.Lock()
	c.docs[docID] = pd
	c.mu.Unlock()
	c.s.join(c, docID)
	return pd, nil
}

// offer sends the peer whatever it lacks of a document, if anything.
func (c *conn) offer(ctx context.Context, docID string) {
	c.mu.Lock()
	pd, ok := c.docs[docID]
	c.mu.Unlock()
	if !ok {
		return
	}
	pd.mu.Lock()
	msg, ok, err := c.s.shared.Generate(ctx, docID, pd.state)
	pd.mu.Unlock()
	if err != nil || !ok {
		return
	}
	if c.write(ctx, message{Type: "sync", SenderID: c.s.peerID, TargetID: c.remote, DocumentID: docID, Data: msg}) == nil {
		c.s.sent.Add(1)
	}
}

// ephemeral relays a peer's presence to the document's other peers, here
// and on every other replica. The engine reads none of it.
func (c *conn) ephemeral(ctx context.Context, m message) {
	if _, err := c.open(ctx, m.DocumentID); err != nil {
		return
	}
	raw, err := cbor.Marshal(m)
	if err != nil {
		return
	}
	for _, p := range c.s.peers(m.DocumentID, c) {
		p.forward(ctx, m.DocumentID, raw)
	}
	env, _ := json.Marshal(relayed{Replica: c.s.shared.Replica(), From: c.remote, Data: raw})
	if len(env) <= fanout.MaxPayload {
		_ = c.s.fan.Publish(ctx, engine.EphemeralTopic(m.DocumentID), env)
	}
}

// forward delivers an ephemeral message, addressed to this peer.
func (c *conn) forward(ctx context.Context, docID string, raw []byte) {
	var m message
	if cbor.Unmarshal(raw, &m) != nil || m.SenderID == c.remote {
		return
	}
	m.TargetID = c.remote
	_ = c.write(ctx, m)
}

func (c *conn) leaveAll() {
	c.mu.Lock()
	docs := c.docs
	c.docs = map[string]*peerDoc{}
	c.mu.Unlock()
	for id, pd := range docs {
		c.s.leave(c, id)
		_ = pd.state.Close()
	}
}

func (c *conn) read(ctx context.Context) (message, error) {
	typ, data, err := c.ws.Read(ctx)
	if err != nil {
		return message{}, err
	}
	if typ != websocket.MessageBinary {
		return message{}, errors.New("text frame on the sync socket")
	}
	var m message
	if err := cbor.Unmarshal(data, &m); err != nil {
		return message{}, err
	}
	return m, nil
}

func (c *conn) write(ctx context.Context, m message) error {
	data, err := cbor.Marshal(m)
	if err != nil {
		return err
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return c.ws.Write(wctx, websocket.MessageBinary, data)
}

func (c *conn) fail(ctx context.Context, why string) error {
	_ = c.write(ctx, message{Type: "error", SenderID: c.s.peerID, TargetID: c.remote, Message: why})
	return errors.New(why)
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
