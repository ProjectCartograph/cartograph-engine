package memory

import (
	"context"
	"fmt"
	"sync"

	"github.com/ProjectCartograph/cartograph-engine/internal/store"
)

// OpLog is the in-memory store.OpLog, for tests and for a single-process
// deployment that accepts losing the shared draft on restart (the
// committed versions are never in the log).
type OpLog struct {
	mu   sync.Mutex
	logs map[manifestKey][]store.Op
}

var _ store.OpLog = (*OpLog)(nil)

// NewOpLog returns an empty OpLog.
func NewOpLog() *OpLog { return &OpLog{logs: map[manifestKey][]store.Op{}} }

func (l *OpLog) Append(_ context.Context, ops []store.Op) (int64, error) {
	if len(ops) == 0 {
		return 0, nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	k := manifestKey{ops[0].Kind, ops[0].ID}
	log := l.logs[k]
	next := int64(1)
	if n := len(log); n > 0 {
		next = log[n-1].Seq + 1
	}
	first := next
	for _, op := range ops {
		if op.Kind != k.kind || op.ID != k.id {
			return 0, fmt.Errorf("append: ops for %s/%s and %s/%s in one batch", k.kind, k.id, op.Kind, op.ID)
		}
		op.Seq = next
		next++
		log = append(log, op)
	}
	l.logs[k] = log
	return first, nil
}

func (l *OpLog) Since(_ context.Context, kind, id string, after int64, limit int) ([]store.Op, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := []store.Op{}
	for _, op := range l.logs[manifestKey{kind, id}] {
		if op.Seq > after {
			out = append(out, op)
			if limit > 0 && len(out) == limit {
				break
			}
		}
	}
	return out, nil
}

func (l *OpLog) Latest(_ context.Context, kind, id string) (int64, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	log := l.logs[manifestKey{kind, id}]
	if len(log) == 0 {
		return 0, nil
	}
	return log[len(log)-1].Seq, nil
}

func (l *OpLog) Compact(_ context.Context, kind, id string, seq int64) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	k := manifestKey{kind, id}
	kept := l.logs[k][:0]
	for _, op := range l.logs[k] {
		if op.Seq > seq {
			kept = append(kept, op)
		}
	}
	l.logs[k] = kept
	return nil
}
