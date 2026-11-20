package mysqlstore

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"cityflow/internal/application/repository"
	"cityflow/internal/domain"
	"cityflow/internal/domain/rider"
)

func (s *Store) RiderForUser(ctx context.Context, id string) (*rider.Profile, error) {
	return s.getRider(activeRiderQuery(s.orm.WithContext(ctx)).Where("rider_profiles.user_id = ?", id))
}

func activeRiderQuery(db *gorm.DB) *gorm.DB {
	// Task owns rider_profiles and must not join Identity's users table. Account
	// status is enforced at the RPC/Gateway boundary; rider profile state is
	// local (is_archived) and never read from another service's schema.
	return db.Model(&riderRecord{}).
		Where("rider_profiles.is_archived = ?", false)
}

func (s *Store) getRider(query *gorm.DB) (*rider.Profile, error) {
	var record riderRecord
	err := query.Take(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load rider profile: %w", err)
	}
	return toRider(record)
}

func (s *Store) GetRider(ctx context.Context, id string) (*rider.Profile, error) {
	return s.getRider(activeRiderQuery(s.orm.WithContext(ctx)).Where("rider_profiles.id = ?", id))
}

func (s *Store) ListRiders(ctx context.Context, status string, page repository.Page) ([]*rider.Profile, error) {
	query := activeRiderQuery(s.orm.WithContext(ctx))
	if status != "" {
		query = query.Where("rider_profiles.status = ?", status)
	}
	var records []riderRecord
	if err := query.Order("rider_profiles.created_at DESC").Order("rider_profiles.id DESC").Scopes(paginate(page)).Find(&records).Error; err != nil {
		return nil, fmt.Errorf("list riders: %w", err)
	}
	result := make([]*rider.Profile, 0, len(records))
	for _, record := range records {
		item, err := toRider(record)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, nil
}

func (s *Store) CreateRider(ctx context.Context, userID string, request rider.UpdateRequest) (*rider.Profile, error) {
	id, err := newUUID()
	if err != nil {
		return nil, err
	}
	serviceTypes, err := serviceTypesJSON(request.Skills)
	if err != nil {
		return nil, err
	}
	record := riderRecord{
		ID: id, UserID: userID, DisplayName: request.DisplayName, VehicleType: request.VehicleType,
		Status: "OFFLINE", ServiceTypes: serviceTypes, ServiceArea: optionalString(request.ServiceArea),
		MaxDistanceMeters: request.MaxDistanceMeters, MaxConcurrentTasks: request.MaxConcurrentTasks,
		Rating: 5, OnTimeRate: 100,
	}
	if err := s.orm.WithContext(ctx).Create(&record).Error; err != nil {
		return nil, writeError(err)
	}
	return s.RiderForUser(ctx, userID)
}

func (s *Store) UpdateRider(ctx context.Context, userID string, request rider.UpdateRequest) (*rider.Profile, error) {
	serviceTypes, err := serviceTypesJSON(request.Skills)
	if err != nil {
		return nil, err
	}
	result := s.orm.WithContext(ctx).Model(&riderRecord{}).
		Where("user_id = ? AND is_archived = ?", userID, false).
		Updates(map[string]any{
			"display_name": request.DisplayName, "vehicle_type": request.VehicleType,
			"service_types": serviceTypes, "service_area": optionalString(request.ServiceArea),
			"max_distance_meters": request.MaxDistanceMeters, "max_concurrent_tasks": request.MaxConcurrentTasks,
		})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		if _, err := s.RiderForUser(ctx, userID); err != nil {
			return nil, err
		}
	}
	return s.RiderForUser(ctx, userID)
}

func (s *Store) ArchiveRider(ctx context.Context, userID string) error {
	result := s.orm.WithContext(ctx).Model(&riderRecord{}).
		Where("user_id = ? AND is_archived = ?", userID, false).
		Updates(map[string]any{"is_archived": true, "status": "SUSPENDED"})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) SetRiderStatus(ctx context.Context, userID, status string) (*rider.Profile, error) {
	result := s.orm.WithContext(ctx).Model(&riderRecord{}).
		Where("user_id = ? AND is_archived = ? AND status <> ?", userID, false, "SUSPENDED").
		Update("status", status)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		if _, err := s.RiderForUser(ctx, userID); err != nil {
			return nil, err
		}
	}
	return s.RiderForUser(ctx, userID)
}
