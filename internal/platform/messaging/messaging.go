// Package messaging defines the transport-neutral event contract used by
// outbox publishers and consumers. The RabbitMQ implementation lives in
// internal/infrastructure/mq; application services depend only on these
// interfaces so they stay testable without a broker.
package messaging

import (
	"context"
	"time"
)

// Message is one durable integration event. ID is the idempotency key shared by
// the producer and consumer; Type is also used as the AMQP routing key.
type Message struct {
	ID          string
	Type        string
	AggregateID string
	Payload     []byte
	OccurredAt  time.Time
}

// Publisher publishes one message. It must return nil only after the broker has
// accepted the message; callers use that acknowledgement to mark an outbox row
// as published.
type Publisher interface {
	Publish(context.Context, Message) error
	Close() error
}

// Handler processes one delivered message. Returning an error asks the
// transport to retry and eventually move the message to its dead-letter queue.
type Handler func(context.Context, Message) error

// Consumer owns a durable queue and its bindings. Consume blocks until the
// context is cancelled or a fatal setup error occurs.
type Consumer interface {
	Consume(context.Context, string, []string, Handler) error
	Close() error
}
