package repository

import (
	"context"
	"time"

	"cityflow/internal/domain/outbox"
	"cityflow/internal/domain/task"
)

// OutboxStore is the generic relay view of a service-owned outbox table.
// Implementations must claim rows atomically: a row that is already being
// published by another relay must not be returned twice until its lease expires.
type OutboxStore interface {
	ClaimPendingOutbox(context.Context, time.Time, int) ([]outbox.Event, error)
	MarkOutboxPublished(context.Context, string, time.Time) error
	MarkOutboxFailed(context.Context, string, string, time.Time) error
	// PurgePublishedOutbox deletes a bounded batch of PUBLISHED rows older than
	// publishedBefore. Keeping published rows forever makes the outbox table and
	// its indexes grow without bound.
	PurgePublishedOutbox(context.Context, time.Time, int) (int64, error)
}

// TaskOutboxRepository is implemented by the Task MySQL store. It changes a
// task status and writes the corresponding integration event in one transaction.
type TaskOutboxRepository interface {
	OutboxStore
	UpdateStatusWithOutbox(context.Context, string, string, string) (*task.Task, error)
}

// InboxEvent is a durably stored task event that can be replayed after a
// consumer crashes between claiming the inbox row and completing the side
// effect. Payload is persisted at claim time, not only in the broker message.
type InboxEvent struct {
	EventID   string
	EventType string
	Payload   []byte
	Attempts  int
}

// PaymentInboxRepository makes task-event consumption idempotent. ClaimInbox
// returns true only for the caller that owns the event; duplicates are ignored
// unless the previous attempt is in FAILED state. The payload is stored on the
// first claim so an Inbox recovery worker can replay a PROCESSING row left
// behind by a crashed consumer.
type PaymentInboxRepository interface {
	ClaimInbox(context.Context, string, string, []byte) (bool, error)
	MarkInboxProcessed(context.Context, string, time.Time) error
	MarkInboxFailed(context.Context, string, string) error
	// MarkInboxRetrying keeps an event in PROCESSING for the recovery worker to
	// retry after the lease, instead of pretending the side effect succeeded.
	MarkInboxRetrying(context.Context, string, string, time.Time) error
	// ClaimRecoverableInbox leases a bounded batch of stale PROCESSING or FAILED
	// rows that still have a payload and have not exceeded maxAttempts.
	ClaimRecoverableInbox(context.Context, time.Time, int, int) ([]InboxEvent, error)
	// InboxStatus returns PROCESSING, PROCESSED, FAILED, or "" when the event
	// does not exist. Consumers use it to tell "already done" apart from "another
	// consumer is still working", which ClaimInbox alone cannot express.
	InboxStatus(context.Context, string) (string, error)
	// PurgeInbox deletes a bounded batch of PROCESSED rows older than
	// processedBefore.
	PurgeInbox(context.Context, time.Time, int) (int64, error)
}
