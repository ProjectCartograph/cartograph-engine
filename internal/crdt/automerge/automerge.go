// Package automerge is the crdt port's adapter: Automerge, compiled to
// WebAssembly from the crate in crdt/ and run on wazero, so the engine
// stays pure Go (CGO_ENABLED=0) and speaks the same document format and
// sync protocol as every interface using the stock Automerge libraries
// (docs/adr/0007).
//
// The module is compiled once per process and instantiated a few times;
// each document and sync state lives in one instance, and a mutex per
// instance serialises calls into it. The adapter knows the port and
// wazero, nothing else of the engine.
//
// automerge.wasm is generated: `just generate` rebuilds it from crdt/
// with `nix build .#automerge-wasm`, and `just drift` refuses a committed
// module that differs from what its source builds.
package automerge

import (
	"bytes"
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/crdt"
)

//go:embed automerge.wasm
var module []byte

// compiled is the module compiled for this machine. Compiling takes far
// longer than instantiating, and the result is immutable, so every
// Engine in the process shares one: it is a cache, not state. The
// runtime that owns it lives as long as the process.
var compiled = sync.OnceValues(func() (*shared, error) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, rt); err != nil {
		return nil, fmt.Errorf("automerge: instantiate wasi: %w", err)
	}
	mod, err := rt.CompileModule(ctx, module)
	if err != nil {
		return nil, fmt.Errorf("automerge: compile module: %w", err)
	}
	return &shared{rt: rt, mod: mod}, nil
})

type shared struct {
	rt  wazero.Runtime
	mod wazero.CompiledModule
}

// Engine is a crdt.Engine backed by a small pool of module instances.
type Engine struct {
	instances []*instance
	next      atomic.Uint32
}

var _ crdt.Engine = (*Engine)(nil)

// New returns an engine with n module instances; n <= 0 chooses one per
// CPU, up to four. Documents are spread over the instances, so n bounds
// how many documents are worked on at once.
func New(n int) (*Engine, error) {
	if n <= 0 {
		n = min(runtime.GOMAXPROCS(0), 4)
	}
	s, err := compiled()
	if err != nil {
		return nil, err
	}
	e := &Engine{}
	for range n {
		in, err := instantiate(s)
		if err != nil {
			e.Close()
			return nil, err
		}
		e.instances = append(e.instances, in)
	}
	return e, nil
}

func (e *Engine) pick() *instance {
	return e.instances[int(e.next.Add(1))%len(e.instances)]
}

// New returns an empty document with a random actor.
func (e *Engine) New() (crdt.Doc, error) {
	in := e.pick()
	in.mu.Lock()
	defer in.mu.Unlock()
	h, err := in.call("new document", in.fn.docNew)
	if err != nil {
		return nil, err
	}
	return &doc{in: in, h: uint64(h)}, nil
}

// Load returns a document from saved bytes, with a random actor.
func (e *Engine) Load(data []byte) (crdt.Doc, error) {
	in := e.pick()
	in.mu.Lock()
	defer in.mu.Unlock()
	args, err := in.args(data)
	if err != nil {
		return nil, err
	}
	h, err := in.call("load document", in.fn.docLoad, args...)
	if err != nil {
		return nil, err
	}
	return &doc{in: in, h: uint64(h)}, nil
}

// NewSyncState returns the state for a new peer. It joins the instance
// of the document it is first used with, since the protocol state and
// the document must live side by side.
func (e *Engine) NewSyncState() (crdt.SyncState, error) {
	return &syncState{}, nil
}

// Close closes every instance; documents and sync states on them return
// crdt.ErrClosed from then on.
func (e *Engine) Close() error {
	var errs []error
	for _, in := range e.instances {
		errs = append(errs, in.close())
	}
	return errors.Join(errs...)
}

// instance is one instantiation of the module, with its exports and an
// argument buffer reused across calls.
type instance struct {
	mu  sync.Mutex
	mod api.Module
	mem api.Memory
	fn  struct {
		alloc, free, out                                             api.Function
		docNew, docLoad, docFree, docSave, docSaveIncremental        api.Function
		docLoadIncremental, docFork, docJSON, docReconcile, docHeads api.Function
		docConflicts, syncNew, syncFree, syncGenerate, syncReceive   api.Function
	}
	// The argument buffer, in the module's memory.
	buf, bufCap uint32
	// stderr holds what the module printed, which is only ever a panic
	// message: worth having when it traps.
	stderr bytes.Buffer
	// broken is set when the instance traps or is closed. A trap leaves
	// the module's memory in an unknown state, so nothing runs in it
	// again.
	broken error
}

