// Package mq contains the RabbitMQ implementation of the messaging contract.
// It deliberately stays small: durable topic exchange, persistent messages,
// consumer acknowledgements, bounded retries and a dead-letter queue.
package mq

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"cityflow/internal/platform/messaging"
)

const (
	defaultExchange   = "cityflow.events"
	defaultRetryLimit = 3
	// A Consumer handles deliveries sequentially. Keep the unacked window small
	// so multiple service instances share the queue instead of one instance
	// reserving ten messages while other instances are idle.
	defaultPrefetch = 2
	// Publishing only returns success after RabbitMQ confirms the delivery.
	// Without this wait the outbox could mark a row PUBLISHED even though the
	// broker never accepted it.
	publishConfirmTimeout = 10 * time.Second
	retryConfirmTimeout   = 5 * time.Second
	reconnectDelay        = 5 * time.Second
	headerRetryCount      = "x-cityflow-retry-count"
	headerAggregateID     = "x-cityflow-aggregate-id"
	messageContentType    = "application/json"
)

// Publisher is a lazy, reconnecting RabbitMQ topic publisher. A single instance
// can be shared by an outbox relay.
type Publisher struct {
	url      string
	exchange string

	mu   sync.Mutex
	conn *amqp.Connection
	ch   *amqp.Channel
}

func NewPublisher(url string) *Publisher {
	return NewPublisherWithExchange(url, defaultExchange)
}

func NewPublisherWithExchange(url, exchange string) *Publisher {
	return &Publisher{url: strings.TrimSpace(url), exchange: strings.TrimSpace(exchange)}
}

// Probe verifies that the broker is reachable and the exchange can be declared.
// It is useful at bootstrap when a service must decide between the durable
// outbox path and a local synchronous fallback.
func (p *Publisher) Probe(ctx context.Context) error {
	if p == nil || p.url == "" {
		return errors.New("rabbitmq url is empty")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.ensureLocked(); err != nil {
		return err
	}
	return ctx.Err()
}

func (p *Publisher) Publish(ctx context.Context, msg messaging.Message) error {
	if p == nil || p.url == "" {
		return errors.New("rabbitmq url is empty")
	}
	if strings.TrimSpace(msg.Type) == "" {
		return errors.New("message type is required")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.ensureLocked(); err != nil {
		return err
	}
	body := msg.Payload
	if len(body) == 0 {
		body = []byte("{}")
	}
	occurredAt := msg.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	dc, err := p.ch.PublishWithDeferredConfirmWithContext(ctx, p.exchange, msg.Type, false, false, amqp.Publishing{
		ContentType:  messageContentType,
		DeliveryMode: amqp.Persistent,
		MessageId:    msg.ID,
		Type:         msg.Type,
		Timestamp:    occurredAt,
		AppId:        "cityflow",
		Headers:      amqp.Table{headerAggregateID: msg.AggregateID},
		Body:         body,
	})
	if err != nil {
		p.resetLocked()
		return fmt.Errorf("publish %s: %w", msg.Type, err)
	}
	if dc == nil {
		p.resetLocked()
		return errors.New("publish failed: publisher confirms are not enabled")
	}
	confirmCtx, cancel := context.WithTimeout(ctx, publishConfirmTimeout)
	defer cancel()
	acked, err := dc.WaitContext(confirmCtx)
	if err != nil {
		// The outcome is unknown. Reset the channel so the outbox retries on a
		// fresh connection instead of guessing that the message was accepted.
		_ = p.resetLocked()
		return fmt.Errorf("confirm publish %s: %w", msg.Type, err)
	}
	if !acked {
		_ = p.resetLocked()
		return fmt.Errorf("publish %s was rejected by the broker", msg.Type)
	}
	return nil
}

func (p *Publisher) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.resetLocked()
}

func (p *Publisher) ensureLocked() error {
	if p.conn != nil && !p.conn.IsClosed() && p.ch != nil {
		return nil
	}
	conn, err := amqp.DialConfig(p.url, amqp.Config{
		Heartbeat: 30 * time.Second,
		Locale:    "en_US",
	})
	if err != nil {
		return fmt.Errorf("connect rabbitmq: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("open rabbitmq channel: %w", err)
	}
	if err := ch.ExchangeDeclare(p.exchange, "topic", true, false, false, false, nil); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return fmt.Errorf("declare exchange %s: %w", p.exchange, err)
	}
	if err := ch.Confirm(false); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return fmt.Errorf("enable publisher confirms on %s: %w", p.exchange, err)
	}
	p.conn, p.ch = conn, ch
	return nil
}

