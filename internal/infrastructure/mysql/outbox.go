package mysqlstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"cityflow/internal/application/repository"
	"cityflow/internal/domain/outbox"
	"cityflow/internal/domain/task"
)

const (
	taskOutboxTable     = "task_outbox"
	identityOutboxTable = "identity_outbox"
	outboxLease         = 30 * time.Second
)

type outboxRecord struct {
	ID          string     `gorm:"column:id;primaryKey"`
	EventType   string     `gorm:"column:event_type"`
	AggregateID string     `gorm:"column:aggregate_id"`
	Payload     string     `gorm:"column:payload"`
	Status      string     `gorm:"column:status"`
	Attempts    int        `gorm:"column:attempts"`
	AvailableAt time.Time  `gorm:"column:available_at"`
	LockedUntil *time.Time `gorm:"column:locked_until"`
	PublishedAt *time.Time `gorm:"column:published_at"`
	LastError   *string    `gorm:"column:last_error"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
}

func (r outboxRecord) TableName() string { return taskOutboxTable }

// TaskOutboxStore adapts the shared Store to the Task-owned outbox table and
// the task status writer that creates the outbox row.
type TaskOutboxStore struct{ Store *Store }

var _ repository.TaskOutboxRepository = TaskOutboxStore{}

func (o TaskOutboxStore) UpdateStatusWithOutbox(ctx context.Context, id, next, actorID string) (*task.Task, error) {
	return o.Store.updateStatus(ctx, id, next, actorID, true)
}

func (o TaskOutboxStore) ClaimPendingOutbox(ctx context.Context, now time.Time, limit int) ([]outbox.Event, error) {
	return o.Store.claimOutbox(ctx, taskOutboxTable, now, limit)
}

func (o TaskOutboxStore) MarkOutboxPublished(ctx context.Context, id string, at time.Time) error {
	return o.Store.markOutboxPublished(ctx, taskOutboxTable, id, at)
}

func (o TaskOutboxStore) MarkOutboxFailed(ctx context.Context, id, reason string, next time.Time) error {
	return o.Store.markOutboxFailed(ctx, taskOutboxTable, id, reason, next)
}

func (o TaskOutboxStore) PurgePublishedOutbox(ctx context.Context, before time.Time, limit int) (int64, error) {
	return o.Store.purgePublishedOutbox(ctx, taskOutboxTable, before, limit)
}

// IdentityOutboxStore is the same adapter for the Identity-owned table. Both
// tables live in their own service database, so the table name is explicit.
type IdentityOutboxStore struct{ Store *Store }

var _ repository.OutboxStore = IdentityOutboxStore{}

func (o IdentityOutboxStore) ClaimPendingOutbox(ctx context.Context, now time.Time, limit int) ([]outbox.Event, error) {
	return o.Store.claimOutbox(ctx, identityOutboxTable, now, limit)
}

func (o IdentityOutboxStore) MarkOutboxPublished(ctx context.Context, id string, at time.Time) error {
	return o.Store.markOutboxPublished(ctx, identityOutboxTable, id, at)
}

func (o IdentityOutboxStore) MarkOutboxFailed(ctx context.Context, id, reason string, next time.Time) error {
	return o.Store.markOutboxFailed(ctx, identityOutboxTable, id, reason, next)
}

func (o IdentityOutboxStore) PurgePublishedOutbox(ctx context.Context, before time.Time, limit int) (int64, error) {
	return o.Store.purgePublishedOutbox(ctx, identityOutboxTable, before, limit)
}

func (s *Store) claimOutbox(ctx context.Context, table string, now time.Time, limit int) ([]outbox.Event, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	var rows []outboxRecord
	err := s.orm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Table(table).
			Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("(status IN ? AND available_at <= ?) OR (status = ? AND locked_until <= ?)",
				[]string{outbox.StatusPending, outbox.StatusFailed}, now, outbox.StatusPublishing, now).
			Order("available_at").Order("created_at").Limit(limit)
		if err := query.Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		lockUntil := now.Add(outboxLease)
		ids := make([]string, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.ID)
		}
		return tx.Table(table).Where("id IN ?", ids).Updates(map[string]any{
			"status": outbox.StatusPublishing, "locked_until": lockUntil,
		}).Error
	})
	if err != nil {
		return nil, fmt.Errorf("claim %s: %w", table, err)
	}
	events := make([]outbox.Event, 0, len(rows))
	for _, row := range rows {
		event := outbox.Event{
			ID: row.ID, Type: row.EventType, AggregateID: row.AggregateID,
			Payload: []byte(row.Payload), Status: outbox.StatusPublishing,
			Attempts: row.Attempts, AvailableAt: row.AvailableAt, LockedUntil: row.LockedUntil,
			PublishedAt: row.PublishedAt, LastError: stringValue(row.LastError), CreatedAt: row.CreatedAt,
		}
		events = append(events, event)
	}
	return events, nil
}

func (s *Store) markOutboxPublished(ctx context.Context, table, id string, at time.Time) error {
	result := s.orm.WithContext(ctx).Table(table).Where("id = ?", id).Updates(map[string]any{
		"status": outbox.StatusPublished, "published_at": at, "locked_until": nil, "last_error": nil,
	})
	if result.Error != nil {
		return result.Error
	}
	return nil
}

func (s *Store) markOutboxFailed(ctx context.Context, table, id, reason string, next time.Time) error {
	result := s.orm.WithContext(ctx).Table(table).Where("id = ?", id).Updates(map[string]any{
		"status": outbox.StatusFailed, "attempts": gorm.Expr("attempts + 1"),
		"available_at": next, "locked_until": nil, "last_error": truncateReason(reason),
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("outbox row not found")
	}
	return nil
}

func truncateReason(reason string) string {
	const limit = 255
	if len(reason) <= limit {
		return reason
	}
	return reason[:limit]
}

func (s *Store) purgePublishedOutbox(ctx context.Context, table string, before time.Time, limit int) (int64, error) {
	if limit <= 0 || limit > 10000 {
		limit = 1000
	}
	result := s.orm.WithContext(ctx).Exec(
		"DELETE FROM "+table+" WHERE status = ? AND published_at IS NOT NULL AND published_at < ? LIMIT ?",
		outbox.StatusPublished, before, limit,
	)
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}
