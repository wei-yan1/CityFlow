package outbox

import (
	"context"
	"time"
)

// Store is the relay view of a service-owned outbox table. Implementations
// must claim rows atomically: a row already being published by another relay
// must not be returned twice until its lease expires.
type Store interface {
	ClaimPendingOutbox(context.Context, time.Time, int) ([]Event, error)
	MarkOutboxPublished(context.Context, string, time.Time) error
	MarkOutboxFailed(context.Context, string, string, time.Time) error
	// PurgePublishedOutbox deletes a bounded batch of PUBLISHED rows older than
	// publishedBefore. Keeping published rows forever makes the outbox table and
	// its indexes grow without bound.
	PurgePublishedOutbox(context.Context, time.Time, int) (int64, error)
}
