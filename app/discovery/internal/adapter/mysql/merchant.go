package discoverymysql

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"gorm.io/gorm"

	"cityflow/app/discovery/domain/merchant"
	"cityflow/app/discovery/repository"
	"cityflow/internal/common/domain"
	"cityflow/internal/common/location"
)

func (s *Store) MerchantForUser(ctx context.Context, id string) (*merchant.Profile, error) {
	return s.getMerchant(activeMerchantQuery(s.db.ORM.WithContext(ctx)).Where("merchant_profiles.user_id = ?", id))
}

// activeMerchantQuery only touches merchant_profiles. Account status is
// checked by the service through Identity, never by joining the users table.
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
	return s.getMerchant(activeMerchantQuery(s.db.ORM.WithContext(ctx)).Where("merchant_profiles.id = ?", id))
}

func (s *Store) GetMerchantsByIDs(ctx context.Context, ids []string) ([]*merchant.Profile, error) {
	return s.getMerchantsByIDs(ctx, ids, false)
}

// GetMerchantsByIDsIncludingArchived is used by historical ranking pages. The
// snapshot intentionally keeps merchant ids after archival, so the read path
// must be able to resolve those profiles without restoring them to the live map.
func (s *Store) GetMerchantsByIDsIncludingArchived(ctx context.Context, ids []string) ([]*merchant.Profile, error) {
	return s.getMerchantsByIDs(ctx, ids, true)
}

func (s *Store) getMerchantsByIDs(ctx context.Context, ids []string, includeArchived bool) ([]*merchant.Profile, error) {
	if len(ids) == 0 {
		return []*merchant.Profile{}, nil
	}
	query := s.db.ORM.WithContext(ctx).Model(&merchantRecord{}).Select("merchant_profiles.*")
	if !includeArchived {
		query = query.Where("merchant_profiles.is_archived = ?", false)
	}
	var records []merchantRecord
	if err := query.Where("merchant_profiles.id IN ?", ids).Find(&records).Error; err != nil {
		return nil, fmt.Errorf("load merchant profiles: %w", err)
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

func (s *Store) ListMerchants(ctx context.Context, status string, page repository.Page) ([]*merchant.Profile, error) {
	query := activeMerchantQuery(s.db.ORM.WithContext(ctx))
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

// SearchMerchants is the local half of map search. A verified CityFlow
// merchant must be discoverable even when AMap has not returned its POI.
func (s *Store) SearchMerchants(ctx context.Context, keywords string, limit int) ([]*merchant.Profile, error) {
	keywords = strings.TrimSpace(keywords)
	if keywords == "" {
		return []*merchant.Profile{}, nil
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	pattern := "%" + keywords + "%"
	var records []merchantRecord
	if err := activeMerchantQuery(s.db.ORM.WithContext(ctx)).
		Where("merchant_profiles.business_status = ?", "OPEN").
		Where("merchant_profiles.store_name LIKE ? OR merchant_profiles.address LIKE ?", pattern, pattern).
		Order("merchant_profiles.store_name").
		Limit(limit).
		Find(&records).Error; err != nil {
		return nil, fmt.Errorf("search merchants: %w", err)
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
	restored := s.db.ORM.WithContext(ctx).Model(&merchantRecord{}).Where("user_id = ? AND is_archived = ?", userID, true).Updates(map[string]any{
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
	if err := s.db.ORM.WithContext(ctx).Create(&record).Error; err != nil {
		return nil, writeError(err)
	}
	return s.MerchantForUser(ctx, userID)
}

func (s *Store) UpdateMerchant(ctx context.Context, userID string, request merchant.UpdateRequest) (*merchant.Profile, error) {
	serviceTypes, err := serviceTypesJSON(request.ServiceTypes)
	if err != nil {
		return nil, err
	}
	result := s.db.ORM.WithContext(ctx).Model(&merchantRecord{}).
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
	result := s.db.ORM.WithContext(ctx).Model(&merchantRecord{}).
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
	return s.getMerchant(activeMerchantQuery(s.db.ORM.WithContext(ctx)).Where("merchant_profiles.amap_poi_id = ?", poiID))
}

func (s *Store) FindMerchantNearName(ctx context.Context, name string, point location.Point, radiusMeters float64) (*merchant.Profile, error) {
	name = strings.TrimSpace(name)
	if name == "" || radiusMeters <= 0 {
		return nil, domain.ErrNotFound
	}
	var records []merchantRecord
	if err := activeMerchantQuery(s.db.ORM.WithContext(ctx)).
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

func (s *Store) MapMerchants(ctx context.Context, b location.Bounds, limit int) ([]location.Place, error) {
	var records []merchantRecord
	err := activeMerchantQuery(s.db.ORM.WithContext(ctx)).Where("merchant_profiles.business_status = ? AND longitude >= ? AND longitude < ? AND latitude >= ? AND latitude < ?", "OPEN", b.West, b.East, b.South, b.North).Order("merchant_profiles.id").Limit(limit).Find(&records).Error
	result := make([]location.Place, 0, len(records))
	for _, r := range records {
		result = append(result, location.Place{ID: r.ID, Name: r.Name, Address: r.Address, Category: "MERCHANT", Location: location.Point{Latitude: r.Latitude, Longitude: r.Longitude}})
	}
	return result, err
}

func (s *Store) GetMerchantMedia(ctx context.Context, id string) (string, []string, error) {
	var media struct {
		Avatar *string `gorm:"column:avatar_url"`
		Photos string  `gorm:"column:photos"`
	}
	err := s.db.ORM.WithContext(ctx).Table("merchant_media").Select("avatar_url,photos").Where("merchant_id = ?", id).Take(&media).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	var images []string
	if err := json.Unmarshal([]byte(media.Photos), &images); err != nil {
		return "", nil, err
	}
	return stringValue(media.Avatar), images, nil
}

func (s *Store) SaveMerchantMedia(ctx context.Context, id, avatar string, images []string) error {
	raw, err := json.Marshal(images)
	if err != nil {
		return err
	}
	return s.db.ORM.WithContext(ctx).Exec(
		`INSERT INTO merchant_media(merchant_id,avatar_url,photos) VALUES(?,?,?) ON DUPLICATE KEY UPDATE avatar_url=VALUES(avatar_url),photos=VALUES(photos)`,
		id, avatar, raw,
	).Error
}
