// Package conformance is the suite every fanout.Bus adapter must pass.
// It proves what the engine relies on: a message reaches subscribers on
// every replica, the publisher's own included; topics do not leak into
// each other; a slow subscriber costs only its own messages; and closing
// ends what it should.
//
// Delivery is best effort, so the suite never asserts that every one of
// many messages arrives, only that a message sent to an attentive
// subscriber does.
package conformance

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/internal/fanout"
)

// wait is how long a message may take to arrive. A database round trip
// on a loaded CI runner is well inside it.
const wait = 5 * time.Second

// quiet is how long the suite listens to be satisfied that nothing came.
const quiet = 300 * time.Millisecond

// Run exercises an adapter. Every Bus newBus returns must share one
// backend, as two replicas share one database or one broker; newBus
// closes the Bus when the test ends. Topics are unique per test, so a
// backend may be shared with other tests running at the same time.
func Run(t *testing.T, newBus func(t *testing.T) fanout.Bus) {
	t.Helper()
	ctx := context.Background()

	t.Run("delivers to subscribers on every replica", func(t *testing.T) {
		a, b := newBus(t), newBus(t)
		topic := uniqueTopic(t)
		sa := subscribe(t, a, topic)
		sb := subscribe(t, b, topic)
		must(t, a.Publish(ctx, topic, []byte("doc changed")))
		expect(t, sa, []byte("doc changed"))
		expect(t, sb, []byte("doc changed"))
	})

	t.Run("the publisher receives its own message", func(t *testing.T) {
		a := newBus(t)
		topic := uniqueTopic(t)
		s := subscribe(t, a, topic)
		must(t, a.Publish(ctx, topic, []byte("mine")))
		expect(t, s, []byte("mine"))
	})

	t.Run("topics are isolated", func(t *testing.T) {
		a, b := newBus(t), newBus(t)
		one, two := uniqueTopic(t), uniqueTopic(t)
		s1 := subscribe(t, b, one)
		s2 := subscribe(t, b, two)
		must(t, a.Publish(ctx, one, []byte("for one")))
		expect(t, s1, []byte("for one"))
		nothing(t, s2)
	})

	t.Run("binary data arrives intact at the largest size", func(t *testing.T) {
		a, b := newBus(t), newBus(t)
		topic := uniqueTopic(t)
		topic += strings.Repeat("x", fanout.MaxTopic-len(topic))
		s := subscribe(t, b, topic)
		data := make([]byte, fanout.MaxPayload)
		for i := range data {
			data[i] = byte(i)
		}
		must(t, a.Publish(ctx, topic, data))
		expect(t, s, data)
		must(t, a.Publish(ctx, topic, nil))
		expect(t, s, []byte{})
	})

	t.Run("a payload or topic over the limit is refused", func(t *testing.T) {
		a := newBus(t)
		topic := uniqueTopic(t)
		err := a.Publish(ctx, topic, make([]byte, fanout.MaxPayload+1))
		if !errors.Is(err, fanout.ErrTooLarge) {
			t.Fatalf("payload over MaxPayload: got %v, want ErrTooLarge", err)
		}
		err = a.Publish(ctx, strings.Repeat("t", fanout.MaxTopic+1), []byte("x"))
		if !errors.Is(err, fanout.ErrTooLarge) {
			t.Fatalf("topic over MaxTopic: got %v, want ErrTooLarge", err)
		}
	})

	t.Run("closing a subscription stops delivery to it only", func(t *testing.T) {
		a, b := newBus(t), newBus(t)
		topic := uniqueTopic(t)
		gone := subscribe(t, b, topic)
		stays := subscribe(t, b, topic)
		must(t, gone.Close())
		closed(t, gone)
		must(t, a.Publish(ctx, topic, []byte("after")))
		expect(t, stays, []byte("after"))
	})

	t.Run("a subscription ends with its context", func(t *testing.T) {
		a := newBus(t)
		sctx, cancel := context.WithCancel(ctx)
		s, err := a.Subscribe(sctx, uniqueTopic(t))
		must(t, err)
		cancel()
		closed(t, s)
	})

	t.Run("a slow subscriber does not block publishers", func(t *testing.T) {
		a, b := newBus(t), newBus(t)
		topic := uniqueTopic(t)
		_ = subscribe(t, b, topic) // never read
		fast := subscribe(t, b, topic)
		got := make(chan struct{})
		go func() {
			for m := range fast.C() {
				if string(m) == "end" {
					close(got)
					return
				}
			}
		}()
		const n = 300
		start := time.Now()
		for i := 0; i < n; i++ {
			must(t, a.Publish(ctx, topic, []byte("burst")))
		}
		if d := time.Since(start); d > wait {
			t.Fatalf("%d publishes took %v with a subscriber not reading", n, d)
		}
		// Even the attentive subscriber may have lost some of a burst
		// that fast; what matters is that it still hears what follows.
		tick := time.NewTicker(50 * time.Millisecond)
		defer tick.Stop()
		deadline := time.After(wait)
		for {
			must(t, a.Publish(ctx, topic, []byte("end")))
			select {
			case <-got:
				return
			case <-tick.C:
			case <-deadline:
				t.Fatal("an attentive subscriber stopped hearing messages after a burst")
			}
		}
	})

	t.Run("closing the bus ends its subscriptions", func(t *testing.T) {
		a := newBus(t)
		s1 := subscribe(t, a, uniqueTopic(t))
		s2 := subscribe(t, a, uniqueTopic(t))
		must(t, a.Close())
		closed(t, s1)
		closed(t, s2)
	})
}

func uniqueTopic(t *testing.T) string {
	t.Helper()
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return "test." + hex.EncodeToString(b)
}

func subscribe(t *testing.T, b fanout.Bus, topic string) fanout.Subscription {
	t.Helper()
	s, err := b.Subscribe(context.Background(), topic)
	must(t, err)
	t.Cleanup(func() { s.Close() })
	return s
}

func expect(t *testing.T, s fanout.Subscription, want []byte) {
	t.Helper()
	select {
	case got, ok := <-s.C():
		if !ok {
			t.Fatal("subscription closed before the message arrived")
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("got %d bytes %.40q, want %d bytes %.40q", len(got), got, len(want), want)
		}
	case <-time.After(wait):
		t.Fatalf("no message within %v", wait)
	}
}

func nothing(t *testing.T, s fanout.Subscription) {
	t.Helper()
	select {
	case got := <-s.C():
		t.Fatalf("expected nothing on this topic, got %q", got)
	case <-time.After(quiet):
	}
}

// closed waits for C to be closed, discarding anything still buffered.
func closed(t *testing.T, s fanout.Subscription) {
	t.Helper()
	deadline := time.After(wait)
	for {
		select {
		case _, ok := <-s.C():
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("subscription not closed")
		}
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
