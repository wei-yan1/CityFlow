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
	"cityflow/app/identity/repository"
	"cityflow/internal/common/domain"
)

var _ repository.RiderRegistrationRepository = (*Store)(nil)

type riderRegistrationRecord struct {
	ID                 string     `gorm:"column:id;type:char(36);primaryKey"`
	Email              string     `gorm:"column:email;size:254;not null"`
	Phone              *string    `gorm:"column:phone;size:32"`
	PasswordHash       string     `gorm:"column:password_hash;size:255;not null"`
	DisplayName        string     `gorm:"column:display_name;size:80;not null"`
	VehicleType        string     `gorm:"column:vehicle_type;size:24;not null"`
	ServiceTypes       string     `gorm:"column:service_types;type:json;not null"`
	ServiceArea        *string    `gorm:"column:service_area;size:160"`
	MaxDistanceMeters  int        `gorm:"column:max_distance_meters;not null"`
	MaxConcurrentTasks int        `gorm:"column:max_concurrent_tasks;not null"`
	PaidAmountFen      int64      `gorm:"column:paid_amount_fen;not null"`
	PaymentReference   string     `gorm:"column:payment_reference;size:128;not null"`
	PaymentNote        *string    `gorm:"column:payment_note;size:255"`
	Status             string     `gorm:"column:status;size:20;not null"`
	ReviewNote         *string    `gorm:"column:review_note;size:255"`
	ReviewedBy         *string    `gorm:"column:reviewed_by;type:char(36)"`
	ReviewedAt         *time.Time `gorm:"column:reviewed_at"`
	UserID             *string    `gorm:"column:user_id;type:char(36)"`
	CreatedAt          time.Time  `gorm:"column:created_at"`
	UpdatedAt          time.Time  `gorm:"column:updated_at"`
}

func (riderRegistrationRecord) TableName() string { return "rider_registrations" }

func toRiderRegistration(record riderRegistrationRecord) (*rider.Registration, error) {
	var skills []string
	if err := json.Unmarshal([]byte(record.ServiceTypes), &skills); err != nil {
		return nil, err
	}
	return &rider.Registration{
		ID: record.ID, Email: record.Email, Phone: stringValue(record.Phone),
		DisplayName: record.DisplayName, VehicleType: record.VehicleType, Skills: skills,
		ServiceArea: stringValue(record.ServiceArea), MaxDistanceMeters: record.MaxDistanceMeters,
		MaxConcurrentTasks: record.MaxConcurrentTasks, PaidAmountFen: record.PaidAmountFen,
		PaymentReference: record.PaymentReference, PaymentNote: stringValue(record.PaymentNote),
		Status: record.Status, ReviewNote: stringValue(record.ReviewNote), ReviewedBy: stringValue(record.ReviewedBy),
		ReviewedAt: record.ReviewedAt, UserID: stringValue(record.UserID),
		CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt, PasswordHash: record.PasswordHash,
	}, nil
}

func (s *Store) CreateRiderRegistration(ctx context.Context, item rider.Registration) (*rider.Registration, error) {
	if item.Email == "" || item.PaymentReference == "" || item.PaidAmountFen < rider.RegistrationMinimumFen || item.Status == "" {
		return nil, fmt.Errorf("%w: invalid rider registration", domain.ErrInvalidRequest)
	}
	serviceTypes, err := serviceTypesJSON(item.Skills)
	if err != nil {
		return nil, err
	}
	record := riderRegistrationRecord{
		ID: item.ID, Email: item.Email, Phone: optionalString(item.Phone), PasswordHash: item.PasswordHash,
		DisplayName: item.DisplayName, VehicleType: item.VehicleType, ServiceTypes: serviceTypes,
		ServiceArea: optionalString(item.ServiceArea), MaxDistanceMeters: item.MaxDistanceMeters,
		MaxConcurrentTasks: item.MaxConcurrentTasks, PaidAmountFen: item.PaidAmountFen,
		PaymentReference: item.PaymentReference, PaymentNote: optionalString(item.PaymentNote), Status: item.Status,
	}
	if record.ID == "" {
		id, err := newUUID()
		if err != nil {
			return nil, err
		}
		record.ID = id
	}
	if err := s.db.ORM.WithContext(ctx).Create(&record).Error; err != nil {
		return nil, writeError(err)
	}
	return s.GetRiderRegistration(ctx, record.ID)
}

