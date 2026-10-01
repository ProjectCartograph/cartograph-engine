package syncserver_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/fxamacker/cbor/v2"
	"github.com/jackc/pgx/v5"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/auth"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/codec/yaml"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/crdt"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/crdt/automerge"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/fanout"
	fanoutmemory "github.com/ProjectCartograph/cartograph-engine/v2/internal/fanout/memory"
	fanoutpostgres "github.com/ProjectCartograph/cartograph-engine/v2/internal/fanout/postgres"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store/memory"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store/postgres"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/syncserver"
)

// msg mirrors the automerge-repo wire message, as a client writes it.
type msg struct {
	Type                      string   `cbor:"type"`
	SenderID                  string   `cbor:"senderId,omitempty"`
	TargetID                  string   `cbor:"targetId,omitempty"`
	DocumentID                string   `cbor:"documentId,omitempty"`
	Data                      []byte   `cbor:"data,omitempty"`
	SupportedProtocolVersions []string `cbor:"supportedProtocolVersions,omitempty"`
	SelectedProtocolVersion   string   `cbor:"selectedProtocolVersion,omitempty"`
	Count                     uint64   `cbor:"count,omitempty"`
	SessionID                 string   `cbor:"sessionId,omitempty"`
	Message                   string   `cbor:"message,omitempty"`
}

var am *automerge.Engine

func TestMain(m *testing.M) {
	var err error
	if am, err = automerge.New(2); err != nil {
		panic(err)
	}
	code := m.Run()
	am.Close()
	os.Exit(code)
}

// backend makes the ports of one replica. Every replica a backend makes
// shares its state with the others and nothing else.
type backend func(t *testing.T) (store.ManifestStore, store.OperationalStore, store.DocStore, fanout.Bus)

// inMemory shares memory adapters and one fan-out hub between replicas.
func inMemory(t *testing.T) backend {
	ms, ops, docs, hub := memory.NewManifestStore(), memory.NewOperationalStore(), memory.NewDocStore(), fanoutmemory.NewHub()
	return func(t *testing.T) (store.ManifestStore, store.OperationalStore, store.DocStore, fanout.Bus) {
		return ms, ops, docs, hub.Bus()
	}
}

// inPostgres gives each replica its own pool on one fresh schema and its
// own LISTEN connection: separate processes in all but the address space.
// It needs CARTOGRAPH_TEST_POSTGRES, which `just test-postgres` sets.
func inPostgres(t *testing.T) backend {
	base := os.Getenv("CARTOGRAPH_TEST_POSTGRES")
	if base == "" {
		t.Skip("CARTOGRAPH_TEST_POSTGRES is unset; `just test-postgres` runs this against a throwaway server")
	}
	ctx := context.Background()
	schema := "sync_" + strings.ToLower(engine.NewDocumentID())
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `CREATE SCHEMA "`+schema+`"`); err != nil {
		t.Fatal(err)
	}
	admin.Close(ctx)
	t.Cleanup(func() {
		if c, err := pgx.Connect(context.Background(), base); err == nil {
			_, _ = c.Exec(context.Background(), `DROP SCHEMA "`+schema+`" CASCADE`)
			c.Close(context.Background())
		}
	})
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return func(t *testing.T) (store.ManifestStore, store.OperationalStore, store.DocStore, fanout.Bus) {
		pool, err := postgres.Open(ctx, u.String())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(pool.Close)
		bus, err := fanoutpostgres.New(ctx, pool)
		if err != nil {
			t.Fatal(err)
		}
		return postgres.NewManifestStore(pool), postgres.NewOperationalStore(pool), postgres.NewDocStore(pool), bus
	}
}

// replicas are n engines over one backend: what n stateless processes
// over one database are.
func replicas(t *testing.T, n int, authz auth.Authorizer, ports backend) []*httptest.Server {
	t.Helper()
	var out []*httptest.Server
	for i := 0; i < n; i++ {
		ms, ops, docs, bus := ports(t)
		e, err := engine.New(ms, ops, engine.WithCodec(yaml.New()), engine.WithCRDT(am), engine.WithDocStore(docs), engine.WithFanout(bus))
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			team := []byte("apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  name: Field team\nspec:\n  purpose: Collect what the depots report\n")
			if err := e.PutWorking(context.Background(), "Team", "field-team", team); err != nil {
				t.Fatal(err)
			}
		}
		log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
		srv := httptest.NewServer(syncserver.New(e.Shared(), bus, authz, log))
		t.Cleanup(func() { srv.Close(); bus.Close() })
		out = append(out, srv)
		replicaEngines = append(replicaEngines, e)
	}
	return out
}

var replicaEngines []*engine.Engine

var backends = map[string]func(*testing.T) backend{"memory": inMemory, "postgres": inPostgres}

