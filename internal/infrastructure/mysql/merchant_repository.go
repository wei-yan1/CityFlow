package mysqlstore

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"gorm.io/gorm"

	"cityflow/internal/application/repository"
	"cityflow/internal/domain"
	"cityflow/internal/domain/location"
	"cityflow/internal/domain/merchant"
)

func (s *Store) MerchantForUser(ctx context.Context, id string) (*merchant.Profile, error) {
	return s.getMerchant(activeMerchantQuery(s.orm.WithContext(ctx)).Where("merchant_profiles.user_id = ?", id))
}

// Discovery owns merchant_profiles. Account activity is checked through the
// Identity RPC by the service layer; this repository must never join the
// identity-owned users table.
func activeMerchantQuery(db *gorm.DB) *gorm.DB {
	return db.Model(&merchantRecord{}).
		Select("merchant_profiles.*").
		Where("merchant_profiles.is_archived = ?", false)
}

func (s *Store) getMerchant(query *gorm.DB) (*merchant.Profile, error) {
	var record merchantRecord
	err := query.Take(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load merchant profile: %w", err)
	}
	return toMerchant(record)
}

func (s *Store) GetMerchant(ctx context.Context, id string) (*merchant.Profile, error) {
	return s.getMerchant(activeMerchantQuery(s.orm.WithContext(ctx)).Where("merchant_profiles.id = ?", id))
}

func (s *Store) ListMerchants(ctx context.Context, status string, page repository.Page) ([]*merchant.Profile, error) {
	query := activeMerchantQuery(s.orm.WithContext(ctx))
	if status != "" {
		query = query.Where("merchant_profiles.business_status = ?", status)
	}
	var records []merchantRecord
	if err := query.Order("merchant_profiles.created_at DESC").Order("merchant_profiles.id DESC").Scopes(paginate(page)).Find(&records).Error; err != nil {
		return nil, fmt.Errorf("list merchants: %w", err)
	}
	result := make([]*merchant.Profile, 0, len(records))
	for _, record := range records {
		item, err := toMerchant(record)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, nil
}

func (s *Store) CreateMerchant(ctx context.Context, userID string, request merchant.UpdateRequest) (*merchant.Profile, error) {
	id, err := newUUID()
	if err != nil {
		return nil, err
	}
	serviceTypes, err := serviceTypesJSON(request.ServiceTypes)
	if err != nil {
		return nil, err
	}
	record := merchantRecord{
		ID: id, UserID: userID, Name: request.Name, Description: optionalString(request.Description),
		Phone: optionalString(request.Phone), Address: request.Address, AMAPPOIID: optionalString(request.AMAPPOIID), Latitude: request.Latitude,
		Longitude: request.Longitude, BusinessStatus: request.BusinessStatus, ServiceTypes: serviceTypes,
		CapacityPerHour: request.CapacityPerHour, BusinessHours: "{}",
	}
	restored := s.orm.WithContext(ctx).Model(&merchantRecord{}).Where("user_id = ? AND is_archived = ?", userID, true).Updates(map[string]any{
		"store_name": request.Name, "description": optionalString(request.Description), "contact_phone": optionalString(request.Phone), "address": request.Address,
		"amap_poi_id": optionalString(request.AMAPPOIID),
		"latitude":    request.Latitude, "longitude": request.Longitude, "business_status": request.BusinessStatus, "service_types": serviceTypes, "capacity_per_hour": request.CapacityPerHour, "is_archived": false,
	})
	if restored.Error != nil {
		return nil, restored.Error
	}
	if restored.RowsAffected > 0 {
		return s.MerchantForUser(ctx, userID)
	}
	if err := s.orm.WithContext(ctx).Create(&record).Error; err != nil {
		return nil, writeError(err)
	}
	return s.MerchantForUser(ctx, userID)
}

func (s *Store) UpdateMerchant(ctx context.Context, userID string, request merchant.UpdateRequest) (*merchant.Profile, error) {
	serviceTypes, err := serviceTypesJSON(request.ServiceTypes)
	if err != nil {
		return nil, err
	}
	result := s.orm.WithContext(ctx).Model(&merchantRecord{}).
		Where("user_id = ? AND is_archived = ?", userID, false).
		Updates(map[string]any{
			"store_name": request.Name, "description": optionalString(request.Description),
			"contact_phone": optionalString(request.Phone), "address": request.Address,
			"amap_poi_id": optionalString(request.AMAPPOIID),
			"latitude":    request.Latitude, "longitude": request.Longitude,
			"business_status": request.BusinessStatus, "service_types": serviceTypes,
			"capacity_per_hour": request.CapacityPerHour,
		})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		if _, err := s.MerchantForUser(ctx, userID); err != nil {
			return nil, err
		}
	}
	return s.MerchantForUser(ctx, userID)
}

func (s *Store) ArchiveMerchant(ctx context.Context, userID string) error {
	result := s.orm.WithContext(ctx).Model(&merchantRecord{}).
		Where("user_id = ? AND is_archived = ?", userID, false).
		Updates(map[string]any{"is_archived": true, "business_status": "CLOSED"})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) FindMerchantByAMapPOI(ctx context.Context, poiID string) (*merchant.Profile, error) {
	poiID = strings.TrimSpace(poiID)
	if poiID == "" {
		return nil, domain.ErrNotFound
	}
	return s.getMerchant(activeMerchantQuery(s.orm.WithContext(ctx)).Where("merchant_profiles.amap_poi_id = ?", poiID))
}

func (s *Store) FindMerchantNearName(ctx context.Context, name string, point location.Point, radiusMeters float64) (*merchant.Profile, error) {
	name = strings.TrimSpace(name)
	if name == "" || radiusMeters <= 0 {
		return nil, domain.ErrNotFound
	}
	var records []merchantRecord
	if err := activeMerchantQuery(s.orm.WithContext(ctx)).
		Where("merchant_profiles.store_name = ? AND merchant_profiles.business_status = ?", name, "OPEN").
		Find(&records).Error; err != nil {
		return nil, err
	}
	var nearest *merchant.Profile
	nearestDistance := math.Inf(1)
	for _, record := range records {
		distance := pointDistanceMeters(point, location.Point{Latitude: record.Latitude, Longitude: record.Longitude})
		if distance <= radiusMeters && distance < nearestDistance {
			item, err := toMerchant(record)
			if err != nil {
				return nil, err
			}
			nearest = item
			nearestDistance = distance
		}
	}
	if nearest == nil {
		return nil, domain.ErrNotFound
	}
	return nearest, nil
}

func pointDistanceMeters(a, b location.Point) float64 {
	const earthRadius = 6371000.0
	lat1 := a.Latitude * math.Pi / 180
	lat2 := b.Latitude * math.Pi / 180
	dLat := lat2 - lat1
	dLon := (b.Longitude - a.Longitude) * math.Pi / 180
	h := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1)*math.Cos(lat2)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * earthRadius * math.Asin(math.Min(1, math.Sqrt(h)))
}