func instantiate(s *shared) (*instance, error) {
	in := &instance{}
	// An anonymous module, so the runtime holds any number of them; no
	// start function, since the crate is a library; and the operating
	// system's randomness for actor ids, because wazero's default source
	// is deterministic and two replicas would then share an actor.
	cfg := wazero.NewModuleConfig().
		WithName("").
		WithStartFunctions().
		WithRandSource(rand.Reader).
		WithStderr(&in.stderr)
	mod, err := s.rt.InstantiateModule(context.Background(), s.mod, cfg)
	if err != nil {
		return nil, fmt.Errorf("automerge: instantiate module: %w", err)
	}
	in.mod = mod
	in.mem = mod.Memory()
	for name, f := range map[string]*api.Function{
		"cg_alloc": &in.fn.alloc, "cg_free": &in.fn.free, "cg_out": &in.fn.out,
		"cg_doc_new": &in.fn.docNew, "cg_doc_load": &in.fn.docLoad, "cg_doc_free": &in.fn.docFree,
		"cg_doc_save": &in.fn.docSave, "cg_doc_save_incremental": &in.fn.docSaveIncremental,
		"cg_doc_load_incremental": &in.fn.docLoadIncremental, "cg_doc_fork": &in.fn.docFork,
		"cg_doc_json": &in.fn.docJSON, "cg_doc_reconcile": &in.fn.docReconcile,
		"cg_doc_heads": &in.fn.docHeads, "cg_doc_conflicts": &in.fn.docConflicts,
		"cg_sync_new": &in.fn.syncNew, "cg_sync_free": &in.fn.syncFree,
		"cg_sync_generate": &in.fn.syncGenerate, "cg_sync_receive": &in.fn.syncReceive,
	} {
		if *f = mod.ExportedFunction(name); *f == nil {
			mod.Close(context.Background())
			return nil, fmt.Errorf("automerge: module does not export %s", name)
		}
	}
	return in, nil
}

func (in *instance) close() error {
	in.mu.Lock()
	defer in.mu.Unlock()
	if errors.Is(in.broken, crdt.ErrClosed) {
		return nil
	}
	in.broken = crdt.ErrClosed
	return in.mod.Close(context.Background())
}

// moduleError is an error the module reported: a code and its message.
type moduleError struct {
	op      string
	code    int32
	message string
}

func (e *moduleError) Error() string {
	return fmt.Sprintf("automerge: %s: %s (code %d)", e.op, e.message, e.code)
}

// call runs an export and returns its non-negative result; a negative
// one is the module's error, with the message it left in its output
// buffer. The caller holds in.mu.
func (in *instance) call(op string, f api.Function, args ...uint64) (int32, error) {
	if in.broken != nil {
		return 0, in.broken
	}
	res, err := f.Call(context.Background(), args...)
	if err != nil {
		in.broken = fmt.Errorf("automerge: %s: module trapped: %w: %s", op, err, bytes.TrimSpace(in.stderr.Bytes()))
		in.mod.Close(context.Background())
		return 0, in.broken
	}
	r := int32(uint32(res[0]))
	if r < 0 {
		msg, err := in.out()
		if err != nil {
			return 0, err
		}
		return 0, &moduleError{op: op, code: r, message: string(msg)}
	}
	return r, nil
}

// out copies the module's output buffer.
func (in *instance) out() ([]byte, error) {
	res, err := in.fn.out.Call(context.Background())
	if err != nil {
		in.broken = fmt.Errorf("automerge: read output: module trapped: %w", err)
		return nil, in.broken
	}
	ptr, n := uint32(res[0]>>32), uint32(res[0])
	b, ok := in.mem.Read(ptr, n)
	if !ok {
		return nil, fmt.Errorf("automerge: output buffer %d+%d is outside memory", ptr, n)
	}
	return bytes.Clone(b), nil
}

