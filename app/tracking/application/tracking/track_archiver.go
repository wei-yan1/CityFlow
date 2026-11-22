package trackingservice

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"cityflow/app/tracking/repository"
	"cityflow/internal/common/location"
	"cityflow/internal/platform/messaging"
)

// TrackArchiveTopic is the durable RabbitMQ routing key for trajectory
// batches. The producer reads pending Redis samples; the consumer writes them
// to MySQL and then removes the exact members from Redis.
const TrackArchiveTopic = "rider.track.batch.v1"

// TrackBatchPayload is the wire contract between Tracking's archiver and its
// MySQL consumer. Member is the exact Redis ZSET payload, which lets the
// consumer delete only points that were committed.
type TrackBatchPayload struct {
	RiderID string            `json:"rider_id"`
	Points  []TrackBatchPoint `json:"points"`
}

type TrackBatchPoint struct {
	Member   string            `json:"member"`
	Position location.Position `json:"position"`
}

// TrackArchiver periodically publishes pending Redis trajectory batches. The
// Redis cursor advances only after RabbitMQ confirms the publish; the consumer
// removes Redis members only after the MySQL transaction commits.
type TrackArchiver struct {
	Queue     repository.TrackQueue
	Publisher messaging.Publisher
	Interval  time.Duration
	BatchSize int
	LockTTL   time.Duration

	rewindMu sync.Mutex
	rewinds  map[string]time.Time
}

func NewTrackArchiver(queue repository.TrackQueue, publisher messaging.Publisher) *TrackArchiver {
	return &TrackArchiver{Queue: queue, Publisher: publisher}
}

func (a *TrackArchiver) Run(ctx context.Context) {
	if a == nil || a.Queue == nil || a.Publisher == nil {
		return
	}
	interval := a.Interval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	if err := a.Queue.BackfillPendingRiders(ctx); err != nil && ctx.Err() == nil {
		log.Printf("track archiver: register legacy pending riders: %v", err)
	}
	if err := a.flush(ctx); err != nil && ctx.Err() == nil {
		log.Printf("track archiver: %v", err)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := a.flush(ctx); err != nil && ctx.Err() == nil {
				log.Printf("track archiver: %v", err)
			}
		}
	}
}

func (a *TrackArchiver) flush(ctx context.Context) error {
	lockTTL := a.LockTTL
	if lockTTL <= 0 {
		lockTTL = 60 * time.Second
	}
	token, acquired, err := a.Queue.AcquireArchiverLock(ctx, lockTTL)
	if err != nil {
		return fmt.Errorf("acquire archiver lock: %w", err)
	}
	if !acquired {
		return nil
	}
	defer func() { _ = a.Queue.ReleaseArchiverLock(ctx, token) }()

	riders, err := a.Queue.PendingRiders(ctx)
	if err != nil {
		return fmt.Errorf("list pending riders: %w", err)
	}
	batchSize := a.BatchSize
	if batchSize <= 0 {
		batchSize = 100
	}
	for _, riderID := range riders {
		if err := ctx.Err(); err != nil {
			return nil
		}
		cursor, err := a.Queue.TrackCursor(ctx, riderID)
		if err != nil {
			return fmt.Errorf("read cursor for %s: %w", riderID, err)
		}
		cursor, err = a.rewindStaleCursor(ctx, riderID, cursor)
		if err != nil {
			return fmt.Errorf("rewind cursor for %s: %w", riderID, err)
		}
		batch, err := a.Queue.PendingTrackBatch(ctx, riderID, cursor, batchSize)
		if err != nil {
			log.Printf("track archiver: read batch for %s: %v", riderID, err)
			continue
		}
		if len(batch.Points) == 0 {
			_, _ = a.Queue.ForgetRiderIfEmpty(ctx, riderID)
			continue
		}
		payload, err := json.Marshal(TrackBatchPayload{RiderID: riderID, Points: batchPoints(batch)})
		if err != nil {
			return fmt.Errorf("encode batch for %s: %w", riderID, err)
		}
		messageID := fmt.Sprintf("track.batch:%s:%d-%d", riderID, batch.Points[0].ID, batch.LastVersion)
		if err := a.Publisher.Publish(ctx, messaging.Message{
			ID: messageID, Type: TrackArchiveTopic, AggregateID: riderID,
			Payload: payload, OccurredAt: time.Now().UTC(),
		}); err != nil {
			return fmt.Errorf("publish batch for %s: %w", riderID, err)
		}
		if err := a.Queue.SetTrackCursor(ctx, riderID, batch.LastVersion); err != nil {
			return fmt.Errorf("advance cursor for %s: %w", riderID, err)
		}
	}
	return nil
}

func batchPoints(batch repository.TrackBatch) []TrackBatchPoint {
	out := make([]TrackBatchPoint, 0, len(batch.Points))
	for index, point := range batch.Points {
		member := ""
		if index < len(batch.Members) {
			member = batch.Members[index]
		}
		out = append(out, TrackBatchPoint{Member: member, Position: point.Position})
	}
	return out
}

// rewindStaleCursor recovers points that were published but never archived,
// for example because the consumer queue was unavailable or a delivery ended
// in the DLQ. A point that is still pending after the cursor has advanced past
// it is republished once it is old enough to avoid duplicates during normal
// consumer lag. The MySQL unique key makes replayed batches idempotent.
func (a *TrackArchiver) rewindStaleCursor(ctx context.Context, riderID string, cursor uint64) (uint64, error) {
	if cursor == 0 {
		return 0, nil
	}
	if !a.rewindAllowed(riderID, time.Now()) {
		return cursor, nil
	}
	oldest, err := a.Queue.PendingTrackBatch(ctx, riderID, 0, 1)
	if err != nil {
		return 0, err
	}
	if len(oldest.Points) == 0 || oldest.LastVersion > cursor {
		return cursor, nil
	}
	observedAt := oldest.Points[0].UpdatedAt
	if observedAt.IsZero() {
		observedAt = oldest.Points[0].ObservedAt
	}
	if observedAt.IsZero() || time.Since(observedAt) < 2*time.Minute {
		return cursor, nil
	}
	rewind := oldest.LastVersion
	if rewind > 0 {
		rewind--
	}
	if err := a.Queue.SetTrackCursor(ctx, riderID, rewind); err != nil {
		return 0, err
	}
	a.markRewind(riderID, time.Now())
	return rewind, nil
}

// rewindAllowed bounds replay attempts for one rider. Without it, an
// unreachable consumer would cause the same stale batch to be republished on
// every archiver tick, amplifying queue volume during an outage.
func (a *TrackArchiver) rewindAllowed(riderID string, now time.Time) bool {
	a.rewindMu.Lock()
	defer a.rewindMu.Unlock()
	if last, ok := a.rewinds[riderID]; ok && now.Sub(last) < 5*time.Minute {
		return false
	}
	return true
}

func (a *TrackArchiver) markRewind(riderID string, now time.Time) {
	a.rewindMu.Lock()
	defer a.rewindMu.Unlock()
	if a.rewinds == nil {
		a.rewinds = make(map[string]time.Time)
	}
	a.rewinds[riderID] = now
}
