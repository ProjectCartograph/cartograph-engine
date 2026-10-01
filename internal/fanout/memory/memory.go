// Package memory is the in-process fanout.Bus: every Bus made from one
// Hub delivers to every subscriber of that Hub. One Hub stands for one
// backend, so a test can run two replicas in one process; a deployment
// with one replica uses one Hub and one Bus.
package memory

import (
	"context"
	"errors"
	"sync"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/fanout"
)

// buffer is how many messages a subscriber may fall behind by before it
// loses them. Messages are hints, so a small buffer is enough.
const buffer = 64

// Hub is the shared backend: what a database or a broker is to the
// other adapters.
type Hub struct {
	mu   sync.Mutex
	subs map[string]map[*subscription]struct{}
}

// NewHub returns a Hub with no subscribers.
func NewHub() *Hub {
	return &Hub{subs: map[string]map[*subscription]struct{}{}}
}

// Bus is one replica's view of a Hub.
type Bus struct {
	hub *Hub

	mu     sync.Mutex
	closed bool
	own    map[*subscription]struct{}
}

var _ fanout.Bus = (*Bus)(nil)

// New returns a Bus over its own Hub, for a deployment of one replica.
func New() *Bus { return NewHub().Bus() }

// Bus returns a new Bus over h.
func (h *Hub) Bus() *Bus {
	return &Bus{hub: h, own: map[*subscription]struct{}{}}
}

var errClosed = errors.New("fanout: bus closed")

// Publish delivers data to every current subscriber of topic without
// waiting for any of them.
func (b *Bus) Publish(ctx context.Context, topic string, data []byte) error {
	if len(data) > fanout.MaxPayload || len(topic) > fanout.MaxTopic {
		return fanout.ErrTooLarge
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.Lock()
	closed := b.closed
	b.mu.Unlock()
	if closed {
		return errClosed
	}
	// Each subscriber gets its own copy, as it would from a network.
	h := b.hub
	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range h.subs[topic] {
		s.deliver(append([]byte(nil), data...))
	}
	return nil
}

// Subscribe registers a subscriber on topic until ctx ends or it is
// closed.
func (b *Bus) Subscribe(ctx context.Context, topic string) (fanout.Subscription, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, errClosed
	}
	s := &subscription{bus: b, topic: topic, c: make(chan []byte, buffer)}
	b.own[s] = struct{}{}
	h := b.hub
	h.mu.Lock()
	if h.subs[topic] == nil {
		h.subs[topic] = map[*subscription]struct{}{}
	}
	h.subs[topic][s] = struct{}{}
	s.stop = context.AfterFunc(ctx, func() { s.Close() })
	h.mu.Unlock()
	return s, nil
}

// Close ends every subscription made through this Bus.
func (b *Bus) Close() error {
	b.mu.Lock()
	b.closed = true
	subs := make([]*subscription, 0, len(b.own))
	for s := range b.own {
		subs = append(subs, s)
	}
	b.mu.Unlock()
	for _, s := range subs {
		s.Close()
	}
	return nil
}

type subscription struct {
	bus   *Bus
	topic string
	c     chan []byte
	stop  func() bool
	once  sync.Once
}

func (s *subscription) C() <-chan []byte { return s.c }

// deliver is called with the hub's lock held, which is also what Close
// takes before closing c, so a send never meets a closed channel.
func (s *subscription) deliver(data []byte) {
	select {
	case s.c <- data:
	default:
		// The subscriber is behind: drop rather than slow the publisher.
	}
}

func (s *subscription) Close() error {
	// The two locks are taken one after the other, never nested, so
	// Subscribe holding both in order cannot deadlock with this.
	s.once.Do(func() {
		s.bus.mu.Lock()
		delete(s.bus.own, s)
		s.bus.mu.Unlock()
		h := s.bus.hub
		h.mu.Lock()
		delete(h.subs[s.topic], s)
		if len(h.subs[s.topic]) == 0 {
			delete(h.subs, s.topic)
		}
		close(s.c)
		stop := s.stop
		h.mu.Unlock()
		stop()
	})
	return nil
}
