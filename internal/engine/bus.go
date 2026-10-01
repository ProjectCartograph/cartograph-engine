package engine

import (
	"context"
	"sync"
	"time"
)

// Event is one thing that happened to a manifest that an open interface
// wants to know about without asking: a version saved, its state
// changed. Interfaces subscribe through the Bus; transports (an SSE
// stream, an in-process channel) carry what the bus emits. Live edits and
// presence are not events: they travel on the sync socket.
type Event struct {
	Type string    `json:"type"` // version, state
	Kind string    `json:"kind,omitempty"`
	ID   string    `json:"id,omitempty"`
	On   time.Time `json:"on"`
	// Seq is the version number for a version event.
	Seq int64 `json:"seq,omitempty"`
	// Actor is who did it.
	Actor string `json:"actor,omitempty"`
	// Field is where, when the event concerns one field.
	Field string `json:"field,omitempty"`
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
	// blocking the publisher; it reads the state again when it next needs it.
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

// WithBus sets the event bus; the in-process one when none is given.
func WithBus(b Bus) Option { return func(e *Engine) { e.bus = b } }

// Bus returns the event bus.
func (e *Engine) Bus() Bus { return e.bus }
