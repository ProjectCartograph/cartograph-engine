// Package postgres is the fanout.Bus over Postgres LISTEN/NOTIFY, so a
// deployment whose state is in Postgres needs no second service for
// fan-out (docs/adr/0008).
//
// Each Bus holds one connection of its own that listens on one channel,
// whatever the number of topics or subscribers: the topic travels in the
// payload and the Bus delivers by it. When that connection drops, the
// Bus reconnects and listens again. Messages sent while it was away are
// lost, which the port allows: the engine treats every message as a
// hint and converges without it.
package postgres

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/fanout"
)

// Channel is the one NOTIFY channel every replica listens on.
const Channel = "cartograph_fanout"

// buffer is how many messages a subscriber may fall behind by before it
// loses them.
const buffer = 64

// maxBackoff caps the wait between attempts to reconnect the listener.
const maxBackoff = 5 * time.Second

var errClosed = errors.New("fanout: bus closed")

// Bus is one replica's connection to the fan-out.
type Bus struct {
	pool   *pgxpool.Pool
	config *pgx.ConnConfig
	cancel context.CancelFunc
	done   chan struct{} // closed when the listener has stopped

	mu     sync.Mutex
	closed bool
	subs   map[string]map[*subscription]struct{}
}

var _ fanout.Bus = (*Bus)(nil)

// New publishes through pool and listens on a connection of its own,
// configured as the pool's connections are. It returns once that
// connection is listening, so a message published after New returns
// reaches this Bus's subscribers.
func New(ctx context.Context, pool *pgxpool.Pool) (*Bus, error) {
	b := &Bus{
		pool:   pool,
		config: pool.Config().ConnConfig.Copy(),
		done:   make(chan struct{}),
		subs:   map[string]map[*subscription]struct{}{},
	}
	conn, err := b.listen(ctx)
	if err != nil {
		return nil, fmt.Errorf("fanout: listen on %s: %w", Channel, err)
	}
	// The listener outlives the caller's ctx; Close ends it.
	lctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	b.cancel = cancel
	go b.run(lctx, conn)
	return b, nil
}

func (b *Bus) listen(ctx context.Context) (*pgx.Conn, error) {
	conn, err := pgx.ConnectConfig(ctx, b.config)
	if err != nil {
		return nil, err
	}
	if _, err := conn.Exec(ctx, "LISTEN "+Channel); err != nil {
		conn.Close(context.Background())
		return nil, err
	}
	return conn, nil
}

// run receives notifications until ctx ends, reconnecting with a
// growing pause when the connection fails.
func (b *Bus) run(ctx context.Context, conn *pgx.Conn) {
	defer close(b.done)
	backoff := 100 * time.Millisecond
	for {
		n, err := conn.WaitForNotification(ctx)
		if err == nil {
			backoff = 100 * time.Millisecond
			b.dispatch(n.Payload)
			continue
		}
		conn.Close(context.Background())
		if ctx.Err() != nil {
			return
		}
		slog.Warn("fanout: listening connection lost, reconnecting", "err", err)
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff = min(2*backoff, maxBackoff)
			if conn, err = b.listen(ctx); err == nil {
				slog.Info("fanout: listening again", "channel", Channel)
				break
			}
			if ctx.Err() != nil {
				return
			}
			slog.Warn("fanout: reconnect failed", "err", err, "retry", backoff.String())
		}
	}
}

// encode puts the topic and the data in one NOTIFY payload. A payload is
// text, so the data goes as base64, which has no spaces; the last space
// therefore ends the topic, whatever the topic holds.
func encode(topic string, data []byte) string {
	return topic + " " + base64.StdEncoding.EncodeToString(data)
}

func decode(payload string) (string, []byte, bool) {
	i := strings.LastIndexByte(payload, ' ')
	if i < 0 {
		return "", nil, false
	}
	data, err := base64.StdEncoding.DecodeString(payload[i+1:])
	if err != nil {
		return "", nil, false
	}
	return payload[:i], data, true
}

// Publish sends data to topic's subscribers on every replica.
func (b *Bus) Publish(ctx context.Context, topic string, data []byte) error {
	if len(data) > fanout.MaxPayload || len(topic) > fanout.MaxTopic {
		return fanout.ErrTooLarge
	}
	// A payload is text in the database's encoding, and cannot hold a
	// zero byte.
	if !utf8.ValidString(topic) || strings.IndexByte(topic, 0) >= 0 {
		return fmt.Errorf("fanout: topic %q is not text", topic)
	}
	b.mu.Lock()
	closed := b.closed
	b.mu.Unlock()
	if closed {
		return errClosed
	}
	if _, err := b.pool.Exec(ctx, `SELECT pg_notify($1, $2)`, Channel, encode(topic, data)); err != nil {
		return fmt.Errorf("fanout: publish to %s: %w", topic, err)
	}
	return nil
}

// dispatch hands a payload to its topic's subscribers without waiting
// for any of them.
func (b *Bus) dispatch(payload string) {
	topic, data, ok := decode(payload)
	if !ok {
		slog.Warn("fanout: ignoring a payload this adapter did not write", "channel", Channel)
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for s := range b.subs[topic] {
		select {
		case s.c <- append([]byte{}, data...):
		default:
			// Behind: drop rather than hold up every other subscriber.
		}
	}
}

// Subscribe delivers topic's messages until ctx ends or the
// subscription is closed.
func (b *Bus) Subscribe(ctx context.Context, topic string) (fanout.Subscription, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, errClosed
	}
	s := &subscription{bus: b, topic: topic, c: make(chan []byte, buffer)}
	if b.subs[topic] == nil {
		b.subs[topic] = map[*subscription]struct{}{}
	}
	b.subs[topic][s] = struct{}{}
	s.stop = context.AfterFunc(ctx, func() { s.Close() })
	return s, nil
}

// Close stops listening and ends every subscription.
func (b *Bus) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	var subs []*subscription
	for _, set := range b.subs {
		for s := range set {
			subs = append(subs, s)
		}
	}
	b.mu.Unlock()
	b.cancel()
	<-b.done
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

// Close removes the subscription and closes C under the Bus's lock,
// which dispatch also holds, so a send never meets a closed channel.
func (s *subscription) Close() error {
	s.once.Do(func() {
		b := s.bus
		b.mu.Lock()
		stop := s.stop
		delete(b.subs[s.topic], s)
		if len(b.subs[s.topic]) == 0 {
			delete(b.subs, s.topic)
		}
		close(s.c)
		b.mu.Unlock()
		stop()
	})
	return nil
}
