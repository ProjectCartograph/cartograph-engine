package engine

//lint:file-ignore SA1019 the merge-based shared draft is deprecated in 1.1.0 and removed in 2.0.0 (docs/adr/0007); until then this file still carries it.

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/internal/kinds"
	"github.com/ProjectCartograph/cartograph-engine/internal/store"
	"github.com/ProjectCartograph/cartograph-engine/pkg/merge"
)

// Drafts is the shared working document of every open manifest: one
// merge.State per manifest, built from its current version and the op
// log, fed by every interface's edits, materialised back into the
// working copy so everything that reads working copies today keeps
// working. This is where multiplayer lives in the engine; the
// interfaces only send ops and listen.
//
// Nil until the engine is given an op log (WithOpLog); the single-user
// paths (PutWorking, Commit) never need it.
type Drafts struct {
	e     *Engine
	log   store.OpLog
	bus   Bus
	clock merge.HLC
	mu    sync.Mutex
	open  map[string]*draft // kind/id
	// Conflicts notes per manifest, newest last, bounded.
	notes map[string][]merge.Conflict
}

type draft struct {
	state *merge.State
	seq   int64 // last op log position folded in
}

// EditResult is what an edit answers: where the log is now, and the
// conflicts the edit raised.
type EditResult struct {
	Seq       int64
	Conflicts []merge.Conflict
}

const maxNotes = 50

// WithOpLog gives the engine an op log, which turns on Drafts.
func WithOpLog(l store.OpLog) Option {
	return func(e *Engine) { e.opLog = l }
}

// WithBus sets the event bus; the in-process one when none is given.
func WithBus(b Bus) Option {
	return func(e *Engine) { e.bus = b }
}

// Drafts returns the draft service, nil when the engine has no op log.
func (e *Engine) Drafts() *Drafts { return e.drafts }

// Bus returns the event bus.
func (e *Engine) Bus() Bus { return e.bus }

func (e *Engine) initDrafts() {
	if e.bus == nil {
		e.bus = NewMemoryBus()
	}
	if e.opLog != nil {
		e.drafts = &Drafts{e: e, log: e.opLog, bus: e.bus, clock: merge.HLC{Actor: "engine"}, open: map[string]*draft{}, notes: map[string][]merge.Conflict{}}
	}
}

func key(kind, id string) string { return kind + "/" + id }

// Keyer returns the merge.Keyer for a kind: which lists are keyed and
// by what, from x-cartograph-list-key in the schema. A list of objects that
// carry an "id" is keyed by it even when the schema says nothing, since
// that is how every identified list in the contract is written.
func (e *Engine) Keyer(kind string) merge.Keyer {
	spec, _ := kinds.ByName(kind)
	raw := e.schemas.raw[spec.SchemaFile]
	return func(listPath string, elem map[string]any) (string, bool) {
		if k := listKeyFor(raw, e.schemas.raw, listPath); k != "" {
			v, ok := elem[k]
			s := fmt.Sprint(v)
			return s, ok && s != ""
		}
		if id, ok := elem["id"].(string); ok && id != "" {
			return id, true
		}
		return "", false
	}
}

// listKeyFor walks a schema document along a JSON pointer, following
// properties, items and same-bundle $refs, and returns the list's
// x-cartograph-list-key, or "" when the list is not keyed.
func listKeyFor(doc map[string]any, all map[string]map[string]any, path string) string {
	node := doc
	for _, seg := range merge.Split(path) {
		node = resolveRef(node, all)
		if node == nil {
			return ""
		}
		if strings.HasPrefix(seg, "{") {
			items, _ := node["items"].(map[string]any)
			node = items
			continue
		}
		props, _ := node["properties"].(map[string]any)
		next, _ := props[seg].(map[string]any)
		node = next
	}
	node = resolveRef(node, all)
	if node == nil {
		return ""
	}
	k, _ := node["x-cartograph-list-key"].(string)
	return k
}

func resolveRef(node map[string]any, all map[string]map[string]any) map[string]any {
	for i := 0; node != nil && i < 8; i++ {
		ref, ok := node["$ref"].(string)
		if !ok {
			return node
		}
		file, frag, _ := strings.Cut(ref, "#")
		target := all[file]
		if file == "" {
			return nil
		}
		cur := map[string]any(target)
		for _, seg := range strings.Split(strings.TrimPrefix(frag, "/"), "/") {
			if seg == "" {
				continue
			}
			cur, _ = cur[seg].(map[string]any)
			if cur == nil {
				return nil
			}
		}
		node = cur
	}
	return node
}

// Open loads a manifest's draft: the current version decomposed into
// leaves, then every op in the log folded in. Idempotent; cheap after
// the first call.
func (d *Drafts) Open(ctx context.Context, kind, id string) (*merge.State, int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	dr, err := d.openLocked(ctx, kind, id)
	if err != nil {
		return nil, 0, err
	}
	return dr.state.Clone(), dr.seq, nil
}