func (s *Store) RiderRegistrationExists(ctx context.Context, email, phone string) (bool, error) {
	var userCount int64
	query := s.db.ORM.WithContext(ctx).Model(&userRecord{}).Where("email = ?", email)
	if phone != "" {
		query = query.Or("phone = ?", phone)
	}
	if err := query.Count(&userCount).Error; err != nil {
		return false, fmt.Errorf("check rider registration account: %w", err)
	}
	if userCount > 0 {
		return true, nil
	}
	var registrationCount int64
	if err := s.db.ORM.WithContext(ctx).Model(&riderRegistrationRecord{}).
		Where("status = ? AND email = ?", rider.RegistrationPending, email).Count(&registrationCount).Error; err != nil {
		return false, fmt.Errorf("check pending rider registration: %w", err)
	}
	if registrationCount > 0 {
		return true, nil
	}
	if phone != "" {
		if err := s.db.ORM.WithContext(ctx).Model(&riderRegistrationRecord{}).
			Where("status = ? AND phone = ?", rider.RegistrationPending, phone).Count(&registrationCount).Error; err != nil {
			return false, fmt.Errorf("check pending rider registration phone: %w", err)
		}
		if registrationCount > 0 {
			return true, nil
		}
	}
	return false, nil
}

func (s *Store) ListRiderRegistrations(ctx context.Context, status string, page repository.Page) ([]*rider.Registration, error) {
	query := s.db.ORM.WithContext(ctx).Model(&riderRegistrationRecord{})
	if status != "" {
		query = query.Where("status = ?", status)
	}
	var records []riderRegistrationRecord
	if err := query.Order("created_at DESC").Order("id").Scopes(paginate(page)).Find(&records).Error; err != nil {
		return nil, fmt.Errorf("list rider registrations: %w", err)
	}
	result := make([]*rider.Registration, 0, len(records))
	for _, record := range records {
		item, err := toRiderRegistration(record)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, nil
}

func (s *Store) GetRiderRegistration(ctx context.Context, id string) (*rider.Registration, error) {
	var record riderRegistrationRecord
	err := s.db.ORM.WithContext(ctx).Where("id = ?", id).Take(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load rider registration: %w", err)
	}
	return toRiderRegistration(record)
}

// ApproveRiderRegistration is kept for the repository interface, but approval
// that creates a Task-owned rider profile must use ApproveRiderRegistrationAccount
// plus a Task RPC call. This implementation never writes rider_profiles.
func (s *Store) ApproveRiderRegistration(ctx context.Context, id, adminID, note string) (*rider.Registration, error) {
	return s.ApproveRiderRegistrationAccount(ctx, id, adminID, note)
}

func (s *Store) RejectRiderRegistration(ctx context.Context, id, adminID, note string) (*rider.Registration, error) {
	var updated *rider.Registration
	err := s.db.ORM.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var record riderRegistrationRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).Take(&record).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrNotFound
			}
			return fmt.Errorf("load rider registration for rejection: %w", err)
		}
		if record.Status != rider.RegistrationPending {
			return fmt.Errorf("%w: rider registration is already %s", domain.ErrConflict, record.Status)
		}
		now := time.Now().UTC()
		result := tx.Model(&riderRegistrationRecord{}).
			Where("id = ? AND status = ?", id, rider.RegistrationPending).
			Updates(map[string]any{
				"status": rider.RegistrationRejected, "reviewed_by": adminID,
				"review_note": optionalString(note), "reviewed_at": now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("%w: rider registration changed during rejection", domain.ErrConflict)
		}
		var fresh riderRegistrationRecord
		if err := tx.Where("id = ?", id).Take(&fresh).Error; err != nil {
			return err
		}
		var errConv error
		updated, errConv = toRiderRegistration(fresh)
		return errConv
	})
	return updated, err
}
