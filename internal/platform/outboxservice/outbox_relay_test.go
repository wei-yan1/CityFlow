package outboxservice

import (
	"context"
	"errors"
	"testing"
	"time"

	"cityflow/internal/common/outbox"
	"cityflow/internal/platform/messaging"
)

type relayStore struct {
	events     []outbox.Event
	claimed    int
	published  []string
	failed     []string
	claimErr   error
	markErr    error
	nextFailed time.Time
}

func (s *relayStore) ClaimPendingOutbox(_ context.Context, _ time.Time, _ int) ([]outbox.Event, error) {
	s.claimed++
	return append([]outbox.Event(nil), s.events...), s.claimErr
}

func (s *relayStore) MarkOutboxPublished(_ context.Context, id string, _ time.Time) error {
	s.published = append(s.published, id)
	return s.markErr
}

func (s *relayStore) MarkOutboxFailed(_ context.Context, id, _ string, next time.Time) error {
	s.failed = append(s.failed, id)
	s.nextFailed = next
	return nil
}

func (s *relayStore) PurgePublishedOutbox(context.Context, time.Time, int) (int64, error) {
	return 0, nil
}

type relayPublisher struct {
	messages []messaging.Message
	err      error
}

func (p *relayPublisher) Publish(_ context.Context, msg messaging.Message) error {
	p.messages = append(p.messages, msg)
	return p.err
}

func (p *relayPublisher) Close() error { return nil }

func TestOutboxRelayPublishesAndMarksRows(t *testing.T) {
	store := &relayStore{events: []outbox.Event{
		{ID: "evt-1", Type: "task.completed.v1", AggregateID: "task-1", Payload: []byte(`{"id":"task-1"}`)},
		{ID: "evt-2", Type: "task.cancelled.v1", AggregateID: "task-2", Payload: []byte(`{"id":"task-2"}`)},
	}}
	publisher := &relayPublisher{}
	relay := NewOutboxRelay(store, publisher, time.Second, 10)
	count, err := relay.RelayOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 || len(publisher.messages) != 2 || len(store.published) != 2 {
		t.Fatalf("relay did not publish both rows: count=%d messages=%d published=%d", count, len(publisher.messages), len(store.published))
	}
	if publisher.messages[0].ID != "evt-1" || publisher.messages[1].Type != "task.cancelled.v1" {
		t.Fatalf("unexpected messages: %+v", publisher.messages)
	}
}

func TestOutboxRelayIsolatesPublishFailure(t *testing.T) {
	store := &relayStore{events: []outbox.Event{{ID: "evt-1", Type: "task.completed.v1"}}}
	publisher := &relayPublisher{err: errors.New("broker unavailable")}
	relay := NewOutboxRelay(store, publisher, time.Second, 10)
	if _, err := relay.RelayOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.failed) != 1 || len(store.published) != 0 {
		t.Fatalf("failed event must be marked failed, not published: failed=%v published=%v", store.failed, store.published)
	}
	if !store.nextFailed.After(time.Now()) {
		t.Fatalf("retry time must be in the future: %v", store.nextFailed)
	}
}