func (p *Publisher) resetLocked() error {
	var err error
	if p.ch != nil {
		err = p.ch.Close()
		p.ch = nil
	}
	if p.conn != nil {
		if closeErr := p.conn.Close(); err == nil {
			err = closeErr
		}
		p.conn = nil
	}
	if errors.Is(err, amqp.ErrClosed) {
		return nil
	}
	return err
}

// Consumer subscribes a durable queue to one or more routing keys. Failed
// handlers are republished with an incremented retry header; after the retry
// limit the delivery is rejected into the queue's dead-letter queue.
type Consumer struct {
	url      string
	exchange string
	queue    string
	bindings []string
	retries  int
}

func NewConsumer(url, queue string, bindings ...string) *Consumer {
	return NewConsumerWithExchange(url, defaultExchange, queue, bindings...)
}

func NewConsumerWithExchange(url, exchange, queue string, bindings ...string) *Consumer {
	return &Consumer{
		url: strings.TrimSpace(url), exchange: strings.TrimSpace(exchange),
		queue: strings.TrimSpace(queue), bindings: append([]string(nil), bindings...),
		retries: defaultRetryLimit,
	}
}

func (c *Consumer) WithRetryLimit(limit int) *Consumer {
	if limit > 0 {
		c.retries = limit
	}
	return c
}

// Consume blocks and reconnects until ctx is cancelled. Queue setup failures are
// logged and retried because the broker may simply be starting.
func (c *Consumer) Consume(ctx context.Context, _ string, _ []string, handler messaging.Handler) error {
	if c == nil || c.url == "" {
		return errors.New("rabbitmq url is empty")
	}
	if handler == nil {
		return errors.New("message handler is required")
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		if err := c.consumeOnce(ctx, handler); err != nil && ctx.Err() == nil {
			log.Printf("mq consumer %s: %v; reconnecting in %s", c.queue, err, reconnectDelay)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(reconnectDelay):
		}
	}
}

func (c *Consumer) consumeOnce(ctx context.Context, handler messaging.Handler) error {
	conn, err := amqp.DialConfig(c.url, amqp.Config{
		Heartbeat: 30 * time.Second,
		Locale:    "en_US",
	})
	if err != nil {
		return fmt.Errorf("connect rabbitmq: %w", err)
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("open rabbitmq channel: %w", err)
	}
	defer ch.Close()
	if err := ch.Confirm(false); err != nil {
		return fmt.Errorf("enable publisher confirms: %w", err)
	}
	if err := ch.Qos(defaultPrefetch, 0, false); err != nil {
		return fmt.Errorf("set qos: %w", err)
	}
	if err := ch.ExchangeDeclare(c.exchange, "topic", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare exchange %s: %w", c.exchange, err)
	}
	if err := c.declareQueue(ch); err != nil {
		return err
	}
	for _, binding := range c.bindings {
		if strings.TrimSpace(binding) == "" {
			continue
		}
		if err := ch.QueueBind(c.queue, binding, c.exchange, false, nil); err != nil {
			return fmt.Errorf("bind %s to %s: %w", c.queue, binding, err)
		}
	}
	deliveries, err := ch.Consume(c.queue, "", false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("consume %s: %w", c.queue, err)
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case delivery, ok := <-deliveries:
			if !ok {
				return errors.New("rabbitmq delivery channel closed")
			}
			c.handleDelivery(ctx, ch, delivery, handler)
		}
	}
}

