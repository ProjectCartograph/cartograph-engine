package engine

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"sync/atomic"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/crdt"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/fanout"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/kinds"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

// ErrReadOnly is returned when a sync message from a principal who may
// not write carries changes.
var ErrReadOnly = errors.New("engine: the principal may not change this document")

// ErrNoShared is returned by Shared methods when the engine was built
// without the three ports shared drafts need.
var ErrNoShared = errors.New("engine: shared drafts are not configured")

// presenceKind and presenceID name the one document that carries
// presence for screens not about a single manifest. It holds no content
// and nobody may change it.
const (
	presenceKind = ""
	presenceID   = "presence"
)

// compactEvery is how many chunks a document collects before a replica
// folds them into a new snapshot. Loading costs one call per chunk, so
// this bounds the cost of opening a busy document.
const compactEvery = 128

// WithCRDT, WithDocStore and WithFanout give the engine what shared
// drafts need (docs/adr/0007, 0008). All three, or shared drafts are off.
func WithCRDT(c crdt.Engine) Option        { return func(e *Engine) { e.crdt = c } }
func WithDocStore(d store.DocStore) Option { return func(e *Engine) { e.docs = d } }
func WithFanout(f fanout.Bus) Option       { return func(e *Engine) { e.fan = f } }

// DocTopic is the fan-out topic a document's "changed" hints go to, and
// EphemeralTopic the one its presence travels on.
func DocTopic(docID string) string       { return "doc/" + docID }
func EphemeralTopic(docID string) string { return "eph/" + docID }

// Changed is the hint published on DocTopic after a replica stores a
// change: which replica, and the chunk it stored.
type Changed struct {
	Replica string `json:"replica"`
	Seq     int64  `json:"seq"`
}

// Shared is the shared draft of every manifest: one CRDT document each,
// held in the DocStore, edited by every interface over the sync protocol,
// and materialised into the working copy so everything that reads working
// copies keeps working.
//
// It is stateless in the sense the process model needs. The documents it
// holds are a cache: before every use it folds in whatever chunks other
// replicas have stored since, so a request served by any replica sees
// the same document, and a replica that starts empty, or is killed,
// loses nothing.
type Shared struct {
	e       *Engine
	crdt    crdt.Engine
	docs    store.DocStore
	fan     fanout.Bus
	replica string

	mu   sync.Mutex
	open map[string]*sharedDoc

	stored, compactions, storeErrors atomic.Int64
}

type sharedDoc struct {
	mu       sync.Mutex
	kind, id string
	doc      crdt.Doc
	seq      int64 // the last chunk folded in
	chunks   int   // chunks stored since the snapshot this replica last saw
}

// SharedStats are counters for monitoring.
type SharedStats struct {
	Cached      int   // documents in this replica's cache
	Stored      int64 // changes appended to the store by this replica
	Compactions int64 // snapshots this replica wrote
	StoreErrors int64 // appends that failed
}

// Stats returns the current counters.
func (s *Shared) Stats() SharedStats {
	s.mu.Lock()
	n := len(s.open)
	s.mu.Unlock()
	return SharedStats{Cached: n, Stored: s.stored.Load(), Compactions: s.compactions.Load(), StoreErrors: s.storeErrors.Load()}
}

// Shared returns the shared-draft service, or nil when the engine has no
// CRDT, document store or fan-out.
func (e *Engine) Shared() *Shared { return e.shared }

func (e *Engine) initShared() {
	if e.crdt == nil || e.docs == nil || e.fan == nil {
		return
	}
	e.shared = &Shared{e: e, crdt: e.crdt, docs: e.docs, fan: e.fan, replica: randomHex(8), open: map[string]*sharedDoc{}}
}

// Replica is this process's id on the fan-out, so it can tell its own
// hints from others'.
func (s *Shared) Replica() string { return s.replica }

