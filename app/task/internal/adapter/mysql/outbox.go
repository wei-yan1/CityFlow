package taskmysql

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"cityflow/app/task/domain/task"
	"cityflow/app/task/repository"
	"cityflow/internal/common/outbox"
)

const (
	taskOutboxTable = "task_outbox"
	outboxLease     = 30 * time.Second
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

func (outboxRecord) TableName() string { return taskOutboxTable }

// OutboxStore adapts the Task-owned outbox table and the transactional
// status writer used by Task's application service.
type OutboxStore struct{ Store *Store }

var _ repository.TaskOutboxRepository = OutboxStore{}

func (o OutboxStore) UpdateStatusWithOutbox(ctx context.Context, id, next, actorID string) (*task.Task, error) {
	return o.Store.updateStatus(ctx, id, next, actorID, "", true)
}

func (o OutboxStore) CancelWithOutbox(ctx context.Context, id, actorID, reason string) (*task.Task, error) {
	return o.Store.updateStatus(ctx, id, task.StatusCancelled, actorID, reason, true)
}

func (o OutboxStore) ClaimPendingOutbox(ctx context.Context, now time.Time, limit int) ([]outbox.Event, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	var rows []outboxRecord
	err := o.Store.db.ORM.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Table(taskOutboxTable).
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
		return tx.Table(taskOutboxTable).Where("id IN ?", ids).Updates(map[string]any{
			"status": outbox.StatusPublishing, "locked_until": lockUntil,
		}).Error
	})
	if err != nil {
		return nil, fmt.Errorf("claim %s: %w", taskOutboxTable, err)
	}
	events := make([]outbox.Event, 0, len(rows))
	for _, row := range rows {
		events = append(events, outbox.Event{
			ID: row.ID, Type: row.EventType, AggregateID: row.AggregateID,
			Payload: []byte(row.Payload), Status: outbox.StatusPublishing,
			Attempts: row.Attempts, AvailableAt: row.AvailableAt, LockedUntil: row.LockedUntil,
			PublishedAt: row.PublishedAt, LastError: stringValue(row.LastError), CreatedAt: row.CreatedAt,
		})
	}
	return events, nil
}

func (o OutboxStore) MarkOutboxPublished(ctx context.Context, id string, at time.Time) error {
	return o.Store.db.ORM.WithContext(ctx).Table(taskOutboxTable).Where("id = ?", id).Updates(map[string]any{
		"status": outbox.StatusPublished, "published_at": at, "locked_until": nil, "last_error": nil,
	}).Error
}

func (o OutboxStore) MarkOutboxFailed(ctx context.Context, id, reason string, next time.Time) error {
	result := o.Store.db.ORM.WithContext(ctx).Table(taskOutboxTable).Where("id = ?", id).Updates(map[string]any{
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

func (o OutboxStore) PurgePublishedOutbox(ctx context.Context, before time.Time, limit int) (int64, error) {
	if limit <= 0 || limit > 10000 {
		limit = 1000
	}
	result := o.Store.db.ORM.WithContext(ctx).Exec(
		"DELETE FROM "+taskOutboxTable+" WHERE status = ? AND published_at IS NOT NULL AND published_at < ? LIMIT ?",
		outbox.StatusPublished, before, limit,
	)
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

func truncateReason(reason string) string {
	const limit = 255
	if len(reason) <= limit {
		return reason
	}
	return reason[:limit]
}