// peer is a client the way automerge-repo is one: a document, a sync
// state, and the protocol over a WebSocket.
type peer struct {
	t     *testing.T
	ws    *websocket.Conn
	id    string
	docID string
	doc   crdt.Doc
	st    crdt.SyncState
	eph   chan msg
	syncs int // sync messages received
}

func connect(t *testing.T, srv *httptest.Server, id, docID string) *peer {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := am.New()
	if err != nil {
		t.Fatal(err)
	}
	st, err := am.NewSyncState()
	if err != nil {
		t.Fatal(err)
	}
	p := &peer{t: t, ws: ws, id: id, docID: docID, doc: doc, st: st, eph: make(chan msg, 16)}
	t.Cleanup(func() { ws.Close(websocket.StatusNormalClosure, ""); doc.Close(); st.Close() })
	p.send(msg{Type: "join", SenderID: id, SupportedProtocolVersions: []string{"1"}})
	if m := p.read(); m.Type != "peer" || m.SelectedProtocolVersion != "1" || m.TargetID != id {
		t.Fatalf("handshake answered %+v", m)
	}
	data, _, err := doc.GenerateSyncMessage(st)
	if err != nil {
		t.Fatal(err)
	}
	p.send(msg{Type: "request", SenderID: id, TargetID: "server", DocumentID: docID, Data: data})
	return p
}

func (p *peer) send(m msg) {
	b, err := cbor.Marshal(m)
	if err != nil {
		p.t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.ws.Write(ctx, websocket.MessageBinary, b); err != nil {
		p.t.Fatal(err)
	}
}

func (p *peer) read() msg {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, b, err := p.ws.Read(ctx)
	if err != nil {
		p.t.Fatalf("%s: read: %v", p.id, err)
	}
	var m msg
	if err := cbor.Unmarshal(b, &m); err != nil {
		p.t.Fatal(err)
	}
	return m
}

// until runs the protocol until cond holds: answering every sync
// message, keeping ephemeral ones, and offering local changes.
func (p *peer) until(cond func() bool) {
	p.t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			j, _ := p.doc.JSON()
			p.t.Fatalf("%s: condition not met; document is %v", p.id, j)
		}
		p.offer()
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		_, b, err := p.ws.Read(ctx)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				continue
			}
			p.t.Fatalf("%s: read: %v", p.id, err)
		}
		var m msg
		if err := cbor.Unmarshal(b, &m); err != nil {
			p.t.Fatal(err)
		}
		switch m.Type {
		case "sync":
			p.syncs++
			if err := p.doc.ReceiveSyncMessage(p.st, m.Data); err != nil {
				p.t.Fatal(err)
			}
		case "ephemeral":
			p.eph <- m
		case "doc-unavailable", "error":
			p.t.Fatalf("%s: %+v", p.id, m)
		}
	}
}

// pump runs several peers in turn, as their own event loops would run
// at once, until cond holds.
func pump(t *testing.T, peers []*peer, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			for _, p := range peers {
				j, _ := p.doc.JSON()
				t.Logf("%s holds %v", p.id, j)
			}
			t.Fatal("peers did not converge")
		}
		for _, p := range peers {
			p.step(50 * time.Millisecond)
		}
	}
}

// step offers local changes and handles at most one message.
func (p *peer) step(wait time.Duration) {
	p.offer()
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	_, b, err := p.ws.Read(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		p.t.Fatalf("%s: read: %v", p.id, err)
	}
	var m msg
	if err := cbor.Unmarshal(b, &m); err != nil {
		p.t.Fatal(err)
	}
	switch m.Type {
	case "sync":
		p.syncs++
		if err := p.doc.ReceiveSyncMessage(p.st, m.Data); err != nil {
			p.t.Fatal(err)
		}
	case "ephemeral":
		p.eph <- m
	case "doc-unavailable", "error":
		p.t.Fatalf("%s: %+v", p.id, m)
	}
}

func (p *peer) offer() {
	data, ok, err := p.doc.GenerateSyncMessage(p.st)
	if err != nil {
		p.t.Fatal(err)
	}
	if ok {
		p.send(msg{Type: "sync", SenderID: p.id, TargetID: "server", DocumentID: p.docID, Data: data})
	}
}

func (p *peer) field(path ...string) any {
	j, err := p.doc.JSON()
	if err != nil {
		p.t.Fatal(err)
	}
	var v any = j
	for _, k := range path {
		m, _ := v.(map[string]any)
		v = m[k]
	}
	return v
}

// The point of the process model: two people connected to two different
// replicas edit one manifest and each sees the other's edit, and the
// working copy every other reader uses follows, with nothing kept in
// either replica that the other needs.
func TestTwoReplicasConverge(t *testing.T) {
	for name, b := range backends {
		t.Run(name, func(t *testing.T) { testTwoReplicasConverge(t, b(t)) })
	}
}