func (c *Consumer) declareQueue(ch *amqp.Channel) error {
	dlx := c.exchange + ".dlx"
	dlq := c.queue + ".dlq"
	if err := ch.ExchangeDeclare(dlx, "direct", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare dead-letter exchange: %w", err)
	}
	if _, err := ch.QueueDeclare(dlq, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare dead-letter queue: %w", err)
	}
	if err := ch.QueueBind(dlq, c.queue+".dead", dlx, false, nil); err != nil {
		return fmt.Errorf("bind dead-letter queue: %w", err)
	}
	args := amqp.Table{
		"x-dead-letter-exchange":    dlx,
		"x-dead-letter-routing-key": c.queue + ".dead",
	}
	if _, err := ch.QueueDeclare(c.queue, true, false, false, false, args); err != nil {
		return fmt.Errorf("declare queue %s: %w", c.queue, err)
	}
	return nil
}

func (c *Consumer) handleDelivery(ctx context.Context, ch *amqp.Channel, delivery amqp.Delivery, handler messaging.Handler) {
	msg := messaging.Message{
		ID:          delivery.MessageId,
		Type:        delivery.Type,
		AggregateID: headerString(delivery.Headers, headerAggregateID),
		Payload:     append([]byte(nil), delivery.Body...),
		OccurredAt:  delivery.Timestamp,
	}
	if msg.Type == "" {
		msg.Type = delivery.RoutingKey
	}
	if msg.ID == "" {
		msg.ID = delivery.MessageId
	}
	handlerCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	err := handler(handlerCtx, msg)
	cancel()
	if err == nil {
		if ackErr := delivery.Ack(false); ackErr != nil {
			log.Printf("mq consumer %s: ack: %v", c.queue, ackErr)
		}
		return
	}
	log.Printf("mq consumer %s: handle %s (%s): %v", c.queue, msg.Type, msg.ID, err)
	retry := retryCount(delivery.Headers)
	if retry < c.retries {
		headers := cloneHeaders(delivery.Headers)
		headers[headerRetryCount] = int32(retry + 1)
		publishErr := error(nil)
		dc, sendErr := ch.PublishWithDeferredConfirmWithContext(ctx, "", c.queue, false, false, amqp.Publishing{
			ContentType:  delivery.ContentType,
			DeliveryMode: amqp.Persistent,
			MessageId:    delivery.MessageId,
			Type:         delivery.Type,
			Timestamp:    delivery.Timestamp,
			AppId:        delivery.AppId,
			Headers:      headers,
			Body:         delivery.Body,
		})
		if sendErr != nil {
			publishErr = sendErr
		} else if dc == nil {
			publishErr = errors.New("retry publish: publisher confirms are not enabled")
		} else {
			confirmCtx, cancel := context.WithTimeout(ctx, retryConfirmTimeout)
			acked, waitErr := dc.WaitContext(confirmCtx)
			cancel()
			if waitErr != nil {
				publishErr = waitErr
			} else if !acked {
				publishErr = errors.New("retry publish was rejected by the broker")
			}
		}
		if publishErr == nil {
			if ackErr := delivery.Ack(false); ackErr != nil {
				log.Printf("mq consumer %s: ack after retry: %v", c.queue, ackErr)
			}
			return
		}
		log.Printf("mq consumer %s: republish retry: %v", c.queue, publishErr)
	}
	if nackErr := delivery.Nack(false, false); nackErr != nil {
		log.Printf("mq consumer %s: nack to dlq: %v", c.queue, nackErr)
	}
}

func retryCount(headers amqp.Table) int {
	value, ok := headers[headerRetryCount]
	if !ok {
		return 0
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int8:
		return int(typed)
	case int16:
		return int(typed)
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case uint8:
		return int(typed)
	case uint16:
		return int(typed)
	case uint32:
		return int(typed)
	case uint64:
		return int(typed)
	}
	return 0
}

func headerString(headers amqp.Table, key string) string {
	if value, ok := headers[key]; ok {
		if text, ok := value.(string); ok {
			return text
		}
	}
	return ""
}

func cloneHeaders(headers amqp.Table) amqp.Table {
	if len(headers) == 0 {
		return amqp.Table{}
	}
	out := make(amqp.Table, len(headers))
	for key, value := range headers {
		out[key] = value
	}
	return out
}

func (c *Consumer) Close() error { return nil }
