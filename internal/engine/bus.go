package engine

import (
	"context"
	"sync"
	"time"
)

// Event is one thing that happened to a manifest that an open interface
// wants to know about without asking: ops appended to its draft, a
// version saved, its state changed, somebody arriving at or leaving a
// field. Interfaces subscribe through the Bus; transports (an SSE
// stream, an in-process channel, a socket) carry what the bus emits.
type Event struct {
	Type string    `json:"type"` // ops, version, state, presence
	Kind string    `json:"kind,omitempty"`
	ID   string    `json:"id,omitempty"`
	On   time.Time `json:"on"`
	// Seq is the op log position for ops events, the version number for
	// version events; a client resumes from the last Seq it saw.
	Seq int64 `json:"seq,omitempty"`
	// Ops carries the appended ops for an ops event.
	Ops []EventOp `json:"ops,omitempty"`
	// Actor is who did it, for version, state and presence events.
	Actor string `json:"actor,omitempty"`
	// Presence carries where the actor is for a presence event; Field is
	// empty when they left the manifest.
	Field string `json:"field,omitempty"`
}

// EventOp is an op as the bus carries it: the merge op plus its
// position.
type EventOp struct {
	Seq int64 `json:"seq"`
	Op  any   `json:"op"`
}

// Subscription is one listener on the bus.
type Subscription struct {
	C      <-chan Event
	cancel func()
}

// Close stops the subscription.
func (s *Subscription) Close() { s.cancel() }

// Bus fans events out to subscribers in one process. It is the port a
// multi-process deployment replaces (the Postgres index's LISTEN/NOTIFY,
// a broker); the engine only ever calls Publish and Subscribe.
type Bus interface {
	Publish(ev Event)
	// Subscribe delivers events for one manifest, or for every manifest
	// when kind is empty. A slow subscriber drops events rather than
	// blocking the publisher; it catches up from the op log by Seq.
	Subscribe(ctx context.Context, kind, id string) *Subscription
}

// MemoryBus is the in-process Bus.
type MemoryBus struct {
	mu   sync.Mutex
	subs map[*memorySub]struct{}
}

type memorySub struct {
	kind, id string
	ch       chan Event
}

// NewMemoryBus returns an empty bus.
func NewMemoryBus() *MemoryBus { return &MemoryBus{subs: map[*memorySub]struct{}{}} }

func (b *MemoryBus) Publish(ev Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for s := range b.subs {
		if s.kind != "" && (s.kind != ev.Kind || (s.id != "" && s.id != ev.ID)) {
			continue
		}
		select {
		case s.ch <- ev:
		default: // a slow listener catches up from the log
		}
	}
}

func (b *MemoryBus) Subscribe(ctx context.Context, kind, id string) *Subscription {
	s := &memorySub{kind: kind, id: id, ch: make(chan Event, 64)}
	b.mu.Lock()
	b.subs[s] = struct{}{}
	b.mu.Unlock()
	var once sync.Once
	cancel := func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.subs, s)
			b.mu.Unlock()
			close(s.ch)
		})
	}
	go func() {
		<-ctx.Done()
		cancel()
	}()
	return &Subscription{C: s.ch, cancel: cancel}
}
