// Package fanout is the port that carries a small message from one
// replica of the engine to every other (docs/adr/0008). The engine uses
// it for two things: "this document changed", so replicas with people
// connected to it sync them; and presence, relayed as it arrives.
//
// Delivery is at most once and best effort, and the engine treats every
// message as a hint. Convergence never depends on it: the sync protocol
// compares heads on every exchange, so a lost message costs latency,
// never an edit. That is what lets a deployment choose its adapter
// (Postgres LISTEN/NOTIFY by default; NATS, Redis streams or a cloud
// pub/sub in a distribution) by what it already runs.
package fanout

import (
	"context"
	"errors"
)

// MaxPayload is the largest message an adapter must carry. It is set by
// what the Postgres adapter can fit in one NOTIFY, whose payload must be
// under 8000 bytes of text: the topic (at most MaxTopic bytes), one
// separator, and the data in base64, which turns 3 bytes into 4. 5800
// bytes of data is 7736 characters of base64, and with the longest
// topic the payload is 7993. A message larger than this is the
// publisher's to drop or split, never the adapter's to truncate.
const MaxPayload = 5800

// MaxTopic is the longest topic, in bytes, an adapter must carry. A
// topic names one document or one channel of presence, so it is short.
const MaxTopic = 256

// ErrTooLarge is returned by Publish for a payload over MaxPayload or a
// topic over MaxTopic.
var ErrTooLarge = errors.New("fanout: payload over MaxPayload or topic over MaxTopic")

// Bus publishes to topics and delivers to subscribers on every replica,
// the publisher's own included.
type Bus interface {
	// Publish sends data to every current subscriber of topic. It
	// returns once the adapter has accepted the message, not when it is
	// delivered.
	Publish(ctx context.Context, topic string, data []byte) error
	// Subscribe delivers messages published to topic from now on until
	// ctx ends or the subscription is closed. A subscriber that falls
	// behind loses messages rather than slowing the publisher.
	Subscribe(ctx context.Context, topic string) (Subscription, error)
	Close() error
}

// Subscription is one listener.
type Subscription interface {
	// C delivers messages; it is closed when the subscription ends.
	C() <-chan []byte
	Close() error
}