// args writes byte arguments one after another into the argument buffer
// and returns them as (pointer, length) pairs. The buffer grows to the
// largest call seen, so steady work allocates nothing in the module.
// The caller holds in.mu.
func (in *instance) args(parts ...[]byte) ([]uint64, error) {
	if in.broken != nil {
		return nil, in.broken
	}
	total := 0
	for _, p := range parts {
		total += len(p)
	}
	if total > 1<<31 {
		return nil, fmt.Errorf("automerge: arguments of %d bytes are too large", total)
	}
	if uint32(total) > in.bufCap {
		if in.bufCap > 0 {
			if _, err := in.fn.free.Call(context.Background(), uint64(in.buf), uint64(in.bufCap)); err != nil {
				in.broken = fmt.Errorf("automerge: free: module trapped: %w", err)
				return nil, in.broken
			}
			in.buf, in.bufCap = 0, 0
		}
		size := max(uint32(total), 64<<10)
		size = (size + 4095) &^ 4095
		res, err := in.fn.alloc.Call(context.Background(), uint64(size))
		if err != nil {
			in.broken = fmt.Errorf("automerge: alloc: module trapped: %w", err)
			return nil, in.broken
		}
		in.buf, in.bufCap = uint32(res[0]), size
	}
	out := make([]uint64, 0, 2*len(parts))
	at := in.buf
	for _, p := range parts {
		if !in.mem.Write(at, p) {
			return nil, fmt.Errorf("automerge: argument buffer %d+%d is outside memory", at, len(p))
		}
		out = append(out, uint64(at), uint64(len(p)))
		at += uint32(len(p))
	}
	return out, nil
}

// doc is a crdt.Doc: a handle in one instance.
type doc struct {
	in *instance
	h  uint64 // zero once closed
}

var _ crdt.Doc = (*doc)(nil)

// lock takes the instance for one operation, or reports why it cannot.
func (d *doc) lock() error {
	d.in.mu.Lock()
	if d.h == 0 {
		d.in.mu.Unlock()
		return crdt.ErrClosed
	}
	return nil
}

// output runs an export that leaves bytes in the output buffer.
func (d *doc) output(op string, f api.Function, args ...uint64) ([]byte, error) {
	if err := d.lock(); err != nil {
		return nil, err
	}
	defer d.in.mu.Unlock()
	if _, err := d.in.call(op, f, append([]uint64{d.h}, args...)...); err != nil {
		return nil, err
	}
	return d.in.out()
}

func (d *doc) JSON() (map[string]any, error) {
	b, err := d.output("materialise", d.in.fn.docJSON)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := decode(b, &m); err != nil {
		return nil, fmt.Errorf("automerge: materialise: %w", err)
	}
	numbers(m)
	return m, nil
}

// shapeWire is crdt.Shape as the module reads it.
type shapeWire struct {
	ListKeys map[string]string `json:"listKeys,omitempty"`
	Texts    []string          `json:"texts,omitempty"`
}

func (d *doc) Reconcile(v map[string]any, shape crdt.Shape, meta crdt.Change) (bool, error) {
	if v == nil {
		v = map[string]any{}
	}
	j, err := json.Marshal(v)
	if err != nil {
		return false, fmt.Errorf("automerge: reconcile: encode document: %w", err)
	}
	s, err := json.Marshal(shapeWire{ListKeys: shape.ListKeys, Texts: shape.Texts})
	if err != nil {
		return false, fmt.Errorf("automerge: reconcile: encode shape: %w", err)
	}
	if err := d.lock(); err != nil {
		return false, err
	}
	defer d.in.mu.Unlock()
	args, err := d.in.args(j, s, []byte(meta.Message))
	if err != nil {
		return false, err
	}
	args = append([]uint64{d.h}, args...)
	args = append(args, api.EncodeI64(meta.Time))
	changed, err := d.in.call("reconcile", d.in.fn.docReconcile, args...)
	return changed == 1, err
}

func (d *doc) Heads() (crdt.Heads, error) {
	b, err := d.output("heads", d.in.fn.docHeads)
	if err != nil {
		return nil, err
	}
	var h crdt.Heads
	if err := json.Unmarshal(b, &h); err != nil {
		return nil, fmt.Errorf("automerge: heads: %w", err)
	}
	return h, nil
}

func (d *doc) Conflicts() ([]crdt.Conflict, error) {
	b, err := d.output("conflicts", d.in.fn.docConflicts)
	if err != nil {
		return nil, err
	}
	var wire []struct {
		Path   string `json:"path"`
		Values []any  `json:"values"`
	}
	if err := decode(b, &wire); err != nil {
		return nil, fmt.Errorf("automerge: conflicts: %w", err)
	}
	out := make([]crdt.Conflict, len(wire))
	for i, c := range wire {
		numbers(c.Values)
		out[i] = crdt.Conflict{Path: c.Path, Values: c.Values}
	}
	return out, nil
}

func (d *doc) Save() ([]byte, error) {
	return d.output("save", d.in.fn.docSave)
}

