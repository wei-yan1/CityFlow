package taskmysql

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"cityflow/app/task/domain/rider"
	"cityflow/app/task/domain/task"
	"cityflow/app/task/repository"
	"cityflow/internal/common/domain"
)

func (s *Store) RiderForUser(ctx context.Context, id string) (*rider.Profile, error) {
	profile, err := s.getRider(activeRiderQuery(s.db.ORM.WithContext(ctx)).Where("rider_profiles.user_id = ?", id))
	if err != nil {
		return nil, err
	}
	return s.withCompleted(ctx, profile)
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
	profile, err := s.getRider(activeRiderQuery(s.db.ORM.WithContext(ctx)).Where("rider_profiles.id = ?", id))
	if err != nil {
		return nil, err
	}
	return s.withCompleted(ctx, profile)
}

func (s *Store) ListRiders(ctx context.Context, status string, page repository.Page) ([]*rider.Profile, error) {
	query := activeRiderQuery(s.db.ORM.WithContext(ctx))
	if status != "" {
		query = query.Where("rider_profiles.status = ?", status)
	}
	var records []riderRecord
	if err := query.Order("rider_profiles.created_at DESC").Order("rider_profiles.id DESC").Scopes(paginate(page)).Find(&records).Error; err != nil {
		return nil, fmt.Errorf("list riders: %w", err)
	}
	result := make([]*rider.Profile, 0, len(records))
	userIDs := make([]string, 0, len(records))
	for _, record := range records {
		item, err := toRider(record)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
		userIDs = append(userIDs, record.UserID)
	}
	counts, err := s.completedCounts(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	for _, item := range result {
		item.CompletedTasks = int(counts[item.UserID])
	}
	return result, nil
}

// CompletedTasks is derived from task rows so a profile can never drift from
// the authoritative work history. rider_profiles.completed_tasks remains for
// compatibility but is not trusted on reads.
func (s *Store) withCompleted(ctx context.Context, profile *rider.Profile) (*rider.Profile, error) {
	counts, err := s.completedCounts(ctx, []string{profile.UserID})
	if err != nil {
		return nil, err
	}
	profile.CompletedTasks = int(counts[profile.UserID])
	return profile, nil
}

func (s *Store) completedCounts(ctx context.Context, userIDs []string) (map[string]int64, error) {
	result := make(map[string]int64, len(userIDs))
	if len(userIDs) == 0 {
		return result, nil
	}
	var rows []struct {
		WorkerID string
		Count    int64
	}
	if err := s.db.ORM.WithContext(ctx).Model(&taskRecord{}).
		Select("worker_id, COUNT(*) AS count").
		Where("worker_id IN ? AND status = ?", userIDs, task.StatusCompleted).
		Group("worker_id").
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("count completed tasks: %w", err)
	}
	for _, row := range rows {
		result[row.WorkerID] = row.Count
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
	if err := s.db.ORM.WithContext(ctx).Create(&record).Error; err != nil {
		return nil, writeError(err)
	}
	return s.RiderForUser(ctx, userID)
}

func (s *Store) UpdateRider(ctx context.Context, userID string, request rider.UpdateRequest) (*rider.Profile, error) {
	serviceTypes, err := serviceTypesJSON(request.Skills)
	if err != nil {
		return nil, err
	}
	result := s.db.ORM.WithContext(ctx).Model(&riderRecord{}).
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
	result := s.db.ORM.WithContext(ctx).Model(&riderRecord{}).
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
	result := s.db.ORM.WithContext(ctx).Model(&riderRecord{}).
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