// DocumentFor returns the id of a manifest's shared draft, creating it
// from the working copy or the current version on first use. When the
// working copy has changed outside the draft (a file edited in a vault, an
// import), the change is folded in first, so the draft never shows less
// than the working copy does.
func (s *Shared) DocumentFor(ctx context.Context, kind, id string) (string, error) {
	if _, ok := kinds.ByName(kind); !ok {
		return "", fmt.Errorf("%w: %s", ErrUnknownKind, kind)
	}
	content, found, err := s.content(ctx, kind, id)
	if err != nil {
		return "", err
	}
	docID, err := s.docs.DocumentFor(ctx, kind, id)
	switch {
	case err == nil:
		if found {
			if _, err := s.reconcile(ctx, docID, content, crdt.Change{Message: "working copy changed outside the shared draft"}); err != nil {
				return "", err
			}
		}
		return docID, nil
	case !errors.Is(err, store.ErrNoDocument):
		return "", err
	case !found:
		return "", fmt.Errorf("%w: %s/%s", ErrNotFound, kind, id)
	}
	return s.create(ctx, kind, id, content, s.e.Shape(kind))
}

// PresenceDocument returns the id of the document that routes presence
// on screens not about one manifest.
func (s *Shared) PresenceDocument(ctx context.Context) (string, error) {
	docID, err := s.docs.DocumentFor(ctx, presenceKind, presenceID)
	if err == nil {
		return docID, nil
	}
	if !errors.Is(err, store.ErrNoDocument) {
		return "", err
	}
	return s.create(ctx, presenceKind, presenceID, map[string]any{}, crdt.Shape{})
}

// create makes a document from content and stores it, unless another
// replica stored one first, in which case that one stands.
func (s *Shared) create(ctx context.Context, kind, id string, content map[string]any, shape crdt.Shape) (string, error) {
	doc, err := s.crdt.New()
	if err != nil {
		return "", err
	}
	defer doc.Close()
	if _, err := doc.Reconcile(content, shape, crdt.Change{Message: "genesis"}); err != nil {
		return "", err
	}
	snapshot, err := doc.Save()
	if err != nil {
		return "", err
	}
	existing, created, err := s.docs.Create(ctx, kind, id, NewDocumentID(), snapshot)
	if err != nil {
		return "", err
	}
	_ = created // either way, existing names the document that stands
	return existing, nil
}

// content is what a manifest's draft should hold: its working copy, or
// its current version when there is none.
func (s *Shared) content(ctx context.Context, kind, id string) (map[string]any, bool, error) {
	if text, found, err := s.e.manifests.GetWorking(ctx, kind, id); err != nil {
		return nil, false, err
	} else if found {
		doc, err := s.e.codec.Decode(text)
		return doc, err == nil, err
	}
	v, found, err := s.e.manifests.GetCurrent(ctx, kind, id)
	if err != nil || !found {
		return nil, false, err
	}
	doc, err := s.e.codec.Decode(v.YAML)
	return doc, err == nil, err
}

// load returns a document, refreshed with every chunk stored so far.
// The caller holds sd.mu.
func (s *Shared) load(ctx context.Context, docID string) (*sharedDoc, error) {
	s.mu.Lock()
	sd, ok := s.open[docID]
	if !ok {
		sd = &sharedDoc{}
		s.open[docID] = sd
	}
	s.mu.Unlock()
	sd.mu.Lock()
	if err := s.refresh(ctx, docID, sd); err != nil {
		sd.mu.Unlock()
		return nil, err
	}
	return sd, nil
}

func (s *Shared) refresh(ctx context.Context, docID string, sd *sharedDoc) error {
	if sd.doc == nil {
		kind, id, err := s.docs.ManifestFor(ctx, docID)
		if err != nil {
			return err
		}
		snapshot, chunks, err := s.docs.Load(ctx, docID)
		if err != nil {
			return err
		}
		doc, err := s.crdt.Load(snapshot)
		if err != nil {
			return err
		}
		sd.kind, sd.id, sd.doc, sd.chunks = kind, id, doc, len(chunks)
		return s.fold(sd, chunks)
	}
	chunks, err := s.docs.Since(ctx, docID, sd.seq)
	if err != nil {
		return err
	}
	return s.fold(sd, chunks)
}

func (s *Shared) fold(sd *sharedDoc, chunks []store.Chunk) error {
	for _, c := range chunks {
		if err := sd.doc.LoadIncremental(c.Data); err != nil {
			return fmt.Errorf("chunk %d: %w", c.Seq, err)
		}
		sd.seq = c.Seq
	}
	sd.chunks += len(chunks)
	return nil
}