func testTwoReplicasConverge(t *testing.T, ports backend) {
	replicaEngines = nil
	srvs := replicas(t, 2, nil, ports)
	ctx := context.Background()
	docID, err := replicaEngines[1].Shared().DocumentFor(ctx, "Team", "field-team")
	if err != nil {
		t.Fatal(err)
	}

	a := connect(t, srvs[0], "peer-a", docID)
	b := connect(t, srvs[1], "peer-b", docID)
	a.until(func() bool { return a.field("spec", "purpose") == "Collect what the depots report" })
	b.until(func() bool { return b.field("spec", "purpose") == "Collect what the depots report" })

	// A rewrites the purpose on replica 0; B types into the name on
	// replica 1 at the same time.
	ja, _ := a.doc.JSON()
	ja["spec"].(map[string]any)["purpose"] = "Collect and check what the depots report"
	if _, err := a.doc.Reconcile(ja, replicaEngines[0].Shape("Team"), crdt.Change{Message: "a"}); err != nil {
		t.Fatal(err)
	}
	jb, _ := b.doc.JSON()
	jb["metadata"].(map[string]any)["name"] = "Field data team"
	if _, err := b.doc.Reconcile(jb, replicaEngines[1].Shape("Team"), crdt.Change{Message: "b"}); err != nil {
		t.Fatal(err)
	}

	both := func(p *peer) func() bool {
		return func() bool {
			return p.field("spec", "purpose") == "Collect and check what the depots report" && p.field("metadata", "name") == "Field data team"
		}
	}
	pump(t, []*peer{a, b}, func() bool { return both(a)() && both(b)() })

	text, found, err := replicaEngines[0].GetWorking(ctx, "Team", "field-team")
	if err != nil || !found {
		t.Fatalf("working copy: %v %v", found, err)
	}
	for _, want := range []string{"Collect and check what the depots report", "Field data team"} {
		if !strings.Contains(string(text), want) {
			t.Errorf("working copy lacks %q:\n%s", want, text)
		}
	}
}

// Presence reaches a peer on another replica, and the engine keeps none
// of it.
func TestPresenceCrossesReplicas(t *testing.T) {
	for name, b := range backends {
		t.Run(name, func(t *testing.T) { testPresenceCrossesReplicas(t, b(t)) })
	}
}

func testPresenceCrossesReplicas(t *testing.T, ports backend) {
	replicaEngines = nil
	srvs := replicas(t, 2, nil, ports)
	docID, err := replicaEngines[0].Shared().PresenceDocument(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	a := connect(t, srvs[0], "peer-a", docID)
	b := connect(t, srvs[1], "peer-b", docID)
	// Each has been answered once the server has sent its first sync
	// message, which it sends after joining the peer to the document.
	synced := func(p *peer) func() bool {
		return func() bool { return p.syncs > 0 }
	}
	a.until(synced(a))
	b.until(synced(b))

	payload := []byte(`{"v":1,"session":"tab-a-0001","actor":"analyst","at":1}`)
	a.send(msg{Type: "ephemeral", SenderID: "peer-a", TargetID: "server", DocumentID: docID, Count: 1, SessionID: "tab-a-0001", Data: payload})
	var got msg
	b.until(func() bool {
		select {
		case got = <-b.eph:
			return true
		default:
			return false
		}
	})
	if got.SenderID != "peer-a" || got.TargetID != "peer-b" || string(got.Data) != string(payload) || got.Count != 1 {
		t.Errorf("relayed %+v", got)
	}
}

// A principal who may only read can sync and read, and a change from
// them is refused rather than applied.
func TestReadersCannotWrite(t *testing.T) {
	for name, b := range backends {
		t.Run(name, func(t *testing.T) { testReadersCannotWrite(t, b(t)) })
	}
}

func testReadersCannotWrite(t *testing.T, ports backend) {
	replicaEngines = nil
	srvs := replicas(t, 1, readOnly{}, ports)
	ctx := context.Background()
	docID, err := replicaEngines[0].Shared().DocumentFor(ctx, "Team", "field-team")
	if err != nil {
		t.Fatal(err)
	}
	p := connect(t, srvs[0], "reader", docID)
	p.until(func() bool { return p.field("spec", "purpose") == "Collect what the depots report" })

	j, _ := p.doc.JSON()
	j["spec"].(map[string]any)["purpose"] = "Rewritten by a reader"
	if _, err := p.doc.Reconcile(j, replicaEngines[0].Shape("Team"), crdt.Change{}); err != nil {
		t.Fatal(err)
	}
	p.offer()
	for {
		m := p.read()
		if m.Type == "error" {
			break
		}
	}
	text, _, _ := replicaEngines[0].GetWorking(ctx, "Team", "field-team")
	if strings.Contains(string(text), "Rewritten by a reader") {
		t.Error("a reader's change reached the working copy")
	}
}

type readOnly struct{}

func (readOnly) Authorize(_ context.Context, _ auth.Principal, a auth.Action) error {
	if a.Verb == auth.VerbWrite {
		return auth.ErrForbidden
	}
	return nil
}

var _ http.Handler = (*syncserver.Server)(nil)
