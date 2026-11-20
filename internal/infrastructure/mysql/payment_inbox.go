package mysqlstore

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"cityflow/internal/application/repository"
)

type paymentInboxRecord struct {
	EventID     string     `gorm:"column:event_id;primaryKey"`
	EventType   string     `gorm:"column:event_type"`
	Payload     []byte     `gorm:"column:payload"`
	Attempts    int        `gorm:"column:attempts"`
	Status      string     `gorm:"column:status"`
	ReceivedAt  time.Time  `gorm:"column:received_at"`
	ProcessedAt *time.Time `gorm:"column:processed_at"`
	LastError   *string    `gorm:"column:last_error"`
	UpdatedAt   time.Time  `gorm:"column:updated_at"`
}

func (paymentInboxRecord) TableName() string { return "payment_inbox" }

var _ repository.PaymentInboxRepository = (*Store)(nil)

// ClaimInbox acquires an event for processing. A brand-new row wins; a row left
// in FAILED can be reclaimed for an immediate transport retry. A PROCESSING row
// is returned to the caller as not claimed: the caller must not ACK it. If the
// original consumer crashed, the payload is durable and the Inbox recovery
// worker replays it after the lease expires.
func (s *Store) ClaimInbox(ctx context.Context, eventID, eventType string, payload []byte) (bool, error) {
	if eventID == "" {
		return true, nil
	}
	now := time.Now().UTC()
	record := paymentInboxRecord{
		EventID: eventID, EventType: eventType, Payload: append([]byte(nil), payload...),
		Attempts: 1, Status: "PROCESSING", ReceivedAt: now, UpdatedAt: now,
	}
	err := s.orm.WithContext(ctx).Create(&record).Error
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, gorm.ErrDuplicatedKey) {
		return false, err
	}

	var existing paymentInboxRecord
	if err := s.orm.WithContext(ctx).Where("event_id = ?", eventID).First(&existing).Error; err != nil {
		return false, err
	}
	switch existing.Status {
	case "PROCESSED":
		return false, nil
	case "PROCESSING":
		lease := now.Add(-5 * time.Minute)
		if existing.UpdatedAt.After(lease) {
			// The original consumer may still be working. Store the payload if this
			// row predates the recovery column, but never let this delivery ACK the
			// event: the caller must retry and the recovery worker can take over.
			if len(existing.Payload) == 0 && len(payload) > 0 {
				_ = s.orm.WithContext(ctx).Model(&paymentInboxRecord{}).
					Where("event_id = ? AND payload IS NULL", eventID).
					Update("payload", payload).Error
			}
			return false, nil
		}
		// Stale PROCESSING: claim the row with a conditional update so two
		// consumers cannot both take ownership.
		result := s.orm.WithContext(ctx).Model(&paymentInboxRecord{}).
			Where("event_id = ? AND status = 'PROCESSING' AND updated_at < ?", eventID, lease).
			Updates(map[string]any{
				"status": "PROCESSING", "last_error": nil, "updated_at": now,
				"attempts": gorm.Expr("attempts + 1"),
				"payload":  gorm.Expr("COALESCE(payload, ?)", payload),
			})
		if result.Error != nil {
			return false, result.Error
		}
		return result.RowsAffected == 1, nil
	case "FAILED":
		// A FAILED event is retryable immediately by the transport. Keeping the
		// payload also lets the recovery worker retry it after a broker retry is
		// exhausted.
		result := s.orm.WithContext(ctx).Model(&paymentInboxRecord{}).
			Where("event_id = ? AND status = 'FAILED'", eventID).
			Updates(map[string]any{
				"status": "PROCESSING", "last_error": nil, "updated_at": now,
				"attempts": gorm.Expr("attempts + 1"),
				"payload":  gorm.Expr("COALESCE(payload, ?)", payload),
			})
		if result.Error != nil {
			return false, result.Error
		}
		return result.RowsAffected == 1, nil
	default:
		return false, nil
	}
}

func (s *Store) MarkInboxProcessed(ctx context.Context, eventID string, at time.Time) error {
	if eventID == "" {
		return nil
	}
	return s.orm.WithContext(ctx).Model(&paymentInboxRecord{}).
		Where("event_id = ?", eventID).
		Updates(map[string]any{"status": "PROCESSED", "processed_at": at, "last_error": nil}).Error
}

func (s *Store) MarkInboxFailed(ctx context.Context, eventID string, reason string) error {
	if eventID == "" {
		return nil
	}
	return s.orm.WithContext(ctx).Model(&paymentInboxRecord{}).
		Where("event_id = ?", eventID).
		Updates(map[string]any{"status": "FAILED", "last_error": truncateReason(reason)}).Error
}

// MarkInboxRetrying keeps the row replayable for the recovery worker. The
// updated_at timestamp becomes the next lease, so a transient failure does not
// turn into a silently dropped event.
func (s *Store) MarkInboxRetrying(ctx context.Context, eventID, reason string, at time.Time) error {
	if eventID == "" {
		return nil
	}
	return s.orm.WithContext(ctx).Model(&paymentInboxRecord{}).
		Where("event_id = ?", eventID).
		Updates(map[string]any{
			"status": "PROCESSING", "last_error": truncateReason(reason), "updated_at": at,
		}).Error
}

// ClaimRecoverableInbox leases stale PROCESSING rows and FAILED rows that still
// have a payload. Rows are locked with SKIP LOCKED so multiple Payment instances
// can run the recovery worker without replaying the same event.
func (s *Store) ClaimRecoverableInbox(ctx context.Context, staleBefore time.Time, limit, maxAttempts int) ([]repository.InboxEvent, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if maxAttempts <= 0 {
		maxAttempts = 8
	}
	now := time.Now().UTC()
	events := make([]repository.InboxEvent, 0, limit)
	err := s.orm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var records []paymentInboxRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("payload IS NOT NULL AND attempts < ? AND ((status = 'PROCESSING' AND updated_at < ?) OR status = 'FAILED')",
				maxAttempts, staleBefore).
			Order("updated_at ASC").Limit(limit).Find(&records).Error; err != nil {
			return err
		}
		for _, record := range records {
			result := tx.Model(&paymentInboxRecord{}).
				Where("event_id = ? AND status = ?", record.EventID, record.Status).
				Updates(map[string]any{
					"status": "PROCESSING", "last_error": nil, "updated_at": now,
					"attempts": gorm.Expr("attempts + 1"),
				})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				continue
			}
			events = append(events, repository.InboxEvent{
				EventID: record.EventID, EventType: record.EventType,
				Payload: append([]byte(nil), record.Payload...), Attempts: record.Attempts + 1,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return events, nil
}

func (s *Store) InboxStatus(ctx context.Context, eventID string) (string, error) {
	if eventID == "" {
		return "", nil
	}
	var records []paymentInboxRecord
	if err := s.orm.WithContext(ctx).Model(&paymentInboxRecord{}).
		Where("event_id = ?", eventID).Limit(1).Find(&records).Error; err != nil {
		return "", err
	}
	if len(records) == 0 {
		return "", nil
	}
	return records[0].Status, nil
}

func (s *Store) PurgeInbox(ctx context.Context, processedBefore time.Time, limit int) (int64, error) {
	if limit <= 0 || limit > 10000 {
		limit = 1000
	}
	result := s.orm.WithContext(ctx).Exec(
		"DELETE FROM payment_inbox WHERE status = ? AND processed_at IS NOT NULL AND processed_at < ? LIMIT ?",
		"PROCESSED", processedBefore, limit,
	)
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}