func (d *doc) SaveIncremental() ([]byte, error) {
	b, err := d.output("save incremental", d.in.fn.docSaveIncremental)
	if len(b) == 0 {
		return nil, err
	}
	return b, err
}

func (d *doc) LoadIncremental(data []byte) error {
	if err := d.lock(); err != nil {
		return err
	}
	defer d.in.mu.Unlock()
	args, err := d.in.args(data)
	if err != nil {
		return err
	}
	_, err = d.in.call("load incremental", d.in.fn.docLoadIncremental, append([]uint64{d.h}, args...)...)
	return err
}

func (d *doc) GenerateSyncMessage(s crdt.SyncState) ([]byte, bool, error) {
	ss, err := d.bind(s)
	if err != nil {
		return nil, false, err
	}
	if err := d.lock(); err != nil {
		return nil, false, err
	}
	defer d.in.mu.Unlock()
	ok, err := d.in.call("generate sync message", d.in.fn.syncGenerate, d.h, ss)
	if err != nil || ok == 0 {
		return nil, false, err
	}
	msg, err := d.in.out()
	return msg, err == nil, err
}

func (d *doc) ReceiveSyncMessage(s crdt.SyncState, msg []byte) error {
	ss, err := d.bind(s)
	if err != nil {
		return err
	}
	if err := d.lock(); err != nil {
		return err
	}
	defer d.in.mu.Unlock()
	args, err := d.in.args(msg)
	if err != nil {
		return err
	}
	_, err = d.in.call("receive sync message", d.in.fn.syncReceive, append([]uint64{d.h, ss}, args...)...)
	return err
}

// bind returns the handle of s in this document's instance, creating it
// on first use.
func (d *doc) bind(s crdt.SyncState) (uint64, error) {
	ss, ok := s.(*syncState)
	if !ok {
		return 0, fmt.Errorf("automerge: sync state %T is not this adapter's", s)
	}
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if ss.closed {
		return 0, crdt.ErrClosed
	}
	if ss.in != nil {
		if ss.in != d.in {
			return 0, errors.New("automerge: sync state belongs to another document")
		}
		return ss.h, nil
	}
	if err := d.lock(); err != nil {
		return 0, err
	}
	defer d.in.mu.Unlock()
	h, err := d.in.call("new sync state", d.in.fn.syncNew)
	if err != nil {
		return 0, err
	}
	ss.in, ss.h = d.in, uint64(h)
	return ss.h, nil
}

func (d *doc) Fork() (crdt.Doc, error) {
	if err := d.lock(); err != nil {
		return nil, err
	}
	defer d.in.mu.Unlock()
	h, err := d.in.call("fork", d.in.fn.docFork, d.h)
	if err != nil {
		return nil, err
	}
	return &doc{in: d.in, h: uint64(h)}, nil
}

func (d *doc) Close() error {
	d.in.mu.Lock()
	defer d.in.mu.Unlock()
	if d.h == 0 {
		return nil
	}
	h := d.h
	d.h = 0
	if errors.Is(d.in.broken, crdt.ErrClosed) {
		return nil
	}
	_, err := d.in.call("free document", d.in.fn.docFree, h)
	return err
}

// syncState is a crdt.SyncState, bound to an instance on first use.
type syncState struct {
	mu     sync.Mutex
	in     *instance
	h      uint64
	closed bool
}

var _ crdt.SyncState = (*syncState)(nil)

func (s *syncState) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.in == nil {
		return nil
	}
	s.in.mu.Lock()
	defer s.in.mu.Unlock()
	if errors.Is(s.in.broken, crdt.ErrClosed) {
		return nil
	}
	_, err := s.in.call("free sync state", s.in.fn.syncFree, s.h)
	return err
}

// decode reads JSON from the module keeping numbers as json.Number, so
// numbers can then choose each one's Go type without losing precision.
func decode(b []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	return dec.Decode(v)
}

// numbers replaces every json.Number in v, in place where it can, with
// what encoding/json would give (float64), except integers beyond 2^53,
// which a float64 cannot hold exactly and so stay exact as int64.
func numbers(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, x := range t {
			t[k] = numbers(x)
		}
	case []any:
		for i, x := range t {
			t[i] = numbers(x)
		}
	case json.Number:
		if i, err := t.Int64(); err == nil && (i > 1<<53 || i < -(1<<53)) {
			return i
		}
		f, _ := t.Float64()
		return f
	}
	return v
}