// Receive applies a sync message from a peer. A principal who may not
// write may still sync, to read; a message of theirs that would change
// the document is refused, and the document is left as it was. The
// presence document takes no changes from anyone.
func (s *Shared) Receive(ctx context.Context, docID string, peer crdt.SyncState, msg []byte, canWrite bool) (changed bool, err error) {
	sd, err := s.load(ctx, docID)
	if err != nil {
		return false, err
	}
	defer sd.mu.Unlock()
	before, err := sd.doc.Heads()
	if err != nil {
		return false, err
	}
	if !canWrite || sd.kind == presenceKind {
		// Try it on a copy first: a read must not change anything.
		fork, err := sd.doc.Fork()
		if err != nil {
			return false, err
		}
		defer fork.Close()
		if err := fork.ReceiveSyncMessage(peer, msg); err != nil {
			return false, err
		}
		after, err := fork.Heads()
		if err != nil {
			return false, err
		}
		if !after.Equal(before) {
			return false, ErrReadOnly
		}
		return false, nil
	}
	if err := sd.doc.ReceiveSyncMessage(peer, msg); err != nil {
		return false, err
	}
	after, err := sd.doc.Heads()
	if err != nil {
		return false, err
	}
	if after.Equal(before) {
		return false, nil
	}
	return true, s.persist(ctx, docID, sd)
}

// Generate returns the next sync message for a peer, after folding in
// what other replicas stored; ok is false when the peer is up to date.
func (s *Shared) Generate(ctx context.Context, docID string, peer crdt.SyncState) ([]byte, bool, error) {
	sd, err := s.load(ctx, docID)
	if err != nil {
		return nil, false, err
	}
	defer sd.mu.Unlock()
	return sd.doc.GenerateSyncMessage(peer)
}

// Reconcile folds a whole document into a manifest's shared draft as one
// change: what SaveWorking, an import or a changed file means for the
// draft. It creates the draft when the manifest has none yet.
func (s *Shared) Reconcile(ctx context.Context, kind, id string, content map[string]any, message string) error {
	docID, err := s.docs.DocumentFor(ctx, kind, id)
	if errors.Is(err, store.ErrNoDocument) {
		_, err = s.create(ctx, kind, id, content, s.e.Shape(kind))
		return err
	}
	if err != nil {
		return err
	}
	_, err = s.reconcile(ctx, docID, content, crdt.Change{Message: message})
	return err
}

func (s *Shared) reconcile(ctx context.Context, docID string, content map[string]any, meta crdt.Change) (bool, error) {
	sd, err := s.load(ctx, docID)
	if err != nil {
		return false, err
	}
	defer sd.mu.Unlock()
	changed, err := sd.doc.Reconcile(content, s.e.Shape(sd.kind), meta)
	if err != nil || !changed {
		return changed, err
	}
	return true, s.store(ctx, docID, sd)
}

// Conflicts returns the fields of a manifest's draft that hold
// concurrent values: the conflict notes, derived on read, so no replica
// keeps them.
func (s *Shared) Conflicts(ctx context.Context, kind, id string) ([]crdt.Conflict, error) {
	docID, err := s.docs.DocumentFor(ctx, kind, id)
	if errors.Is(err, store.ErrNoDocument) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sd, err := s.load(ctx, docID)
	if err != nil {
		return nil, err
	}
	defer sd.mu.Unlock()
	return sd.doc.Conflicts()
}

// persist stores what a sync message changed and writes the working copy
// everything else reads.
func (s *Shared) persist(ctx context.Context, docID string, sd *sharedDoc) error {
	if err := s.store(ctx, docID, sd); err != nil {
		return err
	}
	return s.materialise(ctx, sd)
}

// store appends the document's new changes as a chunk, announces it, and
// compacts when enough chunks have gathered.
func (s *Shared) store(ctx context.Context, docID string, sd *sharedDoc) error {
	chunk, err := sd.doc.SaveIncremental()
	if err != nil || len(chunk) == 0 {
		return err
	}
	seq, err := s.docs.Append(ctx, docID, chunk)
	if err != nil {
		// The cached document now holds changes the store does not, and
		// SaveIncremental will not return them again. Drop it, so the
		// next use loads what the store has and the peer, which still
		// holds its changes, sends them again.
		s.discard(sd)
		s.storeErrors.Add(1)
		return err
	}
	s.stored.Add(1)
	// Folding our own chunk back in is a no-op for the document; it moves
	// the position so the next refresh does not fetch it.
	if seq == sd.seq+1 {
		sd.seq = seq
	}
	sd.chunks++
	hint, _ := json.Marshal(Changed{Replica: s.replica, Seq: seq})
	// A lost hint costs latency, not an edit (docs/adr/0008).
	_ = s.fan.Publish(ctx, DocTopic(docID), hint)
	if sd.chunks >= compactEvery {
		if err := s.compact(ctx, docID, sd); err == nil {
			sd.chunks = 0
		}
	}
	return nil
}