func (d *Drafts) openLocked(ctx context.Context, kind, id string) (*draft, error) {
	k := key(kind, id)
	if dr, ok := d.open[k]; ok {
		return dr, d.catchUp(ctx, kind, id, dr)
	}
	if _, ok := kinds.ByName(kind); !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownKind, kind)
	}
	st := merge.New()
	base := Clock0()
	if text, found, err := d.e.manifests.GetWorking(ctx, kind, id); err == nil && found {
		if doc, err := d.e.codec.Decode(text); err == nil {
			for _, op := range merge.Decompose(doc, base, d.e.Keyer(kind)) {
				st.Apply(op)
			}
		}
	} else if v, found, err := d.e.manifests.GetCurrent(ctx, kind, id); err == nil && found {
		if doc, err := d.e.codec.Decode(v.YAML); err == nil {
			for _, op := range merge.Decompose(doc, base, d.e.Keyer(kind)) {
				st.Apply(op)
			}
		}
	}
	dr := &draft{state: st}
	d.open[k] = dr
	return dr, d.catchUp(ctx, kind, id, dr)
}

// Clock0 is the stamp a committed version's leaves carry: not zero (zero
// means "saw nothing", and a writer who saw the base must be told when
// somebody wrote over it), and before every real clock, so any edit wins
// over the base.
func Clock0() merge.Clock { return merge.Clock{Wall: 1, Logical: 0, Actor: "version"} }

func (d *Drafts) catchUp(ctx context.Context, kind, id string, dr *draft) error {
	ops, err := d.log.Since(ctx, kind, id, dr.seq, 0)
	if err != nil {
		return err
	}
	for _, op := range ops {
		dr.state.Apply(op.Op)
		dr.seq = op.Seq
		d.clock.Observe(op.Clock)
	}
	return nil
}

// Edit applies ops from one actor, appends them to the log, publishes
// them, and writes the materialised document as the working copy.
// Conflicts are returned and kept as notes for the checks list.
func (d *Drafts) Edit(ctx context.Context, kind, id, actor string, ops []merge.Op) (EditResult, error) {
	if len(ops) == 0 {
		return EditResult{}, nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	dr, err := d.openLocked(ctx, kind, id)
	if err != nil {
		return EditResult{}, err
	}
	now := time.Now().UTC()
	var conflicts []merge.Conflict
	logOps := make([]store.Op, 0, len(ops))
	for i := range ops {
		op := ops[i]
		if op.Clock.Actor == "" {
			op.Clock.Actor = actor
		}
		if op.Clock.IsZero() || op.Clock.Wall == 0 {
			op.Clock = d.clock.Now(now.UnixMilli())
			op.Clock.Actor = actor
		}
		d.clock.Observe(op.Clock)
		if _, c := dr.state.Apply(op); c != nil {
			conflicts = append(conflicts, *c)
		}
		logOps = append(logOps, store.Op{Kind: kind, ID: id, On: now, Op: op})
	}
	first, err := d.log.Append(ctx, logOps)
	if err != nil {
		return EditResult{}, err
	}
	evOps := make([]EventOp, len(logOps))
	for i := range logOps {
		logOps[i].Seq = first + int64(i)
		evOps[i] = EventOp{Seq: logOps[i].Seq, Op: logOps[i].Op}
	}
	dr.seq = logOps[len(logOps)-1].Seq
	if len(conflicts) > 0 {
		notes := append(d.notes[key(kind, id)], conflicts...)
		if len(notes) > maxNotes {
			notes = notes[len(notes)-maxNotes:]
		}
		d.notes[key(kind, id)] = notes
	}

	// The working copy everything else reads is the materialised draft.
	text, err := d.e.codec.Encode(dr.state.Document())
	if err != nil {
		return EditResult{}, err
	}
	if err := d.e.PutWorking(ctx, kind, id, text); err != nil {
		return EditResult{}, err
	}
	d.bus.Publish(Event{Type: "ops", Kind: kind, ID: id, On: now, Seq: dr.seq, Ops: evOps, Actor: actor})
	return EditResult{Seq: dr.seq, Conflicts: conflicts}, nil
}

// Since returns the ops after a position, for a client catching up.
func (d *Drafts) Since(ctx context.Context, kind, id string, after int64) ([]store.Op, error) {
	return d.log.Since(ctx, kind, id, after, 0)
}

// Notes returns the conflict notes recorded for a manifest, oldest first.
func (d *Drafts) Notes(kind, id string) []merge.Conflict {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]merge.Conflict(nil), d.notes[key(kind, id)]...)
}

// Committed is called after a version is saved: the draft's history is
// compacted, since the version is now the base, and the notes cleared.
func (d *Drafts) Committed(ctx context.Context, kind, id string, number int) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	k := key(kind, id)
	if dr, ok := d.open[k]; ok {
		if err := d.log.Compact(ctx, kind, id, dr.seq); err != nil {
			return err
		}
	}
	delete(d.open, k)
	delete(d.notes, k)
	d.bus.Publish(Event{Type: "version", Kind: kind, ID: id, On: time.Now().UTC(), Seq: int64(number)})
	return nil
}
