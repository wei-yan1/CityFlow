// Package discoverymysql is the Discovery service's MySQL adapter. It owns
// merchants, media, map projections, trusted places and place reviews/comments.
package discoverymysql

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"

	"cityflow/app/discovery/domain/merchant"
	"cityflow/app/discovery/repository"
	"cityflow/internal/common/domain"
	"cityflow/internal/common/location"
	platformmysql "cityflow/internal/platform/mysql"
)

type Store struct {
	db *platformmysql.DB
}

func New(db *platformmysql.DB) *Store { return &Store{db: db} }

var (
	_ repository.MerchantRepository      = (*Store)(nil)
	_ repository.MerchantMedia           = (*Store)(nil)
	_ repository.MerchantReviews         = (*Store)(nil)
	_ repository.PublicPlaceReviewReader = (*Store)(nil)
	_ repository.PlaceReviewAdmin        = (*Store)(nil)
	_ repository.LikeProjection          = (*Store)(nil)
	_ repository.MerchantLikes           = (*Store)(nil)
	_ repository.MerchantHotSnapshot     = (*Store)(nil)
	_ repository.MerchantCardExtras      = (*Store)(nil)
	_ repository.TrustedPlaces           = (*Store)(nil)
)

type merchantRecord struct {
	ID              string    `gorm:"column:id;type:char(36);primaryKey"`
	UserID          string    `gorm:"column:user_id;type:char(36);not null;uniqueIndex:merchant_profiles_user_unique"`
	Name            string    `gorm:"column:store_name;size:120;not null"`
	Description     *string   `gorm:"column:description;type:text"`
	Phone           *string   `gorm:"column:contact_phone;size:32"`
	Address         string    `gorm:"column:address;type:text;not null"`
	AMAPPOIID       *string   `gorm:"column:amap_poi_id;size:128"`
	Latitude        float64   `gorm:"column:latitude;type:decimal(10,7);not null"`
	Longitude       float64   `gorm:"column:longitude;type:decimal(10,7);not null"`
	BusinessStatus  string    `gorm:"column:business_status;size:20;not null"`
	ServiceTypes    string    `gorm:"column:service_types;type:json;not null"`
	CapacityPerHour int       `gorm:"column:capacity_per_hour;not null"`
	PreparationMins int       `gorm:"column:preparation_minutes;not null"`
	BusinessHours   string    `gorm:"column:business_hours;type:json;not null"`
	CreatedAt       time.Time `gorm:"column:created_at"`
	UpdatedAt       time.Time `gorm:"column:updated_at"`
	Archived        bool      `gorm:"column:is_archived;not null"`
}

func (merchantRecord) TableName() string { return "merchant_profiles" }

type trustedPlaceRecord struct {
	POIID      string    `gorm:"column:poi_id;primaryKey"`
	Name       string    `gorm:"column:name"`
	Address    string    `gorm:"column:address"`
	Latitude   float64   `gorm:"column:latitude"`
	Longitude  float64   `gorm:"column:longitude"`
	VerifiedAt time.Time `gorm:"column:verified_at"`
	ExpiresAt  time.Time `gorm:"column:expires_at"`
}

func (trustedPlaceRecord) TableName() string { return "trusted_places" }

func toMerchant(record merchantRecord) (*merchant.Profile, error) {
	var serviceTypes []string
	if err := json.Unmarshal([]byte(record.ServiceTypes), &serviceTypes); err != nil {
		return nil, err
	}
	return &merchant.Profile{
		ID: record.ID, UserID: record.UserID, Name: record.Name,
		Description: stringValue(record.Description), Phone: stringValue(record.Phone),
		Address: record.Address, AMAPPOIID: stringValue(record.AMAPPOIID), Latitude: record.Latitude, Longitude: record.Longitude,
		BusinessStatus: record.BusinessStatus, ServiceTypes: serviceTypes,
		CapacityPerHour: record.CapacityPerHour, CreatedAt: record.CreatedAt,
		UpdatedAt: record.UpdatedAt, Archived: record.Archived,
	}, nil
}

func serviceTypesJSON(values []string) (string, error) {
	if values == nil {
		values = []string{}
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	copy := value
	return &copy
}

func newUUID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	encoded := fmt.Sprintf("%x", raw)
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:], nil
}

func paginate(page repository.Page) func(*gorm.DB) *gorm.DB {
	if page.Limit <= 0 || page.Limit > 200 {
		page.Limit = 50
	}
	if page.Offset < 0 {
		page.Offset = 0
	}
	return func(db *gorm.DB) *gorm.DB { return db.Limit(page.Limit).Offset(page.Offset) }
}

func writeError(err error) error {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return fmt.Errorf("%w: duplicate unique value", domain.ErrConflict)
	}
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		return fmt.Errorf("%w: duplicate unique value", domain.ErrConflict)
	}
	return err
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