func (s *Shared) compact(ctx context.Context, docID string, sd *sharedDoc) error {
	// Refresh first so the snapshot covers every chunk up to the position
	// it claims.
	if err := s.refresh(ctx, docID, sd); err != nil {
		return err
	}
	snapshot, err := sd.doc.Save()
	if err != nil {
		return err
	}
	if err := s.docs.Compact(ctx, docID, snapshot, sd.seq); err != nil {
		return err
	}
	s.compactions.Add(1)
	return nil
}

// materialise writes the draft as the manifest's working copy.
func (s *Shared) materialise(ctx context.Context, sd *sharedDoc) error {
	if sd.kind == presenceKind {
		return nil
	}
	doc, err := sd.doc.JSON()
	if err != nil {
		return err
	}
	text, err := s.e.codec.Encode(doc)
	if err != nil {
		return err
	}
	return s.e.PutWorking(ctx, sd.kind, sd.id, text)
}

// discard drops a cached document's state; the caller holds sd.mu.
func (s *Shared) discard(sd *sharedDoc) {
	if sd.doc != nil {
		sd.doc.Close()
	}
	sd.doc, sd.seq, sd.chunks = nil, 0, 0
}

// Forget drops a document from this replica's cache. Nothing is lost:
// the next use loads it from the store.
func (s *Shared) Forget(docID string) {
	s.mu.Lock()
	sd, ok := s.open[docID]
	delete(s.open, docID)
	s.mu.Unlock()
	if ok {
		sd.mu.Lock()
		if sd.doc != nil {
			sd.doc.Close()
		}
		sd.mu.Unlock()
	}
}

// NewDocumentID returns a random automerge-repo document id: sixteen
// random bytes in base58check, as automerge-repo writes them, so the
// stock libraries accept it.
func NewDocumentID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // the system's randomness failing is not recoverable
	}
	return base58Check(b[:])
}

func base58Check(payload []byte) string {
	first := sha256.Sum256(payload)
	second := sha256.Sum256(first[:])
	return base58(append(append([]byte{}, payload...), second[:4]...))
}

const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

func base58(b []byte) string {
	n := new(big.Int).SetBytes(b)
	base, mod := big.NewInt(58), new(big.Int)
	var out []byte
	for n.Sign() > 0 {
		n.DivMod(n, base, mod)
		out = append(out, base58Alphabet[mod.Int64()])
	}
	for _, c := range b {
		if c != 0 {
			break
		}
		out = append(out, '1')
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return string(out)
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// ManifestFor names the manifest a document is the draft of. The
// presence document answers kind "" and id "presence".
func (s *Shared) ManifestFor(ctx context.Context, docID string) (kind, id string, err error) {
	return s.docs.ManifestFor(ctx, docID)
}

// IsPresence reports whether a manifest name returned by ManifestFor is
// the presence document's.
func IsPresence(kind, id string) bool { return kind == presenceKind && id == presenceID }

// NewSyncState returns the sync state for a new peer of a document.
func (s *Shared) NewSyncState() (crdt.SyncState, error) { return s.crdt.NewSyncState() }

// SaveWorking writes a whole working copy, as an autosave or an import
// does, and folds it into the manifest's shared draft, so a client that
// saves whole documents and one that syncs end in the same draft.
func (e *Engine) SaveWorking(ctx context.Context, kind, id string, text []byte, actor string) error {
	if err := e.PutWorking(ctx, kind, id, text); err != nil {
		return err
	}
	if e.shared == nil {
		return nil
	}
	doc, err := e.codec.Decode(text)
	if err != nil {
		return nil // an unparseable working copy is kept as text; the draft waits for a valid one
	}
	return e.shared.Reconcile(ctx, kind, id, doc, "saved by "+actor)
}
