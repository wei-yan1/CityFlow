package service

import (
	"context"
	"log"
	"time"

	"cityflow/internal/application/repository"
	"cityflow/internal/platform/messaging"
)

// OutboxRelay drains a service-owned outbox and publishes each row to the
// broker. Publishing is at-least-once: a crash between broker acceptance and
// the PUBLISHED update republishes the same event ID, and consumers deduplicate.
type OutboxRelay struct {
	store       repository.OutboxStore
	publisher   messaging.Publisher
	interval    time.Duration
	batch       int
	lastPurgeAt time.Time
}

const (
	outboxPurgeEvery     = 5 * time.Minute
	outboxRetention      = 7 * 24 * time.Hour
	outboxPurgeBatch     = 1000
	outboxPurgeMaxPasses = 5
)

func NewOutboxRelay(store repository.OutboxStore, publisher messaging.Publisher, interval time.Duration, batch int) *OutboxRelay {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	if batch <= 0 || batch > 500 {
		batch = 50
	}
	return &OutboxRelay{store: store, publisher: publisher, interval: interval, batch: batch}
}

func (r *OutboxRelay) Run(ctx context.Context) {
	if r == nil || r.store == nil || r.publisher == nil {
		return
	}
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := r.RelayOnce(ctx); err != nil && ctx.Err() == nil {
				log.Printf("outbox relay: %v", err)
			}
			r.purgeIfDue(ctx)
		}
	}
}

// purgeIfDue keeps the outbox bounded. PUBLISHED rows are no longer needed by
// the relay once every consumer has had a reasonable replay window, so a small
// batch is deleted periodically instead of letting the table grow forever.
func (r *OutboxRelay) purgeIfDue(ctx context.Context) {
	if r == nil || r.store == nil {
		return
	}
	now := time.Now().UTC()
	if !r.lastPurgeAt.IsZero() && now.Sub(r.lastPurgeAt) < outboxPurgeEvery {
		return
	}
	r.lastPurgeAt = now
	before := now.Add(-outboxRetention)
	var total int64
	for pass := 0; pass < outboxPurgeMaxPasses; pass++ {
		deleted, err := r.store.PurgePublishedOutbox(ctx, before, outboxPurgeBatch)
		if err != nil {
			log.Printf("outbox purge: %v", err)
			return
		}
		total += deleted
		if deleted < int64(outboxPurgeBatch) {
			break
		}
	}
	if total > 0 {
		log.Printf("outbox purge: deleted %d published rows older than %s", total, outboxRetention)
	}
}

// RelayOnce claims one batch, publishes each event and records the outcome.
// A publish failure is isolated to its row so one bad event cannot block the
// rest of the outbox.
func (r *OutboxRelay) RelayOnce(ctx context.Context) (int, error) {
	if r == nil || r.store == nil || r.publisher == nil {
		return 0, nil
	}
	events, err := r.store.ClaimPendingOutbox(ctx, time.Now().UTC(), r.batch)
	if err != nil {
		return 0, err
	}
	published := 0
	for _, event := range events {
		msg := messaging.Message{
			ID: event.ID, Type: event.Type, AggregateID: event.AggregateID,
			Payload: append([]byte(nil), event.Payload...), OccurredAt: event.CreatedAt,
		}
		if err := r.publisher.Publish(ctx, msg); err != nil {
			if markErr := r.store.MarkOutboxFailed(ctx, event.ID, truncateOutboxError(err), retryAt(event.Attempts)); markErr != nil {
				log.Printf("outbox mark failed %s: %v", event.ID, markErr)
			}
			continue
		}
		if err := r.store.MarkOutboxPublished(ctx, event.ID, time.Now().UTC()); err != nil {
			log.Printf("outbox mark published %s: %v", event.ID, err)
			continue
		}
		published++
	}
	return published, nil
}

func retryAt(attempts int) time.Time {
	delay := 5 * time.Second
	for i := 0; i < attempts && delay < 5*time.Minute; i++ {
		delay *= 2
	}
	if delay > 5*time.Minute {
		delay = 5 * time.Minute
	}
	return time.Now().UTC().Add(delay)
}

func truncateOutboxError(err error) string {
	const limit = 240
	text := err.Error()
	if len(text) <= limit {
		return text
	}
	return text[:limit]
}
