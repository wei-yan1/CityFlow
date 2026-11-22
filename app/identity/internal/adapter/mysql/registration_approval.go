package identitymysql

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"cityflow/app/identity/domain/rider"
	"cityflow/internal/common/auth"
	"cityflow/internal/common/domain"
)

// ApproveRiderRegistrationAccount is the Identity-side half of rider approval.
// It creates the login account and marks the application approved in one local
// transaction, but does NOT create the rider performance profile: that table is
// owned by the Task service and is provisioned over RPC by the caller. Keeping
// the two writes in their owning services is what preserves data ownership.
func (s *Store) ApproveRiderRegistrationAccount(ctx context.Context, id, adminID, note string) (*rider.Registration, error) {
	var updated *rider.Registration
	err := s.db.ORM.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var record riderRegistrationRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).Take(&record).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrNotFound
			}
			return fmt.Errorf("load rider registration for approval: %w", err)
		}
		if record.Status != rider.RegistrationPending {
			return fmt.Errorf("%w: rider registration is already %s", domain.ErrConflict, record.Status)
		}
		if record.PaidAmountFen < rider.RegistrationMinimumFen {
			return fmt.Errorf("%w: rider registration payment is below the minimum", domain.ErrInvalidRequest)
		}
		userID, err := newUUID()
		if err != nil {
			return err
		}
		user := userRecord{
			ID: userID, Email: record.Email, Phone: record.Phone, PasswordHash: record.PasswordHash,
			DisplayName: record.DisplayName, Role: auth.RoleRider, Status: "ACTIVE",
		}
		if err := tx.Create(&user).Error; err != nil {
			return writeError(err)
		}
		now := time.Now().UTC()
		result := tx.Model(&riderRegistrationRecord{}).
			Where("id = ? AND status = ?", id, rider.RegistrationPending).
			Updates(map[string]any{
				"status": rider.RegistrationApproved, "user_id": userID,
				"reviewed_by": adminID, "review_note": optionalString(note), "reviewed_at": now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("%w: rider registration changed during approval", domain.ErrConflict)
		}
		var fresh riderRegistrationRecord
		if err := tx.Where("id = ?", id).Take(&fresh).Error; err != nil {
			return err
		}
		var errConv error
		updated, errConv = toRiderRegistration(fresh)
		if errConv != nil {
			return errConv
		}
		payload, err := json.Marshal(updated)
		if err != nil {
			return fmt.Errorf("encode rider approval event: %w", err)
		}
		eventID, err := newUUID()
		if err != nil {
			return err
		}
		now = time.Now().UTC()
		event := outboxRecord{
			ID: eventID, EventType: "rider.approved.v1", AggregateID: userID,
			Payload: string(payload), Status: "PENDING", AvailableAt: now, CreatedAt: now,
		}
		if err := tx.Table(identityOutboxTable).Create(&event).Error; err != nil {
			return fmt.Errorf("write rider approval outbox: %w", err)
		}
		return nil
	})
	return updated, err
}
